// Package tui implements zone's terminal UI: a dashboard for projects/tasks, a
// full-screen pomodoro focus "zone", and a stats view. It is built on Bubble Tea
// v2 (declarative views) and Lip Gloss v2.
package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/session"
	"github.com/zkfriendly/zone/internal/store"
)

type viewID int

const (
	viewDashboard viewID = iota
	viewZone
	viewStats
)

// Messages used for navigation between views.
type (
	tickMsg          time.Time
	startSessionMsg  struct{ task store.Task }
	resumeSessionMsg struct{}
	gotoStatsMsg     struct{}
	gotoDashboardMsg struct{}
	quitMsg          struct{}
)

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// App is the root Bubble Tea model.
type App struct {
	store  *store.Store
	cfg    config.Config
	styles Styles

	width, height int
	view          viewID

	dashboard *dashboard
	zone      *zoneView
	stats     *statsView
}

// NewApp builds the root model. If a focus session is already running (e.g. it
// was started before the terminal was closed), it auto-attaches to it.
func NewApp(st *store.Store, cfg config.Config) *App {
	s := newStyles()
	m := &App{
		store:     st,
		cfg:       cfg,
		styles:    s,
		view:      viewDashboard,
		dashboard: newDashboard(st, s),
	}
	if sess, ok, err := st.ActiveSession(); err == nil && ok {
		m.attach(sess)
	}
	return m
}

// Init implements tea.Model.
func (m *App) Init() tea.Cmd {
	return tickCmd()
}

// Update implements tea.Model.
func (m *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.shutdown()
			return m, tea.Quit
		}
		return m, m.updateActive(msg)

	case tickMsg:
		cmd := m.updateActive(msg)
		return m, tea.Batch(cmd, tickCmd())

	case startSessionMsg:
		return m, m.startSession(msg.task)

	case resumeSessionMsg:
		if sess, ok, err := m.store.ActiveSession(); err == nil && ok {
			m.attach(sess)
		} else {
			m.dashboard.reload()
		}
		return m, nil

	case gotoStatsMsg:
		m.stats = newStats(m.store, m.styles)
		m.view = viewStats
		return m, nil

	case gotoDashboardMsg:
		m.dashboard.reload()
		m.view = viewDashboard
		return m, nil

	case quitMsg:
		m.shutdown()
		return m, tea.Quit
	}

	return m, m.updateActive(msg)
}

// View implements tea.Model.
func (m *App) View() tea.View {
	var content string
	switch m.view {
	case viewZone:
		content = m.zone.render(m.width, m.height)
	case viewStats:
		content = m.stats.render(m.width, m.height)
	default:
		content = m.dashboard.render(m.width, m.height)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.BackgroundColor = colBG
	return v
}

func (m *App) updateActive(msg tea.Msg) tea.Cmd {
	switch m.view {
	case viewZone:
		if m.zone != nil {
			return m.zone.update(msg)
		}
	case viewStats:
		if m.stats != nil {
			return m.stats.update(msg)
		}
	default:
		if m.dashboard != nil {
			return m.dashboard.update(msg)
		}
	}
	return nil
}

func (m *App) startSession(t store.Task) tea.Cmd {
	// Stop any standalone tracking before entering a focus session.
	m.dashboard.stopTracking()

	// Only one focus session at a time: attach to an existing one if present.
	if sess, ok, err := m.store.ActiveSession(); err == nil && ok {
		m.attach(sess)
		return nil
	}

	sess, err := m.store.CreateSession(t.ID, m.cfg.WorkSec(), m.cfg.BreakSec(), m.cfg.TotalSec(), m.cfg.PrepareSec())
	if err != nil {
		m.dashboard.err = err
		return nil
	}
	m.attach(sess)
	return nil
}

// attach spawns/connects the focus daemon for sess and switches to the zone view.
func (m *App) attach(sess store.Session) {
	client, err := session.EnsureDaemon(sess.ID)
	if err != nil {
		// Couldn't run the daemon; abandon the session so it doesn't linger.
		_ = m.store.EndSession(sess.ID, store.SessionAbandoned)
		m.dashboard.err = err
		m.view = viewDashboard
		return
	}
	snap, _ := client.Status()
	m.zone = newZone(client, m.styles, snap)
	m.view = viewZone
}

// shutdown cleanly closes the UI before quitting. The focus daemon is left
// running in the background; quitting the UI never ends a session.
func (m *App) shutdown() {
	if m.zone != nil && m.zone.client != nil {
		m.zone.client.Close()
	}
	if m.dashboard != nil {
		m.dashboard.stopTracking()
	}
}
