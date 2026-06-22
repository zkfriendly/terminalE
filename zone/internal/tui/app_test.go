package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/db"
	"github.com/zkfriendly/zone/internal/llm"
	"github.com/zkfriendly/zone/internal/session"
	"github.com/zkfriendly/zone/internal/store"
)

func testCfg() config.Config {
	c := config.Default()
	c.LMStudioEnabled = false
	return c
}

func testCfgPtr() *config.Config {
	c := testCfg()
	return &c
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

func noteEnrichedFromCmd(t *testing.T, cmd tea.Cmd) noteEnrichedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	switch msg := cmd().(type) {
	case noteEnrichedMsg:
		return msg
	case tea.BatchMsg:
		for _, sub := range msg {
			if sub == nil {
				continue
			}
			if enriched, ok := sub().(noteEnrichedMsg); ok {
				return enriched
			}
		}
		t.Fatalf("no noteEnrichedMsg in batch")
	default:
		t.Fatalf("unexpected msg type %T", msg)
	}
	return noteEnrichedMsg{}
}

func noteActionablesFromCmd(t *testing.T, cmd tea.Cmd) noteActionablesScannedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	switch msg := cmd().(type) {
	case noteActionablesScannedMsg:
		return msg
	case tea.BatchMsg:
		for _, sub := range msg {
			if sub == nil {
				continue
			}
			if scanned, ok := sub().(noteActionablesScannedMsg); ok {
				return scanned
			}
		}
		t.Fatalf("no noteActionablesScannedMsg in batch")
	default:
		t.Fatalf("unexpected msg type %T", msg)
	}
	return noteActionablesScannedMsg{}
}

func TestDashboardRenders(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	st.CreateTask(p.ID, "First task")
	app.dashboard.reload()
	sizeApp(app)

	out := app.View()
	if !strings.Contains(out.Content, "ZONE") || !strings.Contains(out.Content, "First task") {
		t.Fatalf("dashboard render missing content:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "Work") {
		t.Fatal("expected current page in bottom info bar")
	}
	if strings.Contains(out.Content, "1:Stats") {
		t.Fatal("page switcher should not be visible by default")
	}
}

func TestPageSwitcherOpensOnTab(t *testing.T) {
	app, _ := newTestApp(t)
	sizeApp(app)

	_, cmd := app.Update(keyPress("tab"))
	if cmd != nil {
		app.Update(cmd())
	}
	if !app.shell.focusNav {
		t.Fatal("tab should open page switcher")
	}
	out := app.View().Content
	for _, want := range []string{"1:Work", "2:Stats", "3:History", "4:Notes", "5:Config"} {
		if !strings.Contains(out, want) {
			t.Fatalf("page switcher missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "←→") || strings.Contains(out, "column") {
		t.Fatalf("top bar should show page switcher controls, not work actions:\n%s", out)
	}

	app.Update(keyPress("right"))
	if app.shell.navIdx != int(pageStats) {
		t.Fatalf("expected stats selected in switcher, got %d", app.shell.navIdx)
	}
	app.Update(keyPress("enter"))
	if app.shell.page != pageStats {
		t.Fatal("enter should navigate to stats")
	}
	if app.shell.focusNav {
		t.Fatal("switcher should close after selecting a page")
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
	if !strings.Contains(app.View().Content, "● focus session") {
		t.Fatal("expected active session in bottom info bar")
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
	if !strings.Contains(out, "resume available") || !strings.Contains(out, "Half-done") {
		t.Fatalf("expected resume info in bottom bar:\n%s", out)
	}
	if !strings.Contains(out, "project") || !strings.Contains(out, "tasks") {
		t.Fatalf("expected selected project info in bottom bar:\n%s", out)
	}

	// Resume last session with R.
	cmd := app.dashboard.updateNormal(keyPress("R"))
	if cmd == nil {
		t.Fatal("R should resume the last session when available")
	}
	if _, ok := cmd().(resumeLastMsg); !ok {
		t.Fatal("R should produce resumeLastMsg")
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
func TestEndConfirmationState(t *testing.T) {
	z := newZone(nil, nil, newStyles(), testCfgPtr(), session.Snapshot{
		Phase: "work", Running: true, Remaining: 1500, Planned: 3000, Cycles: 4,
	})

	z.handleKey(keyPress("E"))
	if !z.confirmingEnd {
		t.Fatal("expected E to request confirmation")
	}
	if !strings.Contains(z.render(100, 40), "end this session?") {
		t.Fatal("expected end confirmation prompt")
	}
	z.handleKey(keyPress("n"))
	if z.confirmingEnd {
		t.Fatal("n should cancel end confirmation")
	}
}

func TestSkipConfirmationState(t *testing.T) {
	z := newZone(nil, nil, newStyles(), testCfgPtr(), session.Snapshot{
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
	z := newZone(nil, nil, newStyles(), testCfgPtr(), session.Snapshot{
		Phase: "prepare", Running: true, Remaining: 180,
		TaskTitle: "Chapter one", ProjectName: "Writing",
	})
	out := z.render(100, 40)
	for _, want := range []string{
		"the zone", "glass of water",
		"Chapter one", "elapsed", "block",
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

func TestSettingsEnterViaKeyStartsEdit(t *testing.T) {
	app, _ := newTestApp(t)
	sizeApp(app)
	app.Update(gotoSettingsMsg{})

	// Work (min) is cursor 1 by default after new settings... actually cursor starts at 0
	app.settings.cursor = 1

	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		app.Update(cmd())
	}
	if !app.settings.editing {
		t.Fatal("enter on Work (min) should start editing")
	}
}

func TestNavFocusDoesNotMoveDashboard(t *testing.T) {
	app, st := newTestApp(t)
	p1, _ := st.CreateProject("Alpha", "")
	p2, _ := st.CreateProject("Beta", "")
	st.CreateTask(p1.ID, "Task A")
	st.CreateTask(p2.ID, "Task B")
	app.dashboard.reload()
	sizeApp(app)

	app.shell.focusNav = true
	app.shell.navIdx = int(pageWork)
	beforePane := app.dashboard.pane
	beforeProj := app.dashboard.selProj

	app.Update(keyPress("right"))
	if app.shell.navIdx != int(pageStats) {
		t.Fatalf("expected nav to move to stats, got idx %d", app.shell.navIdx)
	}
	if app.dashboard.pane != beforePane {
		t.Fatalf("page switcher right should not switch dashboard column (was %d, now %d)", beforePane, app.dashboard.pane)
	}
	if app.dashboard.selProj != beforeProj {
		t.Fatalf("nav right should not move project selection (was %d, now %d)", beforeProj, app.dashboard.selProj)
	}

	app.Update(keyPress("down"))
	if app.dashboard.selProj != beforeProj {
		t.Fatalf("nav down should not move project selection (was %d, now %d)", beforeProj, app.dashboard.selProj)
	}
}

func TestNavEnterOnCurrentWorkTabDoesNotStartFocus(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Alpha", "")
	st.CreateTask(p.ID, "Task A")
	app.dashboard.reload()
	sizeApp(app)

	app.shell.focusNav = true
	app.shell.navIdx = int(pageWork)

	app.Update(keyPress("enter"))
	if app.view != viewShell {
		t.Fatalf("enter on current Work tab should stay on shell, got view %d", app.view)
	}
	if app.shell.focusNav {
		t.Fatal("enter should dismiss nav focus")
	}
}

func TestSettingsEnterWorksWithNavFocus(t *testing.T) {
	app, _ := newTestApp(t)
	sizeApp(app)
	app.Update(gotoSettingsMsg{})
	app.shell.focusNav = true
	app.settings.cursor = 1

	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		app.Update(cmd())
	}
	if !app.settings.editing {
		t.Fatal("enter should edit settings field even when nav bar had focus")
	}
}

func TestSettingsView(t *testing.T) {
	app, _ := newTestApp(t)

	app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	app.Update(gotoSettingsMsg{})
	if app.view != viewShell || app.shell.page != pageSettings {
		t.Fatal("expected settings page in shell")
	}
	out := app.View()
	for _, want := range []string{"ZONE", "Config", "Application Support/zone", "Work (min)", "LM Studio URL"} {
		if !strings.Contains(out.Content, want) {
			t.Fatalf("settings render missing %q:\n%s", want, out.Content)
		}
	}

	app.settings.cursor = 1 // Work (min)
	app.settings.startInput()
	app.settings.input.SetValue("45")
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		app.Update(cmd())
	}
	if app.cfg.WorkMinutes != 45 {
		t.Fatalf("expected work minutes 45, got %d", app.cfg.WorkMinutes)
	}

	_, cmd = app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		app.Update(cmd())
	}
	if app.shell.page != pageWork {
		t.Fatal("esc should return to work page from settings")
	}
}

func TestAllNotesRenders(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Deep work")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	st.AddSessionNote(sess.ID, "remember the refactor")

	app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	app.Update(gotoNotesMsg{})
	if app.shell.page != pageNotes {
		t.Fatal("expected notes page in shell")
	}
	out := app.View().Content
	for _, want := range []string{"Notes", "Open note", "unlabeled", "Deep work"} {
		if !strings.Contains(out, want) {
			t.Fatalf("notes render missing %q:\n%s", want, out)
		}
	}
}

func TestAllNotesEditAndSave(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Deep work")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	note, _ := st.AddSessionNote(sess.ID, "remember the refactor")

	sizeApp(app)
	app.Update(gotoNotesMsg{})
	app.Update(keyPress("enter"))
	if app.allNotes.picking {
		t.Fatal("enter should open note editor")
	}
	app.allNotes.editor.Load("remember the refactor\nupdated")
	app.allNotes.saveDraft()
	got, _ := st.GetSessionNote(note.ID)
	if got.Body != "remember the refactor\nupdated" {
		t.Fatalf("expected saved body, got %q", got.Body)
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
	if app.view != viewShell || app.shell.page != pageStats {
		t.Fatal("expected stats page in shell")
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
	app.Update(gotoHistoryMsg{})
	if app.view != viewShell || app.shell.page != pageHistory {
		t.Fatal("expected history page in shell")
	}
	out := app.View()
	for _, want := range []string{"History", "Deep work", "general focus", "wall", "focus", "notes"} {
		if !strings.Contains(out.Content, want) {
			t.Fatalf("history render missing %q:\n%s", want, out.Content)
		}
	}
}

func TestZoneBackgroundReturnsToWork(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	app.view = viewZone
	app.zone = newZone(st, nil, newStyles(), &app.cfg, session.Snapshot{
		SessionID: sess.ID, Phase: "work", Running: true, Remaining: 3000,
		TaskTitle: "Focus",
	})
	sizeApp(app)

	cmd := app.zone.handleKey(keyPress("b"))
	if cmd == nil {
		t.Fatal("b should detach from zone")
	}
	app.Update(cmd())
	if app.view != viewShell {
		t.Fatalf("expected shell view after background, got %v", app.view)
	}
	if app.shell.page != pageWork {
		t.Fatal("expected work page after background")
	}
	out := app.View().Content
	if !strings.Contains(out, "Focus") || !strings.Contains(out, "Work") {
		t.Fatalf("expected dashboard in shell after background:\n%s", out)
	}
}

func TestZoneEscBackgroundReturnsToWork(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	app.view = viewZone
	app.zone = newZone(st, nil, newStyles(), &app.cfg, session.Snapshot{
		SessionID: sess.ID, Phase: "work", Running: true, Remaining: 3000,
		TaskTitle: "Focus",
	})
	sizeApp(app)

	_, cmd := app.Update(keyPress("esc"))
	if cmd == nil {
		t.Fatal("esc should detach from zone focus page")
	}
	app.Update(cmd())
	if app.view != viewShell {
		t.Fatalf("expected shell view after esc, got %v", app.view)
	}
	if app.shell.page != pageWork {
		t.Fatal("expected work page after esc")
	}
	_ = st
}

func TestZoneNotesOverlay(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	st.AddSessionNote(sess.ID, "remember to refactor")

	z := newZone(st, nil, newStyles(), testCfgPtr(), session.Snapshot{
		SessionID: sess.ID, Phase: "work", Running: true, Remaining: 3000,
		TaskTitle: "Focus",
	})
	z.width, z.height = 100, 40
	z.openNotes()

	out := z.renderNotes(100, 40)
	for _, want := range []string{"Open note", "+ new note", "unlabeled"} {
		if !strings.Contains(out, want) {
			t.Fatalf("notes should open browser by default, missing %q:\n%s", want, out)
		}
	}

	// Opening notes from zone key handler (may return label cmds for unlabeled notes).
	z.noting = false
	_ = z.handleKey(keyPress("n"))
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

	z := newZone(st, nil, newStyles(), testCfgPtr(), session.Snapshot{SessionID: sess.ID, Phase: "work"})
	z.width, z.height = 100, 40
	z.openNotes()
	if !z.notePicking {
		t.Fatal("expected note picker on open")
	}

	z.notePickIdx = 3 // row 0 = new, row 1 = session divider, row 2 = newest, row 3 = "first note"
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

func TestNoteDeleteConfirmation(t *testing.T) {
	_, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	n1, _ := st.AddSessionNote(sess.ID, "keep me")
	st.AddSessionNote(sess.ID, "delete me")

	z := newZone(st, nil, newStyles(), testCfgPtr(), session.Snapshot{SessionID: sess.ID, Phase: "work"})
	z.width, z.height = 100, 40
	z.openNotes()
	z.notePickIdx = 2 // row 0 = new, row 1 = session divider, row 2 = newest ("delete me")

	z.handleNotePickerKey("d")
	if !z.confirmingNoteDelete {
		t.Fatal("expected delete confirmation")
	}
	out := z.render(100, 40)
	if !strings.Contains(out, "delete") || !strings.Contains(out, "permanently") {
		t.Fatalf("expected delete prompt in chrome, got:\n%s", out)
	}

	z.handleNotePickerKey("n")
	if z.confirmingNoteDelete {
		t.Fatal("n should cancel delete confirmation")
	}
	notes, _ := st.ListSessionNotes(sess.ID)
	if len(notes) != 2 {
		t.Fatalf("cancel should leave notes intact, got %d", len(notes))
	}

	z.handleNotePickerKey("d")
	z.handleNotePickerKey("y")
	if z.confirmingNoteDelete {
		t.Fatal("y should clear confirmation")
	}
	notes, _ = st.ListSessionNotes(sess.ID)
	if len(notes) != 1 {
		t.Fatalf("expected 1 note after delete, got %d", len(notes))
	}
	if notes[0].ID != n1.ID {
		t.Fatalf("expected note %d to remain, got %+v", n1.ID, notes[0])
	}
}

func TestNoteLabelDisabledLeavesUnlabeled(t *testing.T) {
	_, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	cfg := config.Default()
	cfg.LMStudioEnabled = false

	z := newZone(st, nil, newStyles(), &cfg, session.Snapshot{SessionID: sess.ID, Phase: "work"})
	z.openNotes()
	z.newNote()
	z.noteEditor.Load("my fresh note")

	cmd := z.saveNoteDraft()
	if cmd == nil {
		t.Fatal("expected label cmd after save")
	}
	msg := noteEnrichedFromCmd(t, cmd)
	z.onNoteEnriched(msg)

	if z.noteLabelErr == "" {
		t.Fatal("expected disabled labeling message")
	}
	if !strings.Contains(z.noteLabelErr, "lm_studio_enabled") {
		t.Fatalf("expected config hint, got %q", z.noteLabelErr)
	}
	notes, _ := st.ListSessionNotes(sess.ID)
	if notes[0].Title != "" || notes[0].Emoji != "" {
		t.Fatalf("expected empty label on failure, got %+v", notes[0])
	}
	out := z.render(100, 40)
	if !strings.Contains(out, "label:") {
		t.Fatalf("expected label error in info bar:\n%s", out)
	}
}

func TestNoteSaveTriggersEnrich(t *testing.T) {
	llm.ResetModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"emoji\":\"💡\",\"title\":\"New idea captured\"}"}}]}`))
		}
	}))
	defer srv.Close()

	_, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	cfg := config.Default()
	cfg.LMStudioEnabled = true
	cfg.LMStudioURL = srv.URL

	z := newZone(st, nil, newStyles(), &cfg, session.Snapshot{SessionID: sess.ID, Phase: "work"})
	z.width, z.height = 100, 40
	z.openNotes()
	z.newNote()
	z.noteEditor.Load("brand new thought")

	cmd := z.saveNoteDraft()
	if cmd == nil {
		t.Fatal("expected enrich cmd after save")
	}
	msg := noteEnrichedFromCmd(t, cmd)
	z.onNoteEnriched(msg)

	notes, _ := st.ListSessionNotes(sess.ID)
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if notes[0].Title != "New idea captured" || notes[0].Emoji != "💡" {
		t.Fatalf("expected LLM label, got %+v", notes[0])
	}
	if z.noteLabelErr != "" {
		t.Fatalf("unexpected label error: %q", z.noteLabelErr)
	}
}

func TestNoteSaveTriggersActionableScan(t *testing.T) {
	llm.ResetModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"has_actionables\": true}"}}]}`))
		}
	}))
	defer srv.Close()

	_, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	cfg := config.Default()
	cfg.LMStudioEnabled = true
	cfg.LMStudioURL = srv.URL

	z := newZone(st, nil, newStyles(), &cfg, session.Snapshot{SessionID: sess.ID, Phase: "work"})
	z.openNotes()
	z.newNote()
	z.noteEditor.Load("todo: ship the feature")

	cmd := z.saveNoteDraft()
	if cmd == nil {
		t.Fatal("expected scan cmd after save")
	}
	msg := noteActionablesFromCmd(t, cmd)
	z.onNoteActionablesScanned(msg)

	notes, _ := st.ListSessionNotes(sess.ID)
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if !notes[0].HasActionables {
		t.Fatalf("expected actionable flag, got %+v", notes[0])
	}
	z.openNotePicker()
	out := z.render(100, 40)
	if !strings.Contains(out, "◆ tasks") {
		t.Fatalf("expected actionable indicator in picker:\n%s", out)
	}
}

func TestNoteEnrichErrorLeavesUnlabeled(t *testing.T) {
	llm.ResetModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
		case "/v1/chat/completions":
			http.Error(w, "server down", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	_, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	cfg := config.Default()
	cfg.LMStudioEnabled = true
	cfg.LMStudioURL = srv.URL

	z := newZone(st, nil, newStyles(), &cfg, session.Snapshot{SessionID: sess.ID, Phase: "work"})
	z.width, z.height = 100, 40
	z.openNotes()
	z.newNote()
	z.noteEditor.Load("note without label")

	cmd := z.saveNoteDraft()
	if cmd == nil {
		t.Fatal("expected enrich cmd after save")
	}
	msg := noteEnrichedFromCmd(t, cmd)
	z.onNoteEnriched(msg)

	if z.noteLabelErr == "" {
		t.Fatal("expected label error to be shown")
	}
	notes, _ := st.ListSessionNotes(sess.ID)
	if notes[0].Title != "" || notes[0].Emoji != "" {
		t.Fatalf("expected empty label on LLM error, got %+v", notes[0])
	}
	z.notePicking = true
	z.notePickIdx = notePickIdxForNote(z.noteRows, notes[0].ID)
	z.ensureNotePickVisible()
	out := z.renderNotePicker(100, 40)
	if !strings.Contains(out, "unlabeled") {
		t.Fatalf("expected unlabeled in picker:\n%s", out)
	}
}

func TestNoteEscBrowseDoesNotSave(t *testing.T) {
	_, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	n1, _ := st.AddSessionNote(sess.ID, "original text")

	z := newZone(st, nil, newStyles(), testCfgPtr(), session.Snapshot{SessionID: sess.ID, Phase: "work"})
	z.width, z.height = 100, 40
	z.openNotes()
	z.loadNote(n1)
	z.notePicking = false

	z.noteEditor.Load("temporary edit")
	_ = z.handleNotesInput(tea.KeyPressMsg{Text: "esc"}) // insert -> normal
	if z.noteEditor.mode != noteModeNormal {
		t.Fatalf("expected normal mode after esc, got %d", z.noteEditor.mode)
	}
	_ = z.handleNotesInput(tea.KeyPressMsg{Text: "esc"}) // normal -> blocked while dirty

	if z.notePicking {
		t.Fatal("esc should not browse while note has unsaved changes")
	}
	if z.noteEditor.Value() != "temporary edit" {
		t.Fatalf("expected editor to keep edits, got %q", z.noteEditor.Value())
	}

	notes, _ := st.ListSessionNotes(sess.ID)
	for _, n := range notes {
		if n.ID == n1.ID && n.Body != "original text" {
			t.Fatalf("esc to browse should not save; got body %q", n.Body)
		}
	}
}

func TestNoteEscBrowseWhenClean(t *testing.T) {
	_, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	n1, _ := st.AddSessionNote(sess.ID, "original text")

	z := newZone(st, nil, newStyles(), testCfgPtr(), session.Snapshot{SessionID: sess.ID, Phase: "work"})
	z.width, z.height = 100, 40
	z.openNotes()
	z.loadNote(n1)
	z.notePicking = false

	_ = z.handleNotesInput(tea.KeyPressMsg{Text: "esc"}) // insert -> normal
	_ = z.handleNotesInput(tea.KeyPressMsg{Text: "esc"}) // normal -> browse

	if !z.notePicking {
		t.Fatal("esc should browse when note is unchanged")
	}
	if z.editingNoteID != n1.ID {
		t.Fatalf("expected editing id to remain %d, got %d", n1.ID, z.editingNoteID)
	}
}

func TestNoteSaveQuitReturnsToPicker(t *testing.T) {
	_, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)
	st.AddSessionNote(sess.ID, "existing note")

	z := newZone(st, nil, newStyles(), testCfgPtr(), session.Snapshot{
		SessionID: sess.ID, Phase: "work", Running: true, Remaining: 1800,
		CycleIndex: 1, Cycles: 4, Accrued: 720, WallSec: 900, TodayTotal: 5400,
		TaskTitle: "Focus", ProjectName: "Demo",
	})
	z.width, z.height = 100, 40
	z.openNotes()
	z.newNote()
	z.noteEditor.Load("draft note")

	z.noteEditor.Update(tea.KeyPressMsg{Text: "esc"})
	z.noteEditor.Update(tea.KeyPressMsg{Text: ":"})
	z.noteEditor.Update(tea.KeyPressMsg{Text: "w"})
	z.noteEditor.Update(tea.KeyPressMsg{Text: "q"})
	_ = z.handleNotesInput(tea.KeyPressMsg{Text: "enter"})

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
	for _, want := range []string{formatClock(1800), "2/4", "12m", "elapsed", "Focus", "today"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected status bar to include %q:\n%s", want, out)
		}
	}
	if got, want := lipgloss.Height(out), 40; got != want {
		t.Fatalf("notes view height = %d, want %d (status bar must fit on screen)", got, want)
	}
}

func TestNotesPickerEscIsLocal(t *testing.T) {
	_, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	app, _ := newTestApp(t)
	app.view = viewZone
	app.zone = newZone(st, nil, newStyles(), &app.cfg, session.Snapshot{SessionID: sess.ID})
	app.zone.openNotes()
	if !app.zone.notePicking {
		t.Fatal("expected notes picker")
	}
	if !app.escIsLocal() {
		t.Fatal("esc should stay local on notes picker (close notes, not quit)")
	}

	app.zone.newNote()
	if !app.escIsLocal() {
		t.Fatal("esc should stay local while editing a note")
	}
}

func TestResumeNotesPersisted(t *testing.T) {
	app, st := newTestApp(t)
	p, _ := st.CreateProject("Demo", "")
	task, _ := st.CreateTask(p.ID, "Focus")
	sess, _ := st.CreateSession(&task.ID, 3000, 600, 14400, 180)

	app.view = viewZone
	app.zone = newZone(st, nil, newStyles(), &app.cfg, session.Snapshot{SessionID: sess.ID})
	app.zone.openNotes()

	app.persistResumeNotesBeforeQuit()
	if app.cfg.ResumeNotesSessionID != sess.ID {
		t.Fatalf("expected resume hint for session %d, got %d", sess.ID, app.cfg.ResumeNotesSessionID)
	}

	// Re-attaching the same session should reopen the notes browser.
	app.zone = newZone(st, nil, newStyles(), &app.cfg, session.Snapshot{SessionID: sess.ID})
	if app.cfg.ResumeNotesSessionID == sess.ID {
		app.zone.openNotes()
		app.cfg.ResumeNotesSessionID = 0
	}
	if !app.zone.noting || !app.zone.notePicking {
		t.Fatal("expected notes browser restored on reconnect")
	}

	app.zone.closeNotes(false)
	if app.cfg.ResumeNotesSessionID != 0 {
		t.Fatal("closing notes should clear resume hint")
	}
}
