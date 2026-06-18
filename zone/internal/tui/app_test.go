package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/db"
	"github.com/zkfriendly/zone/internal/session"
	"github.com/zkfriendly/zone/internal/store"
)

func testCfg() config.Config {
	c := config.Default()
	c.LMStudioEnabled = false
	return c
}

func newTestApp(t *testing.T) (*App, *store.Store) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	st := store.New(database)
	return NewApp(st, testCfg()), st
}

func sizeApp(app *App) {
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
}

func keyPress(s string) tea.KeyPressMsg {
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func TestDashboardRenders(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	st.CreateTask(p.ID, "First task")
	app.dashboard.reload()
	sizeApp(app)

	out := app.View()
	if !strings.Contains(out.Content, "zone") || !strings.Contains(out.Content, "First task") {
		t.Fatalf("dashboard render missing content:\n%s", out.Content)
	}
}

func TestDashboardShowsActiveSessionBanner(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Deep work")
	// Simulate a running background session in the database.
	st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	app.dashboard.reload()
	sizeApp(app)
	if !app.dashboard.hasActive {
		t.Fatal("expected dashboard to detect the active session")
	}
	if !strings.Contains(app.View().Content, "focus session running") {
		t.Fatal("expected running-session banner")
	}
}

func TestDashboardShowsResumeBanner(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Half-done")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	st.SaveRuntime(sess.ID, store.Runtime{Phase: "work", Remaining: 1500, Cycle: 1, Accrued: 1500, Running: true})
	st.EndSession(sess.ID, store.SessionAbandoned)

	app.dashboard.reload()
	sizeApp(app)

	if app.dashboard.hasActive {
		t.Fatal("ended session should not be active")
	}
	if !app.dashboard.canResume {
		t.Fatal("expected resume to be available for an abandoned session")
	}
	out := app.View().Content
	if !strings.Contains(out, "ended early") || !strings.Contains(out, "Half-done") {
		t.Fatalf("expected resume banner:\n%s", out)
	}

	// Pressing R should produce a resumeLastMsg.
	cmd := app.dashboard.update(keyPress("R"))
	if cmd == nil {
		t.Fatal("R produced no command")
	}
	if _, ok := cmd().(resumeLastMsg); !ok {
		t.Fatal("R should request resuming the last session")
	}

	// A completed (not abandoned) latest session must NOT offer resume.
	st.ReopenSession(sess.ID)
	st.EndSession(sess.ID, store.SessionCompleted)
	app.dashboard.reload()
	if app.dashboard.canResume {
		t.Fatal("completed sessions should not be resumable")
	}
}

func TestDashboardFooterFitsNarrowWidth(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	st.CreateTask(p.ID, "A task")
	app.dashboard.reload()

	for _, w := range []int{40, 60, 80, 120} {
		app.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		out := app.View().Content
		for _, line := range strings.Split(out, "\n") {
			if lipgloss.Width(line) > w {
				t.Fatalf("width=%d: line exceeds terminal (%d cells): %q",
					w, lipgloss.Width(line), line)
			}
		}
	}
}

// TestSkipConfirmationState verifies the confirm/cancel state machine in the
// zone view without a live daemon (client is nil; commands are no-ops).
func TestSkipConfirmationState(t *testing.T) {
	z := newZone(nil, nil, newStyles(), testCfg(), session.Snapshot{
		Phase: "work", Running: true, Remaining: 1500, Planned: 3000, Cycles: 4,
	})

	z.handleKey(keyPress("s"))
	if !z.confirmingSkip {
		t.Fatal("expected s to request confirmation")
	}
	if !strings.Contains(z.render(100, 40), "skip this block?") {
		t.Fatal("expected confirmation prompt")
	}
	z.handleKey(keyPress("n"))
	if z.confirmingSkip {
		t.Fatal("n should cancel the skip")
	}
	z.handleKey(keyPress("s"))
	z.handleKey(keyPress("y"))
	if z.confirmingSkip {
		t.Fatal("y should clear the confirmation")
	}
}

func TestPrepareScreenIsFriendly(t *testing.T) {
	z := newZone(nil, nil, newStyles(), testCfg(), session.Snapshot{
		Phase: "prepare", Running: true, Remaining: 180,
		TaskTitle: "Chapter one", ProjectName: "Writing",
	})
	out := z.render(100, 40)
	for _, want := range []string{
		"the zone", "glass of water", "rising chime", "finishing chime",
		"Chapter one", "focus begins in",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("prepare screen missing %q:\n%s", want, out)
		}
	}
	// Should fit a normal-width terminal without clipping.
	for _, w := range []int{80, 100, 120} {
		for _, line := range strings.Split(z.render(w, 40), "\n") {
			if lipgloss.Width(line) > w {
				t.Fatalf("width=%d: prepare line too wide (%d): %q", w, lipgloss.Width(line), line)
			}
		}
	}
}

func TestStatsRenders(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Task")
	now := time.Now()
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	sid := sess.ID
	st.AddEntry(task.ID, &sid, store.KindWork, now.Add(-30*time.Minute), now)
	st.EndSession(sess.ID, store.SessionCompleted)

	app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	app.Update(gotoStatsMsg{})
	if app.view != viewStats {
		t.Fatal("expected stats view")
	}
	out := app.View()
	if !strings.Contains(out.Content, "Recent sessions") {
		t.Fatalf("stats render missing content:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "wall") {
		t.Fatalf("stats should mention wall time:\n%s", out.Content)
	}
}

func TestHistoryRenders(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Deep work")
	now := time.Now()
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	sid := sess.ID
	st.AddEntry(task.ID, &sid, store.KindWork, now.Add(-40*time.Minute), now.Add(-10*time.Minute))
	st.EndSession(sess.ID, store.SessionCompleted)
	// A general (no-task) session too.
	st.CreateSession(nil, 3000, 600, 14400, 180)

	app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	// Pressing 'h' in stats should route to the history view.
	app.Update(gotoStatsMsg{})
	if cmd := app.stats.update(keyPress("h")); cmd != nil {
		if _, ok := cmd().(gotoHistoryMsg); !ok {
			t.Fatal("stats 'h' should open history")
		}
	} else {
		t.Fatal("stats 'h' produced no command")
	}

	app.Update(gotoHistoryMsg{})
	if app.view != viewHistory {
		t.Fatal("expected history view")
	}
	out := app.View()
	for _, want := range []string{"session history", "Deep work", "general focus", "wall", "focus", "notes"} {
		if !strings.Contains(out.Content, want) {
			t.Fatalf("history render missing %q:\n%s", want, out.Content)
		}
	}
}

func TestZoneNotesOverlay(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	st.AddSessionNote(sess.ID, "remember to refactor")

	z := newZone(st, nil, newStyles(), testCfg(), session.Snapshot{
		SessionID: sess.ID, Phase: "work", Running: true, Remaining: 3000,
		TaskTitle: "Focus",
	})
	z.width, z.height = 100, 40
	z.openNotes()

	out := z.renderNotes(100, 40)
	for _, want := range []string{"Open note", "+ new note", "remember to refactor"} {
		if !strings.Contains(out, want) {
			t.Fatalf("notes should open browser by default, missing %q:\n%s", want, out)
		}
	}

	// Opening notes from zone key handler.
	z.noting = false
	cmd := z.handleKey(keyPress("n"))
	if cmd != nil {
		t.Fatal("open notes should not return cmd")
	}
	if !z.noting || !z.notePicking {
		t.Fatal("expected noting mode with picker open after n")
	}
	_ = app
}

func TestNotePickerAndEdit(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	n1, _ := st.AddSessionNote(sess.ID, "first note")
	st.AddSessionNote(sess.ID, "second note")

	z := newZone(st, nil, newStyles(), testCfg(), session.Snapshot{SessionID: sess.ID, Phase: "work"})
	z.width, z.height = 100, 40
	z.openNotes()
	if !z.notePicking {
		t.Fatal("expected note picker on open")
	}

	z.notePickIdx = 2 // row 0 = new, row 1 = newest ("second note"), row 2 = "first note"
	z.handleNotePickerKey("enter")
	if z.editingNoteID != n1.ID {
		t.Fatalf("expected to load first note id %d, got %d", n1.ID, z.editingNoteID)
	}
	if z.noteEditor.Value() != "first note" {
		t.Fatalf("expected note body loaded, got %q", z.noteEditor.Value())
	}

	z.noteEditor.Load("first note edited")
	z.saveNoteDraft()
	notes, _ := st.ListSessionNotes(sess.ID)
	var found bool
	for _, n := range notes {
		if n.ID == n1.ID {
			found = true
			if n.Body != "first note edited" {
				t.Fatalf("expected updated note, got %q", n.Body)
			}
		}
	}
	if !found {
		t.Fatalf("note %d not found in %+v", n1.ID, notes)
	}
	_ = app
}

func TestNoteSaveQuitReturnsToPicker(t *testing.T) {
	_, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	st.AddSessionNote(sess.ID, "existing note")

	z := newZone(st, nil, newStyles(), testCfg(), session.Snapshot{
		SessionID: sess.ID, Phase: "work", Running: true, Remaining: 1800,
		TaskTitle: "Focus",
	})
	z.width, z.height = 100, 40
	z.openNotes()
	z.newNote()
	z.noteEditor.Load("draft note")

	z.noteEditor.Update(tea.KeyPressMsg{Text: "esc"})
	z.noteEditor.Update(tea.KeyPressMsg{Text: ":"})
	z.noteEditor.Update(tea.KeyPressMsg{Text: "w"})
	z.noteEditor.Update(tea.KeyPressMsg{Text: "q"})
	_ = z.handleNotesKey(tea.KeyPressMsg{Text: "enter"})

	if !z.noting {
		t.Fatal(":wq should stay in notes mode")
	}
	if !z.notePicking {
		t.Fatal(":wq should return to note picker")
	}
	if z.noteEditor.Value() != "" {
		t.Fatal("editor should be cleared after :wq")
	}

	out := z.render(100, 40)
	if !strings.Contains(out, "Open note") {
		t.Fatalf("expected note picker after :wq:\n%s", out)
	}
	if !strings.Contains(out, formatClock(1800)) {
		t.Fatalf("expected timer bar on notes overlay:\n%s", out)
	}
}
