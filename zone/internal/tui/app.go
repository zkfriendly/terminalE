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
	viewShell viewID = iota
	viewZone
)

// Messages used for navigation between views.
type (
	tickMsg              time.Time
	startSessionMsg      struct{ task *store.Task }
	resumeSessionMsg     struct{ task *store.Task }
	resumeLastMsg        struct{}
	gotoStatsMsg         struct{}
	gotoHistoryMsg       struct{}
	gotoNotesMsg         struct{}
	gotoSettingsMsg      struct{}
	gotoWorkMsg          struct{}
	shellToggleFocusMsg  struct{}
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

	shell     *shell
	dashboard *dashboard
	zone      *zoneView
	stats     *statsView
	history   *historyView
	allNotes  *allNotesView
	settings  *settingsView
}

// NewApp builds the root model. If a focus session is already running (e.g. it
// was started before the terminal was closed), it auto-attaches to it.
func NewApp(st *store.Store, cfg config.Config) *App {
	s := newStyles()
	m := &App{
		store:     st,
		cfg:       cfg,
		styles:    s,
		view:      viewShell,
		shell:     newShell(s),
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
		if msg.String() == "esc" && !m.escIsLocal() {
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
			if msg.task != nil && m.zone != nil && m.zone.client != nil {
				if snap, err := m.zone.client.SetTask(msg.task.ID); err == nil {
					m.zone.snap = snap
				}
			}
		} else {
			m.dashboard.reload()
		}
		return m, nil

	case resumeLastMsg:
		m.resumeLast()
		return m, nil

	case gotoStatsMsg:
		m.shell.syncPage(pageStats)
		m.shell.focusNav = false
		m.stats = newStats(m.store, m.styles)
		return m, nil

	case gotoHistoryMsg:
		m.shell.syncPage(pageHistory)
		m.shell.focusNav = false
		m.history = newHistory(m.store, m.styles)
		return m, nil

	case gotoNotesMsg:
		m.shell.syncPage(pageNotes)
		m.shell.focusNav = false
		m.allNotes = newAllNotes(m.store, &m.cfg, m.styles)
		return m, m.allNotes.initScanCmd()

	case gotoSettingsMsg:
		path, _ := config.Path()
		m.shell.syncPage(pageSettings)
		m.shell.focusNav = false
		m.settings = newSettings(&m.cfg, m.styles, path)
		return m, nil

	case gotoWorkMsg:
		m.shell.goWork()
		m.shell.focusNav = false
		m.view = viewShell
		m.dashboard.reload()
		return m, nil

	case shellToggleFocusMsg:
		if m.shell != nil {
			m.shell.toggleFocus()
		}
		return m, nil

	case noteEnrichedMsg:
		if m.zone != nil {
			m.zone.onNoteEnriched(msg)
		}
		if m.allNotes != nil {
			m.allNotes.onNoteEnriched(msg)
		}
		return m, nil

	case noteActionablesScannedMsg:
		if m.zone != nil {
			m.zone.onNoteActionablesScanned(msg)
		}
		if m.allNotes != nil {
			m.allNotes.onNoteActionablesScanned(msg)
		}
		return m, nil

	case noteActionablesExtractedMsg:
		if m.zone != nil {
			m.zone.onNoteActionablesExtracted(msg)
		}
		if m.allNotes != nil {
			m.allNotes.onNoteActionablesExtracted(msg)
		}
		return m, nil
	}

	return m, m.updateActive(msg)
}

// View implements tea.Model.
func (m *App) View() tea.View {
	var content string
	switch m.view {
	case viewZone:
		content = m.zone.render(m.width, m.height)
	default:
		content = m.renderShell()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.BackgroundColor = colBG
	return v
}

func (m *App) renderShell() string {
	m.shell.width = m.width
	m.shell.height = m.height
	m.shell.setStatus(m.dashboard.hasActive, m.dashboard.activeTask, m.dashboard.activeProj, m.dashboard.tracking)

	w, h := m.shell.contentSize()
	var body string
	var actions, info []string
	switch m.shell.page {
	case pageStats:
		if m.stats == nil {
			m.stats = newStats(m.store, m.styles)
		}
		body = m.stats.renderBody(w, h)
		actions = m.stats.actionHints()
		info = m.stats.infoHints()
	case pageHistory:
		if m.history == nil {
			m.history = newHistory(m.store, m.styles)
		}
		body = m.history.renderBody(w, h)
		actions = m.history.actionHints()
		info = m.history.infoHints()
	case pageNotes:
		if m.allNotes == nil {
			m.allNotes = newAllNotes(m.store, &m.cfg, m.styles)
		}
		body = m.allNotes.renderBody(w, h)
		actions = m.allNotes.actionHints()
		info = m.allNotes.infoHints()
	case pageSettings:
		if m.settings == nil {
			path, _ := config.Path()
			m.settings = newSettings(&m.cfg, m.styles, path)
		}
		body = m.settings.renderBody(w, h)
		actions = m.settings.actionHints()
		info = m.settings.infoHints()
	default:
		body = m.dashboard.renderBody(w, h)
		actions = m.dashboard.actionHints()
		info = m.dashboard.infoHints()
	}
	return m.shell.render(body, actions, info, m.cfg)
}

// escIsLocal reports whether esc should be handled by the active view rather than quitting.
func (m *App) escIsLocal() bool {
	switch m.view {
	case viewZone:
		if m.zone == nil {
			return false
		}
		return true
	case viewShell:
		if m.shell == nil {
			return false
		}
		if m.shell.focusNav {
			return true
		}
		if m.shellContentEscLocal() {
			return true
		}
		if m.shell.page != pageWork {
			return true
		}
	}
	return false
}

func (m *App) shellContentEscLocal() bool {
	switch m.shell.page {
	case pageWork:
		return m.dashboard != nil && m.dashboard.escIsLocal()
	case pageHistory:
		return m.history != nil && m.history.escIsLocal()
	case pageNotes:
		return m.allNotes != nil && m.allNotes.escIsLocal()
	case pageSettings:
		return m.settings != nil && m.settings.escIsLocal()
	}
	return false
}

func (m *App) updateActive(msg tea.Msg) tea.Cmd {
	switch m.view {
	case viewZone:
		if m.zone != nil {
			return m.zone.update(msg)
		}
	default:
		return m.updateShell(msg)
	}
	return nil
}

func (m *App) updateShell(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tickMsg); ok && m.shell != nil {
		m.shell.update(msg)
	}

	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "esc" {
		if m.shell.focusNav {
			m.shell.focusNav = false
			return nil
		}
		if m.shell.page != pageWork && !m.shellContentEscLocal() {
			m.shell.handleEsc()
			return nil
		}
	}

	if m.shell.focusNav {
		cmd := m.shell.update(msg)
		if cmd != nil {
			return cmd
		}
		if m.shell.focusNav {
			// Nav consumed the key (e.g. ←→); do not forward to page content.
			return nil
		}
		// Enter/space/tab on the already-active tab only dismisses nav focus.
		// Do not forward those keys to page content (Work would start focus).
		if key, ok := msg.(tea.KeyPressMsg); ok && m.shell.navIdx == int(m.shell.page) {
			switch key.String() {
			case "tab":
				return nil
			case "enter":
				if m.shell.page != pageSettings {
					return nil
				}
			case " ", "space":
				if m.shell.page != pageSettings {
					return nil
				}
			}
		}
	}

	switch m.shell.page {
	case pageStats:
		if m.stats == nil {
			m.stats = newStats(m.store, m.styles)
		}
		return m.stats.update(msg)
	case pageHistory:
		if m.history == nil {
			m.history = newHistory(m.store, m.styles)
		}
		return m.history.update(msg)
	case pageNotes:
		if m.allNotes == nil {
			m.allNotes = newAllNotes(m.store, &m.cfg, m.styles)
		}
		return m.allNotes.update(msg)
	case pageSettings:
		if m.settings == nil {
			path, _ := config.Path()
			m.settings = newSettings(&m.cfg, m.styles, path)
		}
		return m.settings.update(msg)
	default:
		return m.dashboard.update(msg)
	}
}

func (m *App) startSession(t *store.Task) tea.Cmd {
	m.dashboard.stopTracking()

	if sess, ok, err := m.store.ActiveSession(); err == nil && ok {
		m.attach(sess)
		return nil
	}

	var taskID *int64
	if t != nil {
		id := t.ID
		taskID = &id
	}
	sess, err := m.store.CreateSession(taskID, m.cfg.WorkSec(), m.cfg.BreakSec(), m.cfg.TotalSec(), m.cfg.PrepareSec())
	if err != nil {
		m.dashboard.err = err
		return nil
	}
	m.attach(sess)
	return nil
}

func (m *App) resumeLast() {
	sess, ok, err := m.store.LastEndedSession()
	if err != nil || !ok || sess.Status != store.SessionAbandoned {
		m.dashboard.reload()
		return
	}
	if err := m.store.ReopenSession(sess.ID); err != nil {
		m.dashboard.err = err
		return
	}
	sess.Status = store.SessionActive
	m.connect(sess, session.ResumeDaemon)
}

func (m *App) attach(sess store.Session) {
	m.connect(sess, session.EnsureDaemon)
}

func (m *App) connect(sess store.Session, ensure func(int64) (*session.Client, error)) {
	client, err := ensure(sess.ID)
	if err != nil {
		_ = m.store.EndSession(sess.ID, store.SessionAbandoned)
		m.dashboard.err = err
		m.view = viewShell
		return
	}
	snap, _ := client.Status()
	m.zone = newZone(m.store, client, m.styles, &m.cfg, snap)
	if m.cfg.ResumeNotesSessionID == sess.ID {
		m.zone.openNotes()
		m.zone.deferActionableScan = true
		m.cfg.ResumeNotesSessionID = 0
		_ = m.cfg.Save()
	}
	m.view = viewZone
}

func (m *App) persistResumeNotesBeforeQuit() {
	if m.zone != nil && m.zone.noting && m.zone.notePicking && m.zone.snap.SessionID != 0 {
		m.cfg.ResumeNotesSessionID = m.zone.snap.SessionID
	} else {
		m.cfg.ResumeNotesSessionID = 0
	}
	_ = m.cfg.Save()
}

func (m *App) shutdown() {
	m.persistResumeNotesBeforeQuit()
	if m.zone != nil && m.zone.client != nil {
		m.zone.client.Close()
	}
	if m.dashboard != nil {
		m.dashboard.stopTracking()
	}
}
