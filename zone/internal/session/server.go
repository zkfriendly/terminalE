package session

import (
	"encoding/json"
	"net"
	"os"
	"sync"
	"time"

	"github.com/zkfriendly/zone/internal/audio"
	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/db"
	"github.com/zkfriendly/zone/internal/store"
	"github.com/zkfriendly/zone/internal/timer"
)

// finishedGrace is how long the daemon lingers after a session completes so a
// reconnecting client can show the summary, before it exits on its own.
const finishedGrace = 15 * time.Minute

// server is the running focus daemon.
type server struct {
	mu sync.Mutex

	store  *store.Store
	audio  *audio.Manager
	cfg    config.Config
	engine *timer.Engine

	session store.Session
	task    store.Task

	accrued    int
	finished   bool
	finishedAt time.Time

	ln   net.Listener
	done chan struct{}
	once sync.Once
}

// RunDaemon is the entry point for the background process (`zone __daemon <id>`).
// It blocks until the session ends or completes.
func RunDaemon(sessionID int64) error {
	sockPath, err := SocketPath()
	if err != nil {
		return err
	}

	// If a live daemon already owns the socket, do nothing.
	if c, err := net.Dial("unix", sockPath); err == nil {
		c.Close()
		return nil
	}
	_ = os.Remove(sockPath) // clear any stale socket

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	dbPath, err := db.DefaultPath()
	if err != nil {
		return err
	}
	database, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()
	st := store.New(database)

	sess, err := st.GetSession(sessionID)
	if err != nil {
		return err
	}
	task, err := st.GetTask(sess.TaskID)
	if err != nil {
		return err
	}
	rt, err := st.LoadRuntime(sessionID)
	if err != nil {
		return err
	}

	engine := timer.Restore(sess.WorkSec, sess.BreakSec, sess.TotalSec, sess.PrepareSec,
		timer.PhaseFromString(rt.Phase), rt.Remaining, rt.Cycle, rt.Running)
	fresh := rt.Remaining <= 0
	if fresh {
		// Fresh session: start running immediately.
		engine.Start()
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}

	s := &server{
		store:   st,
		audio:   audio.New(cfg),
		cfg:     cfg,
		engine:  engine,
		session: sess,
		task:    task,
		accrued: rt.Accrued,
		ln:      ln,
		done:    make(chan struct{}),
	}

	if s.engine.Running() && s.engine.Phase() == timer.Work {
		s.audio.StartAmbient()
	}
	// On a brand-new session, demo the start/end chimes during the prepare block
	// so the user knows what to listen for.
	if fresh && s.engine.Phase() == timer.Prepare {
		s.previewChimes()
	}
	s.persistRuntime()

	go s.acceptLoop()
	go s.tickLoop()

	<-s.done
	s.cleanup(sockPath)
	return nil
}

func (s *server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed
		}
		go s.handleConn(conn)
	}
}

func (s *server) handleConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var cmd Command
		if err := dec.Decode(&cmd); err != nil {
			return
		}
		snap := s.handle(cmd)
		if err := enc.Encode(snap); err != nil {
			return
		}
	}
}

func (s *server) tickLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			if !s.finished && s.engine.Running() {
				if tr, ok := s.engine.Tick(); ok {
					s.handleTransition(tr)
				}
			}
			s.persistRuntime()
			expired := s.finished && time.Since(s.finishedAt) > finishedGrace
			s.mu.Unlock()
			if expired {
				s.signalDone()
				return
			}
		}
	}
}

// handle processes a command and returns the resulting snapshot.
func (s *server) handle(cmd Command) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch cmd.Op {
	case OpToggle:
		if !s.finished {
			s.engine.Toggle()
			if s.engine.Running() && s.engine.Phase() == timer.Work {
				s.audio.StartAmbient()
			} else {
				s.audio.StopAmbient()
			}
		}
	case OpSkip:
		if !s.finished {
			if tr, ok := s.engine.Skip(); ok {
				s.handleTransition(tr)
			}
		}
	case OpTrack:
		s.audio.ToggleTrack(cmd.Index)
	case OpVolume:
		s.audio.SetVolume(cmd.Volume)
	case OpPreview:
		s.previewChimes()
	case OpEnd:
		s.endLocked()
		defer s.signalDone()
	}
	s.persistRuntime()
	return s.snapshotLocked()
}

// previewChimes plays the work-start chime and, shortly after, the session-done
// chime, so the user learns what each transition sounds like. Non-blocking.
func (s *server) previewChimes() {
	go func() {
		s.audio.Chime(audio.ChimeWork)
		time.Sleep(1300 * time.Millisecond)
		s.audio.Chime(audio.ChimeDone)
	}()
}

// handleTransition records the finished block and reacts to the next one.
// Caller must hold the mutex.
func (s *server) handleTransition(tr timer.Transition) {
	s.recordBlock(tr.Ended.Phase, tr.Elapsed)

	if tr.Finished {
		s.finished = true
		s.finishedAt = time.Now()
		s.audio.StopAmbient()
		s.audio.Chime(audio.ChimeDone)
		_ = s.store.EndSession(s.session.ID, store.SessionCompleted)
		return
	}

	if tr.Next.Phase == timer.Work {
		s.audio.Chime(audio.ChimeWork)
		if s.engine.Running() {
			s.audio.StartAmbient()
		}
	} else {
		s.audio.Chime(audio.ChimeBreak)
		s.audio.StopAmbient()
	}
}

// recordBlock persists a completed block as a time entry. Caller holds the mutex.
func (s *server) recordBlock(phase timer.Phase, elapsed int) {
	if elapsed <= 0 || phase == timer.Prepare {
		return // the prepare block is settle-in time; never tracked
	}
	kind := store.KindWork
	if phase == timer.Break {
		kind = store.KindBreak
	}
	end := time.Now()
	start := end.Add(-time.Duration(elapsed) * time.Second)
	sid := s.session.ID
	if _, err := s.store.AddEntry(s.task.ID, &sid, kind, start, end); err == nil && phase == timer.Work {
		s.accrued += elapsed
	}
}

// endLocked ends the session early, recording any in-flight work. Holds the mutex.
func (s *server) endLocked() {
	if s.finished {
		return
	}
	if s.engine.Phase() == timer.Work {
		s.recordBlock(timer.Work, s.engine.Elapsed())
	}
	s.audio.StopAmbient()
	s.finished = true
	s.finishedAt = time.Now()
	_ = s.store.EndSession(s.session.ID, store.SessionAbandoned)
}

// persistRuntime writes the live engine state so the session can be resumed.
// Caller holds the mutex.
func (s *server) persistRuntime() {
	_ = s.store.SaveRuntime(s.session.ID, store.Runtime{
		Phase:     s.engine.Phase().String(),
		Remaining: s.engine.Remaining(),
		Cycle:     s.engine.CycleIndex(),
		Accrued:   s.accrued,
		Running:   s.engine.Running(),
	})
}

// snapshotLocked builds a Snapshot of the current state. Caller holds the mutex.
func (s *server) snapshotLocked() Snapshot {
	today, _ := s.store.TodayWorkSeconds()
	if !s.finished && s.engine.Phase() == timer.Work {
		today += s.engine.Elapsed()
	}

	var tracks []TrackInfo
	for _, t := range s.audio.Tracks() {
		tracks = append(tracks, TrackInfo{ID: t.ID, Label: t.Label, Enabled: t.Enabled})
	}

	return Snapshot{
		SessionID:   s.session.ID,
		TaskTitle:   s.task.Title,
		ProjectName: s.task.ProjectName,
		Phase:       s.engine.Phase().String(),
		Running:     s.engine.Running(),
		Remaining:   s.engine.Remaining(),
		Planned:     s.engine.Planned(),
		CycleIndex:  s.engine.CycleIndex(),
		Cycles:      s.engine.Cycles(),
		Finished:    s.finished,
		Accrued:     s.accrued,
		TodayTotal:  today,
		Volume:      s.audio.Volume(),
		Tracks:      tracks,
	}
}

func (s *server) signalDone() {
	s.once.Do(func() { close(s.done) })
}

func (s *server) cleanup(sockPath string) {
	s.ln.Close()
	_ = os.Remove(sockPath)

	// Remember the user's last sound selection and volume.
	s.cfg.Layers = s.audio.Selection()
	s.cfg.Volume = s.audio.Volume()
	_ = s.cfg.Save()
	s.audio.Close()
}
