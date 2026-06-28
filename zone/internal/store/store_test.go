package store

import (
	"testing"
	"time"

	"github.com/zkfriendly/zone/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return New(database)
}

func projectRootChildren(s *Store, projectID int64, includeArchived bool) ([]Task, error) {
	p, err := s.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	if p.RootTaskID == 0 {
		return nil, nil
	}
	all, err := s.ListTaskTree(includeArchived)
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, t := range all {
		if t.ParentID != nil && *t.ParentID == p.RootTaskID {
			out = append(out, t)
		}
	}
	return out, nil
}

func TestProjectTaskLifecycle(t *testing.T) {
	s := newTestStore(t)

	p, err := s.CreateProject("Writing", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if p.Color == "" {
		t.Fatal("expected an auto-assigned color")
	}

	task, err := s.CreateTask(p.ID, "Chapter 1")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.ProjectName != "Writing" {
		t.Fatalf("expected joined project name, got %q", task.ProjectName)
	}

	tasks, err := projectRootChildren(s, p.ID, false)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("list tasks: %v (n=%d)", err, len(tasks))
	}

	if err := s.SetTaskArchived(task.ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	tasks, _ = projectRootChildren(s, p.ID, false)
	if len(tasks) != 0 {
		t.Fatalf("archived task should be hidden, got %d", len(tasks))
	}
}

func TestEntriesAndStats(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("Code", "#fff")
	task, _ := s.CreateTask(p.ID, "Refactor")

	sess, err := s.CreateSession(&task.ID, 3000, 600, 14400, 180)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	now := time.Now()
	sid := sess.ID
	// 50 minutes of work today.
	if _, err := s.AddEntry(task.ID, &sid, KindWork, now.Add(-50*time.Minute), now); err != nil {
		t.Fatalf("add entry: %v", err)
	}

	today, err := s.TodayWorkSeconds()
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if today != 3000 {
		t.Fatalf("expected 3000s today, got %d", today)
	}

	ws, _ := s.TaskWorkSeconds()
	if ws[task.ID] != 3000 {
		t.Fatalf("expected task total 3000, got %d", ws[task.ID])
	}

	projStats, _ := s.ProjectBreakdown(startOfDay(now))
	if len(projStats) != 1 || projStats[0].WorkSec != 3000 {
		t.Fatalf("unexpected project breakdown: %+v", projStats)
	}

	if err := s.EndSession(sess.ID, SessionCompleted); err != nil {
		t.Fatalf("end session: %v", err)
	}
	sessions, _ := s.RecentSessions(5)
	if len(sessions) != 1 || sessions[0].Status != SessionCompleted {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
	if sessions[0].WorkedSec != 3000 {
		t.Fatalf("expected worked 3000 in summary, got %d", sessions[0].WorkedSec)
	}
	// Wall time spans from session start to end (here ~0s since the test runs
	// within a second); it must at least be populated and non-negative.
	if sessions[0].WallSec < 0 {
		t.Fatalf("expected non-negative wall, got %d", sessions[0].WallSec)
	}
}

func TestResumeEndedSession(t *testing.T) {
	s := newTestStore(t)

	// No ended sessions yet.
	if _, ok, err := s.LastEndedSession(); err != nil || ok {
		t.Fatalf("expected no ended session, got ok=%v err=%v", ok, err)
	}

	sess, err := s.CreateSession(nil, 3000, 600, 14400, 180)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Persist some live state, then end it early (abandoned).
	if err := s.SaveRuntime(sess.ID, Runtime{Phase: "work", Remaining: 1500, Cycle: 1, Accrued: 1500, SegCredited: 1500, Running: true}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}
	if err := s.EndSession(sess.ID, SessionAbandoned); err != nil {
		t.Fatalf("end session: %v", err)
	}

	// It must no longer count as active, but should be the last ended session.
	if _, ok, _ := s.ActiveSession(); ok {
		t.Fatal("ended session should not be active")
	}
	last, ok, err := s.LastEndedSession()
	if err != nil || !ok {
		t.Fatalf("expected last ended session, ok=%v err=%v", ok, err)
	}
	if last.ID != sess.ID || last.Status != SessionAbandoned {
		t.Fatalf("unexpected last ended session: %+v", last)
	}

	// Reopening makes it active again with no end time, runtime preserved.
	if err := s.ReopenSession(sess.ID); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	active, ok, err := s.ActiveSession()
	if err != nil || !ok || active.ID != sess.ID {
		t.Fatalf("expected reopened session active, got ok=%v err=%v", ok, err)
	}
	if active.EndedAt != nil {
		t.Fatal("reopened session should have no end time")
	}
	rt, err := s.LoadRuntime(sess.ID)
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if rt.Remaining != 1500 || rt.Accrued != 1500 || rt.Cycle != 1 || rt.SegCredited != 1500 {
		t.Fatalf("runtime not preserved across reopen: %+v", rt)
	}
}

func TestStreak(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("Daily", "#fff")
	task, _ := s.CreateTask(p.ID, "Practice")
	sid := int64(0)
	_ = sid

	now := time.Now()
	// Work today, yesterday, and two days ago (each 30 min) -> streak of 3.
	for _, d := range []int{0, 1, 2} {
		day := now.AddDate(0, 0, -d)
		start := time.Date(day.Year(), day.Month(), day.Day(), 10, 0, 0, 0, day.Location())
		if _, err := s.AddEntry(task.ID, nil, KindWork, start, start.Add(30*time.Minute)); err != nil {
			t.Fatalf("add entry: %v", err)
		}
	}
	streak, err := s.Streak(25 * 60)
	if err != nil {
		t.Fatalf("streak: %v", err)
	}
	if streak != 3 {
		t.Fatalf("expected streak 3, got %d", streak)
	}
}
