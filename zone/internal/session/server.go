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

	// Current task (may be unset for general focus). segCredited is how many
	// seconds of the in-progress work block have already been attributed (to the
	// current or a previously-selected task), so switching tasks mid-block splits
	// the time correctly.
	task        store.Task
	hasTask     bool
	segCredited int

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
	var task store.Task
	hasTask := false
	if sess.TaskID != nil {
		if t, err := st.GetTask(*sess.TaskID); err == nil {
			task = t
			hasTask = true
		}
	}
	rt, err := st.LoadRuntime(sessionID)
	if err != nil {
		return err
	}

	engine := timer.Restore(sess.WorkSec, sess.BreakSec, sess.TotalSec, sess.PrepareSec,
		timer.PhaseFromString(rt.Phase), rt.Remaining, rt.Cycle, rt.Running)
	fresh := rt.Remaining <= 0
	// segCredited tracks how much of the current block is already recorded. When
	// the engine is reset to a fresh start, nothing is credited yet.
	segCredited := rt.SegCredited
	if fresh {
		segCredited = 0
		// Fresh session: start running immediately.
		engine.Start()
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}

	s := &server{
		store:       st,
		audio:       audio.New(cfg),
		cfg:         cfg,
		engine:      engine,
		session:     sess,
		task:        task,
		hasTask:     hasTask,
		accrued:     rt.Accrued,
		segCredited: segCredited,
		ln:          ln,
		done:        make(chan struct{}),
	}

	if s.engine.Running() && s.engine.Phase() == timer.Work {
		s.audio.StartAmbient()
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
	case OpSetTask:
		if !s.finished {
			s.setTask(cmd.TaskID)
		}
	case OpAdjust:
		if !s.finished && cmd.Delta != 0 {
			if s.engine.AdjustRemaining(cmd.Delta) {
				if s.engine.Phase() == timer.Work {
					elapsed := s.engine.Elapsed()
					if cmd.Delta < 0 {
						s.recordWorkSegment(elapsed)
					} else if s.segCredited > elapsed {
						s.segCredited = elapsed
					}
				}
			}
		}
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

// setTask switches the current task, attributing the work done since the last
// switch to the previous task before changing. Caller holds the mutex.
func (s *server) setTask(taskID int64) {
	// Credit time accrued so far in this work block to the outgoing task.
	if s.engine.Phase() == timer.Work {
		s.recordWorkSegment(s.engine.Elapsed())
	}
	if taskID == 0 {
		s.task = store.Task{}
		s.hasTask = false
		_ = s.store.SetSessionTask(s.session.ID, nil)
		return
	}
	if t, err := s.store.GetTask(taskID); err == nil {
		s.task = t
		s.hasTask = true
		id := taskID
		_ = s.store.SetSessionTask(s.session.ID, &id)
	}
}

// handleTransition records the finished block and reacts to the next one.
// Caller must hold the mutex.
func (s *server) handleTransition(tr timer.Transition) {
	// Credit the remaining portion of an ending work block to the current task.
	if tr.Ended.Phase == timer.Work {
		s.recordWorkSegment(tr.Elapsed)
	}
	s.segCredited = 0 // reset for the next block

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

// recordWorkSegment attributes the portion of the current work block that hasn't
// been credited yet (blockElapsed - segCredited) to the current task, if any.
// Caller holds the mutex.
func (s *server) recordWorkSegment(blockElapsed int) {
	delta := blockElapsed - s.segCredited
	if delta <= 0 {
		return
	}
	if s.hasTask {
		end := time.Now()
		start := end.Add(-time.Duration(delta) * time.Second)
		sid := s.session.ID
		if _, err := s.store.AddEntry(s.task.ID, &sid, store.KindWork, start, end); err == nil {
			s.accrued += delta
		}
	}
	s.segCredited = blockElapsed
}

// endLocked ends the session early, recording any in-flight work. Holds the mutex.
func (s *server) endLocked() {
	if s.finished {
		return
	}
	if s.engine.Phase() == timer.Work {
		s.recordWorkSegment(s.engine.Elapsed())
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
		Phase:       s.engine.Phase().String(),
		Remaining:   s.engine.Remaining(),
		Cycle:       s.engine.CycleIndex(),
		Accrued:     s.accrued,
		SegCredited: s.segCredited,
		Running:     s.engine.Running(),
	})
}

// snapshotLocked builds a Snapshot of the current state. Caller holds the mutex.
func (s *server) snapshotLocked() Snapshot {
	today, _ := s.store.TodayWorkSeconds()
	// Add the not-yet-recorded portion of the current work block (it only lands
	// in the DB at a block boundary / task switch), when a task is being tracked.
	if !s.finished && s.engine.Phase() == timer.Work && s.hasTask {
		today += s.engine.Elapsed() - s.segCredited
	}

	var tracks []TrackInfo
	for _, t := range s.audio.Tracks() {
		tracks = append(tracks, TrackInfo{ID: t.ID, Label: t.Label, Enabled: t.Enabled})
	}

	var curTaskID int64
	if s.hasTask {
		curTaskID = s.task.ID
	}

	wall := 0
	if !s.session.StartedAt.IsZero() {
		wall = int(time.Since(s.session.StartedAt).Seconds())
	}

	return Snapshot{
		SessionID:     s.session.ID,
		CurrentTaskID: curTaskID,
		TaskTitle:     s.task.Title,
		ProjectName:   s.task.ProjectName,
		Phase:         s.engine.Phase().String(),
		Running:       s.engine.Running(),
		Remaining:     s.engine.Remaining(),
		Planned:       s.engine.Planned(),
		CycleIndex:    s.engine.CycleIndex(),
		Cycles:        s.engine.Cycles(),
		Finished:      s.finished,
		Accrued:       s.accrued,
		WallSec:       wall,
		TodayTotal:    today,
		Volume:        s.audio.Volume(),
		Tracks:        tracks,
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
