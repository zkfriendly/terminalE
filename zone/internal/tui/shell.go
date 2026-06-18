package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/config"
)

type shellPage int

const (
	pageWork shellPage = iota
	pageStats
	pageHistory
	pageSettings
)

type shellNavItem struct {
	label string
	desc  string
}

var shellNavItems = []shellNavItem{
	{label: "Work", desc: "projects & tasks"},
	{label: "Stats", desc: "focus overview"},
	{label: "History", desc: "session log"},
	{label: "Config", desc: "settings file"},
}

// shell is the OS-like frame around non-focus views.
type shell struct {
	styles Styles
	page   shellPage
	navIdx int  // cursor in page nav
	focusNav bool // true = page nav, false = content

	width, height int
	clock         time.Time
	status        string // session state shown in bottom bar
}

func newShell(s Styles) *shell {
	return &shell{styles: s, page: pageWork}
}

func shellToggleFocusCmd() tea.Cmd {
	return func() tea.Msg { return shellToggleFocusMsg{} }
}

func (sh *shell) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tickMsg:
		sh.clock = time.Time(msg)
		return nil
	case tea.KeyPressMsg:
		if sh.focusNav {
			return sh.updateNav(msg)
		}
	}
	return nil
}

func (sh *shell) updateNav(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "left", "h", "up", "k":
		sh.navIdx = clampInt(sh.navIdx-1, 0, len(shellNavItems)-1)
	case "right", "l", "down", "j":
		sh.navIdx = clampInt(sh.navIdx+1, 0, len(shellNavItems)-1)
	case "enter", " ", "space":
		next := shellPage(sh.navIdx)
		sh.focusNav = false
		if next != sh.page {
			sh.page = next
			return sh.pageCmd()
		}
		return nil
	case "tab":
		sh.focusNav = false
	}
	return nil
}

func (sh *shell) pageCmd() tea.Cmd {
	switch sh.page {
	case pageStats:
		return func() tea.Msg { return gotoStatsMsg{} }
	case pageHistory:
		return func() tea.Msg { return gotoHistoryMsg{} }
	case pageSettings:
		return func() tea.Msg { return gotoSettingsMsg{} }
	default:
		return func() tea.Msg { return gotoWorkMsg{} }
	}
}

func (sh *shell) goWork() {
	sh.page = pageWork
	sh.navIdx = int(pageWork)
}

func (sh *shell) syncPage(p shellPage) {
	sh.page = p
	sh.navIdx = int(p)
}

// handleEsc returns true if esc was consumed (navigate back), false if caller should quit.
func (sh *shell) handleEsc() bool {
	if sh.focusNav {
		sh.focusNav = false
		return true
	}
	if sh.page != pageWork {
		sh.goWork()
		return true
	}
	return false
}

func (sh *shell) toggleFocus() {
	sh.focusNav = !sh.focusNav
	if sh.focusNav {
		sh.navIdx = int(sh.page)
	}
}

func (sh *shell) chromeRows() int {
	// action bar + page nav + bottom info bar (approximate).
	return 5
}

func (sh *shell) contentSize() (int, int) {
	w := sh.width
	if w < 20 {
		w = 20
	}
	h := sh.height - sh.chromeRows()
	if h < 5 {
		h = 5
	}
	return w, h
}

func (sh *shell) render(body string, actionHints, infoHints []string, cfg config.Config) string {
	if sh.width == 0 {
		return "loading..."
	}
	s := sh.styles

	allActions := append(sh.globalActionHints(), actionHints...)
	actionBar := renderActionBar(s, allActions, sh.width)
	navStrip := sh.renderNavStrip()
	info := append(sh.globalInfoHints(cfg), infoHints...)
	bottomBar := renderInfoBar(s, info, sh.width)

	return composeShellLayout(actionBar, navStrip, body, bottomBar, sh.width, sh.height)
}

func (sh *shell) globalActionHints() []string {
	s := sh.styles
	return []string{
		s.helpEntry("tab", "pages/content"),
		s.helpEntry("←→", "pages"),
		s.helpEntry("enter", "open"),
		s.helpEntry("esc", sh.escHint()),
	}
}

func (sh *shell) globalInfoHints(cfg config.Config) []string {
	s := sh.styles
	hints := []string{
		s.Title.Render("ZONE"),
		renderLLMStatus(s, cfg),
	}
	if !sh.clock.IsZero() {
		hints = append(hints, s.Dim.Render(sh.clock.Format("15:04:05")))
	}
	if sh.status != "" {
		hints = append(hints, sh.status)
	}
	return hints
}

func (sh *shell) escHint() string {
	if sh.page != pageWork {
		return "back"
	}
	return "quit"
}

func (sh *shell) renderNavStrip() string {
	s := sh.styles
	sep := s.Dim.Render("   ")
	var tabs []string
	for i, item := range shellNavItems {
		label := item.label
		switch {
		case sh.focusNav && i == sh.navIdx:
			label = s.ItemSel.Render(" "+item.label+" ")
		case shellPage(i) == sh.page:
			label = s.Item.Render("[" + item.label + "]")
		default:
			label = s.Dim.Render(item.label)
		}
		tabs = append(tabs, label)
	}
	line := strings.Join(tabs, sep)
	if sh.focusNav {
		line += "  " + s.Dim.Render("— "+shellNavItems[sh.navIdx].desc)
	}

	style := lipgloss.NewStyle().Width(sh.width)
	if sh.focusNav {
		style = style.
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(colAccent)
	}
	return style.Render(line)
}

func (sh *shell) setStatus(hasActive bool, activeTask, activeProj string, tracking bool) {
	s := sh.styles
	switch {
	case hasActive:
		label := activeTask
		if activeProj != "" {
			label += " · " + activeProj
		}
		sh.status = s.Work.Render("● focus") + " " + s.Dim.Render(truncate(label, 32))
	case tracking:
		sh.status = s.Work.Render("● tracking")
	default:
		sh.status = ""
	}
}

func shellPageTitle(p shellPage) string {
	return shellNavItems[p].label
}
