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

func newTestApp(t *testing.T) (*App, *store.Store) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	st := store.New(database)
	return NewApp(st, config.Default()), st
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
	st.CreateSession(task.ID, 3000, 600, 14400, 180)

	app.dashboard.reload()
	sizeApp(app)
	if !app.dashboard.hasActive {
		t.Fatal("expected dashboard to detect the active session")
	}
	if !strings.Contains(app.View().Content, "focus session running") {
		t.Fatal("expected running-session banner")
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
	z := newZone(nil, newStyles(), session.Snapshot{
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
	z := newZone(nil, newStyles(), session.Snapshot{
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
	st.AddEntry(task.ID, nil, store.KindWork, now.Add(-30*time.Minute), now)

	app.Update(gotoStatsMsg{})
	if app.view != viewStats {
		t.Fatal("expected stats view")
	}
	out := app.View()
	if !strings.Contains(out.Content, "stats") {
		t.Fatalf("stats render missing content:\n%s", out.Content)
	}
}
