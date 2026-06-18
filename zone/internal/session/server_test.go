package session

import (
	"os"
	"testing"
	"time"

	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/db"
	"github.com/zkfriendly/zone/internal/store"
)

// isolate points config/db/socket paths at a temp HOME so tests don't touch the
// real user config dir, and writes a silent config so no audio device is used.
// HOME is kept short because macOS limits Unix socket paths to ~104 bytes.
func isolate(t *testing.T) *store.Store {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "zone")
	if err != nil {
		home = t.TempDir()
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg := config.Default()
	cfg.Volume = 0
	cfg.ChimesEnabled = false
	cfg.Layers = map[string]bool{"beat15": false, "beat45": false}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dbPath, err := db.DefaultPath()
	if err != nil {
		t.Fatalf("db path: %v", err)
	}
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return store.New(database)
}

func waitFor(cond func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestDaemonRoundtrip(t *testing.T) {
	st := isolate(t)
	p, _ := st.CreateProject("Code", "")
	task, _ := st.CreateTask(p.ID, "Refactor")
	sess, err := st.CreateSession(&task.ID, 2, 1, 3, 0) // 1 cycle: 2s work, 1s break
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	daemonErr := make(chan error, 1)
	go func() { daemonErr <- RunDaemon(sess.ID) }()
	t.Cleanup(func() { <-daemonErr })

	if !waitFor(IsAlive, 3*time.Second) {
		t.Fatal("daemon did not start")
	}

	client, err := Dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	snap, err := client.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if snap.Phase != "work" || !snap.Running || snap.Remaining != 2 {
		t.Fatalf("unexpected initial snapshot: %+v", snap)
	}
	if snap.Cycles != 1 {
		t.Fatalf("expected 1 cycle, got %d", snap.Cycles)
	}

	// Toggling an ambient layer should flip its reported state.
	snap, _ = client.Track(1)
	if len(snap.Tracks) < 2 || !snap.Tracks[1].Enabled {
		t.Fatalf("expected track 1 enabled: %+v", snap.Tracks)
	}

	// Skip work -> break, then skip break -> finished.
	snap, _ = client.Skip()
	if snap.Phase != "break" {
		t.Fatalf("expected break after skip, got %q", snap.Phase)
	}
	snap, _ = client.Skip()
	if !snap.Finished {
		t.Fatalf("expected finished after final skip: %+v", snap)
	}

	// The session should now be recorded as completed.
	if !waitFor(func() bool {
		s, err := st.GetSession(sess.ID)
		return err == nil && s.Status == store.SessionCompleted
	}, 2*time.Second) {
		t.Fatal("session was not marked completed")
	}

	// End dismisses the daemon.
	_, _ = client.End()
	client.Close()
	if !waitFor(func() bool { return !IsAlive() }, 3*time.Second) {
		t.Fatal("daemon did not shut down after End")
	}
}

func TestDaemonTaskSwitching(t *testing.T) {
	st := isolate(t)
	p, _ := st.CreateProject("Code", "")
	alpha, _ := st.CreateTask(p.ID, "Alpha")
	beta, _ := st.CreateTask(p.ID, "Beta")
	// General session (no starting task), one long work block.
	sess, _ := st.CreateSession(nil, 30, 1, 31, 0)

	daemonErr := make(chan error, 1)
	go func() { daemonErr <- RunDaemon(sess.ID) }()
	t.Cleanup(func() {
		if c, err := Dial(); err == nil {
			c.End()
			c.Close()
		}
		<-daemonErr
	})
	if !waitFor(IsAlive, 3*time.Second) {
		t.Fatal("daemon did not start")
	}
	client, err := Dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	snap, _ := client.Status()
	if snap.CurrentTaskID != 0 || snap.TaskTitle != "" {
		t.Fatalf("expected general (no task) start, got %+v", snap)
	}

	// Work ~1.2s on Alpha, then switch to Beta and work ~1.2s.
	snap, _ = client.SetTask(alpha.ID)
	if snap.CurrentTaskID != alpha.ID || snap.TaskTitle != "Alpha" {
		t.Fatalf("expected current task Alpha, got %+v", snap)
	}
	time.Sleep(1200 * time.Millisecond)
	snap, _ = client.SetTask(beta.ID)
	if snap.CurrentTaskID != beta.ID {
		t.Fatalf("expected current task Beta, got %+v", snap)
	}
	time.Sleep(1200 * time.Millisecond)

	// Ending records the in-flight segment for Beta.
	_, _ = client.End()
	if !waitFor(func() bool { return !IsAlive() }, 3*time.Second) {
		t.Fatal("daemon did not shut down")
	}

	ws, _ := st.TaskWorkSeconds()
	if ws[alpha.ID] <= 0 {
		t.Fatalf("expected time attributed to Alpha, got %d", ws[alpha.ID])
	}
	if ws[beta.ID] <= 0 {
		t.Fatalf("expected time attributed to Beta, got %d", ws[beta.ID])
	}

	// The session's current task should have been persisted as Beta.
	final, _ := st.GetSession(sess.ID)
	if final.TaskID == nil || *final.TaskID != beta.ID {
		t.Fatalf("expected session current task = Beta, got %v", final.TaskID)
	}
}

func TestDaemonResumesPersistedState(t *testing.T) {
	st := isolate(t)
	p, _ := st.CreateProject("Code", "")
	task, _ := st.CreateTask(p.ID, "Resume me")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	// Simulate a session that was mid-break with progress when the daemon stopped.
	if err := st.SaveRuntime(sess.ID, store.Runtime{
		Phase: "break", Remaining: 400, Cycle: 2, Accrued: 7200, Running: false,
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	daemonErr := make(chan error, 1)
	go func() { daemonErr <- RunDaemon(sess.ID) }()
	t.Cleanup(func() {
		if c, err := Dial(); err == nil {
			c.End()
			c.Close()
		}
		<-daemonErr
	})

	if !waitFor(IsAlive, 3*time.Second) {
		t.Fatal("daemon did not start")
	}
	client, err := Dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	snap, _ := client.Status()
	if snap.Phase != "break" || snap.Remaining != 400 || snap.CycleIndex != 2 {
		t.Fatalf("daemon did not resume persisted state: %+v", snap)
	}
	if snap.Running {
		t.Fatal("resumed session should still be paused")
	}
	if snap.Accrued != 7200 {
		t.Fatalf("expected accrued 7200, got %d", snap.Accrued)
	}
}

func TestDaemonResumesAbandonedSession(t *testing.T) {
	st := isolate(t)
	p, _ := st.CreateProject("Code", "")
	task, _ := st.CreateTask(p.ID, "Pick me back up")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	// Simulate a session that was running mid-work block, then ended early.
	if err := st.SaveRuntime(sess.ID, store.Runtime{
		Phase: "work", Remaining: 1500, Cycle: 1, Accrued: 1500, Running: true,
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}
	if err := st.EndSession(sess.ID, store.SessionAbandoned); err != nil {
		t.Fatalf("end: %v", err)
	}

	// Reopen and resume it (as the UI's resume path does before spawning).
	if err := st.ReopenSession(sess.ID); err != nil {
		t.Fatalf("reopen: %v", err)
	}

	daemonErr := make(chan error, 1)
	go func() { daemonErr <- RunDaemon(sess.ID) }()
	t.Cleanup(func() {
		if c, err := Dial(); err == nil {
			c.End()
			c.Close()
		}
		<-daemonErr
	})
	if !waitFor(IsAlive, 3*time.Second) {
		t.Fatal("daemon did not start")
	}
	client, err := Dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	snap, _ := client.Status()
	if snap.Phase != "work" || snap.CycleIndex != 1 {
		t.Fatalf("resumed session lost its place: %+v", snap)
	}
	if !snap.Running {
		t.Fatal("a session ended while running should resume running")
	}
	if snap.Remaining > 1500 || snap.Remaining < 1490 {
		t.Fatalf("expected to resume near 1500s remaining, got %d", snap.Remaining)
	}
	if snap.Finished {
		t.Fatal("resumed session should not be finished")
	}
}
