package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/session"
	"github.com/zkfriendly/zone/internal/store"
)

// zoneView is the full-screen pomodoro focus experience. It is a thin client of
// the focus daemon: it renders snapshots and sends commands; the daemon owns the
// timer and audio and keeps running even if this UI (or the whole terminal) is
// closed.
type zoneView struct {
	styles Styles
	store  *store.Store
	cfg    *config.Config
	client *session.Client

	snap          session.Snapshot
	width, height int

	confirmingSkip bool
	confirmingEnd  bool
	disconnected   bool

	// Task switcher overlay.
	picking bool
	tasks   []store.Task // picker entries; index 0 is the "no task" option
	pickIdx int

	// Session notes overlay.
	noting         bool
	noteEditor     vimNoteEditor
	noteRows       []noteBrowseRow
	editingNoteID  int64 // 0 = composing a new note
	notePicking     bool
	notePickIdx     int
	notePickOffset       int // first visible row in the browse list
	enrichingNotes       map[int64]bool // notes waiting on LM Studio
	scanningActionables  map[int64]bool // notes waiting on actionable scan
	deferActionableScan  bool           // run pending scan on next tick (resume path)
	noteLabelErr         string         // last labeling error (shown in picker)
	confirmingNoteDelete bool

	viewingActionables    bool
	actionablesNoteID     int64
	actionablesTitle      string
	actionablesItems      []string
	extractingActionables bool
	actionablesExtractErr string
}

type noteEnrichedMsg struct {
	noteID int64
	note   store.SessionNote
	err    error
}

func newZone(st *store.Store, client *session.Client, s Styles, cfg *config.Config, initial session.Snapshot) *zoneView {
	return &zoneView{
		store:          st,
		cfg:            cfg,
		styles:         s,
		client:         client,
		snap:           initial,
		enrichingNotes:      map[int64]bool{},
		scanningActionables: map[int64]bool{},
	}
}

func (z *zoneView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tickMsg:
		z.refresh()
		if z.deferActionableScan {
			z.deferActionableScan = false
			return tea.Batch(z.enrichPendingCmd(), z.scanPendingActionablesCmd())
		}
		return nil
	case noteEnrichedMsg:
		z.onNoteEnriched(msg)
		return nil
	case noteActionablesScannedMsg:
		z.onNoteActionablesScanned(msg)
		return nil
	case noteActionablesExtractedMsg:
		z.onNoteActionablesExtracted(msg)
		return nil
	case tea.KeyPressMsg:
		return z.handleKey(msg)
	}
	return nil
}

func (z *zoneView) refresh() {
	if z.client == nil {
		return
	}
	snap, err := z.client.Status()
	if err != nil {
		z.disconnected = true
		return
	}
	z.snap = snap
}

func (z *zoneView) apply(snap session.Snapshot, err error) {
	if err != nil {
		z.disconnected = true
		return
	}
	z.snap = snap
}

func (z *zoneView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if z.disconnected {
		return z.detach()
	}
	if z.snap.Finished {
		// Dismiss the summary: tell the daemon to wind down, then go home.
		if z.client != nil {
			_, _ = z.client.End()
		}
		return z.detach()
	}

	key := msg.String()

	if z.picking {
		return z.handlePickerKey(key)
	}

	if z.noting {
		return z.handleNotesKey(msg)
	}

	if z.confirmingSkip || z.confirmingEnd {
		switch key {
		case "y", "enter":
			if z.confirmingSkip {
				z.confirmingSkip = false
				if z.client != nil {
					z.apply(z.client.Skip())
				}
			} else {
				z.confirmingEnd = false
				if z.client != nil {
					_, _ = z.client.End()
				}
				return z.detach()
			}
		case "s":
			if z.confirmingSkip {
				z.confirmingSkip = false
				if z.client != nil {
					z.apply(z.client.Skip())
				}
			} else {
				z.confirmingEnd = false
			}
		case "n", "esc":
			z.confirmingSkip = false
			z.confirmingEnd = false
		default:
			z.confirmingSkip = false
			z.confirmingEnd = false
		}
		return nil
	}

	switch key {
	case "space", " ":
		if z.client != nil {
			z.apply(z.client.Toggle())
		}
	case "t":
		z.openPicker()
	case "n":
		z.openNotes()
		return tea.Batch(z.enrichPendingCmd(), z.scanPendingActionablesCmd())
	case "s":
		// During prepare, starting early is harmless, so skip straight in.
		if z.snap.Phase == "prepare" {
			if z.client != nil {
				z.apply(z.client.Skip())
			}
		} else {
			z.confirmingSkip = true
			z.confirmingEnd = false
		}
	case "E":
		z.confirmingEnd = true
		z.confirmingSkip = false
	case "b", "esc":
		// Detach: leave the session running in the background, go to dashboard.
		return z.detach()
	}
	return nil
}

// detach closes the client and returns to the dashboard, leaving the daemon as-is.
func (z *zoneView) detach() tea.Cmd {
	if z.client != nil {
		z.client.Close()
		z.client = nil
	}
	return func() tea.Msg { return gotoWorkMsg{} }
}

// openPicker loads the task list and opens the task switcher overlay. Entry 0 is
// always the "no task / just focus" option (represented by a zero-id task).
func (z *zoneView) openPicker() {
	z.tasks = []store.Task{{}} // index 0 = no task
	if z.store != nil {
		if tasks, err := z.store.ListAllTasks(false); err == nil {
			z.tasks = append(z.tasks, tasks...)
		}
	}
	z.pickIdx = 0
	for i, t := range z.tasks {
		if t.ID == z.snap.CurrentTaskID {
			z.pickIdx = i
			break
		}
	}
	z.picking = true
}

func (z *zoneView) handlePickerKey(key string) tea.Cmd {
	switch key {
	case "up", "k":
		z.pickIdx = clampInt(z.pickIdx-1, 0, len(z.tasks)-1)
	case "down", "j":
		z.pickIdx = clampInt(z.pickIdx+1, 0, len(z.tasks)-1)
	case "enter", "t", " ", "space":
		if z.pickIdx >= 0 && z.pickIdx < len(z.tasks) && z.client != nil {
			z.apply(z.client.SetTask(z.tasks[z.pickIdx].ID))
		}
		z.picking = false
	case "esc":
		z.picking = false
	}
	return nil
}

func (z *zoneView) render(width, height int) string {
	z.width, z.height = width, height
	if width == 0 {
		return "entering the zone..."
	}
	if z.disconnected {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			z.styles.Dim.Render("focus session ended. press any key."))
	}
	if z.snap.Finished {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, z.renderSummary())
	}
	if z.picking {
		return z.renderWithChrome(z.renderPicker, width, height)
	}
	if z.noting {
		return z.renderWithChrome(z.renderNotes, width, height)
	}
	if z.snap.Phase == "prepare" {
		return z.renderWithChrome(z.renderPrepareBody, width, height)
	}
	return z.renderWithChrome(z.renderFocusBody, width, height)
}

func (z *zoneView) zoneActionHints() []string {
	s := z.styles
	if z.picking {
		return []string{
			s.helpEntry("↑↓", "move"),
			s.helpEntry("enter", "select"),
			s.helpEntry("esc", "cancel"),
		}
	}
	if z.noting {
		return z.noteActionHints()
	}
	if z.confirmingSkip {
		return []string{
			s.Break.Render("skip this block?") + " " +
				s.helpEntry("y", "yes") + s.Dim.Render("  ·  ") + s.helpEntry("n", "no"),
		}
	}
	if z.confirmingEnd {
		return []string{
			s.Break.Render("end this session?") + " " +
				s.helpEntry("y", "yes") + s.Dim.Render("  ·  ") + s.helpEntry("n", "no"),
		}
	}
	if z.snap.Phase == "prepare" {
		return []string{
			s.helpEntry("t", "task"),
			s.helpEntry("n", "notes"),
			s.helpEntry("space", "pause"),
			s.helpEntry("s", "start now"),
			s.helpEntry("b/esc", "background"),
			s.helpEntry("E", "end session"),
		}
	}
	return []string{
		s.helpEntry("space", "pause"),
		s.helpEntry("t", "task"),
		s.helpEntry("n", "notes"),
		s.helpEntry("s", "skip"),
		s.helpEntry("b/esc", "background"),
		s.helpEntry("E", "end session"),
	}
}

func (z *zoneView) renderFocusBody(width, height int) string {
	s := z.styles

	var phaseLabel string
	var clockStyle lipgloss.Style
	switch z.snap.Phase {
	case "break":
		phaseLabel = s.Break.Render("◌ BREAK")
		clockStyle = lipgloss.NewStyle().Foreground(colBreak).Bold(true)
	default:
		phaseLabel = s.Work.Render("● FOCUS")
		clockStyle = lipgloss.NewStyle().Foreground(colWork).Bold(true)
	}
	if !z.snap.Running {
		phaseLabel = s.Dim.Render("⏸ PAUSED")
		clockStyle = lipgloss.NewStyle().Foreground(colDim).Bold(true)
	}

	clock := clockStyle.Render(bigText(formatClock(z.snap.Remaining)))

	var taskLine, projLine string
	if z.snap.TaskTitle != "" {
		taskLine = s.Title.Render(z.snap.TaskTitle)
		projLine = s.Subtitle.Render(z.snap.ProjectName)
	} else {
		taskLine = s.Dim.Render("general focus")
		projLine = s.Help.Render("press ") + s.HelpKey.Render("t") + s.Help.Render(" to pick a task")
	}

	dots := z.renderCycles()

	block := lipgloss.JoinVertical(lipgloss.Center,
		phaseLabel,
		"",
		clock,
		"",
		taskLine,
		projLine,
		"",
		dots,
		"",
		s.Dim.Render("running in the background — safe to close this terminal"),
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

// renderPrepareBody is the warm, calming settle-in screen shown before focus begins.
func (z *zoneView) renderPrepareBody(width, height int) string {
	s := z.styles

	hi := s.Title.Render(greeting() + " — let's ease into the zone.")
	sub := s.Subtitle.Render("Take a moment to settle in before you begin.")

	var about string
	if z.snap.TaskTitle != "" {
		about = s.Dim.Render("up next   ") + s.StatValue.Render(z.snap.TaskTitle)
		if z.snap.ProjectName != "" {
			about += s.Dim.Render("  ·  ") + s.Subtitle.Render(z.snap.ProjectName)
		}
	} else {
		about = s.Dim.Render("general focus   ") +
			s.Help.Render("press ") + s.HelpKey.Render("t") + s.Help.Render(" to pick a task")
	}

	bullet := s.Accent.Render("•") + " "
	tips := lipgloss.JoinVertical(lipgloss.Left,
		s.Dim.Render("while you wait"),
		bullet+s.Help.Render("pour yourself a glass of water"),
		bullet+s.Help.Render("sit up, relax your shoulders"),
		bullet+s.Help.Render("take a few slow, deep breaths"),
		bullet+s.Help.Render("silence notifications you don't need"),
	)

	var countdown string
	if z.snap.Running {
		countdown = s.Dim.Render("focus begins in  ") + lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render(formatClock(z.snap.Remaining))
	} else {
		countdown = s.Dim.Render("paused at  ") + s.Dim.Render(formatClock(z.snap.Remaining))
	}

	block := lipgloss.JoinVertical(lipgloss.Center,
		hi,
		sub,
		"",
		about,
		"",
		tips,
		"",
		countdown,
		"",
		s.Dim.Render("this runs in the background — you can safely close the terminal"),
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

// renderPicker draws the task switcher overlay.
func (z *zoneView) renderPicker(width, height int) string {
	s := z.styles
	var lines []string
	lines = append(lines, s.PaneTitle.Render("Switch task"), "")

	for i, t := range z.tasks {
		label := t.Title
		if t.ID == 0 {
			label = "no task · just focus"
		} else if t.ProjectName != "" {
			label = t.Title + "  " + s.Dim.Render(t.ProjectName)
		}
		marker := "  "
		if t.ID == z.snap.CurrentTaskID {
			marker = s.Work.Render("● ")
		}
		if i == z.pickIdx {
			lines = append(lines, marker+s.ItemSel.Render(" "+plain(t)+" "))
		} else {
			lines = append(lines, marker+label)
		}
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 3).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// plain returns the bare label used inside the selection highlight.
func plain(t store.Task) string {
	if t.ID == 0 {
		return "no task · just focus"
	}
	if t.ProjectName != "" {
		return t.Title + " — " + t.ProjectName
	}
	return t.Title
}

// greeting returns a time-of-day greeting.
func greeting() string {
	switch h := time.Now().Hour(); {
	case h < 5:
		return "Good night"
	case h < 12:
		return "Good morning"
	case h < 18:
		return "Good afternoon"
	default:
		return "Good evening"
	}
}

func (z *zoneView) renderCycles() string {
	label := z.styles.Dim.Render(fmt.Sprintf("cycle %d/%d", min(z.snap.CycleIndex+1, z.snap.Cycles), z.snap.Cycles))
	return z.renderCycleDotsOnly() + "   " + label
}

func (z *zoneView) renderSummary() string {
	s := z.styles
	lastTask := s.Title.Render(z.snap.TaskTitle)
	if z.snap.TaskTitle == "" {
		lastTask = s.Dim.Render("general focus")
	}
	body := lipgloss.JoinVertical(lipgloss.Center,
		s.Work.Render("session complete"),
		"",
		lastTask,
		s.Subtitle.Render(z.snap.ProjectName),
		"",
		s.Dim.Render("focused for ")+s.StatValue.Render(formatDur(z.snap.Accrued))+
			s.Dim.Render("  ·  ")+s.StatValue.Render(formatDur(z.snap.WallSec))+s.Dim.Render(" elapsed"),
		"",
		s.Help.Render("press any key to return"),
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colWork).
		Padding(1, 4).
		Render(body)
}

// renderWithChrome stacks content between global action and info bars.
func (z *zoneView) renderWithChrome(mainFn func(width, height int) string, width, height int) string {
	topBar := renderActionBar(z.styles, z.zoneActionHints(), width)
	bottomBar := z.renderZoneInfoBar(width)

	topH := lipgloss.Height(topBar)
	bottomH := lipgloss.Height(bottomBar)
	mainH := height - topH - bottomH
	if mainH < 1 {
		mainH = 1
	}

	main := mainFn(width, mainH)
	return composeChrome(topBar, main, bottomBar, width, height)
}

func (z *zoneView) renderZoneInfoBar(width int) string {
	s := z.styles
	var parts []string
	infoEntries := append([]string{renderLLMStatus(s, *z.cfg)}, z.noteInfoHints()...)
	if bar := renderInfoBar(s, infoEntries, width); bar != "" {
		parts = append(parts, bar)
	}
	parts = append(parts, z.renderSessionStatusBar(width))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (z *zoneView) renderSessionStatusBar(width int) string {
	s := z.styles
	sep := s.Dim.Render("  ·  ")

	var phaseLabel string
	var phaseStyle lipgloss.Style
	switch z.snap.Phase {
	case "break":
		phaseLabel = "◌ BREAK"
		phaseStyle = lipgloss.NewStyle().Foreground(colBreak).Bold(true)
	case "prepare":
		phaseLabel = "◎ PREPARE"
		phaseStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	default:
		phaseLabel = "● FOCUS"
		phaseStyle = lipgloss.NewStyle().Foreground(colWork).Bold(true)
	}
	if !z.snap.Running && z.snap.Phase != "prepare" {
		phaseLabel = "⏸ PAUSED"
		phaseStyle = s.Dim
	}

	head := phaseStyle.Render(phaseLabel) + s.Dim.Render("  ") +
		lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render(formatClock(z.snap.Remaining))

	var line1Parts []string
	line1Parts = append(line1Parts, head)
	if z.snap.Cycles > 0 {
		cur := min(z.snap.CycleIndex+1, z.snap.Cycles)
		cycle := z.renderCycleDotsOnly() + s.Dim.Render(fmt.Sprintf("  %d/%d", cur, z.snap.Cycles))
		line1Parts = append(line1Parts, cycle)
	}
	task := "general focus"
	if z.snap.TaskTitle != "" {
		task = z.snap.TaskTitle
		if z.snap.ProjectName != "" {
			task += " · " + z.snap.ProjectName
		}
	}
	line1Parts = append(line1Parts, s.Dim.Render(truncate(task, 36)))
	line1 := joinStatusParts(line1Parts, sep, width-2)

	line2 := s.Dim.Render("session ") + s.StatValue.Render(formatDur(z.snap.Accrued)) +
		s.Dim.Render(" focus") + sep + s.StatValue.Render(formatDur(z.snap.WallSec)) +
		s.Dim.Render(" elapsed")
	today := sep + s.Dim.Render("today ") + s.StatValue.Render(formatDur(z.snap.TodayTotal))
	if lipgloss.Width(line2+today) <= width-2 {
		line2 += today
	}

	return lipgloss.NewStyle().
		Width(width).
		BorderTop(true).
		BorderForeground(colAccent).
		Background(lipgloss.Color("#24283b")).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, line1, line2))
}

func joinStatusParts(parts []string, sep string, width int) string {
	if len(parts) == 0 {
		return ""
	}
	line := parts[0]
	for i := 1; i < len(parts); i++ {
		candidate := line + sep + parts[i]
		if lipgloss.Width(candidate) <= width {
			line = candidate
			continue
		}
		// Drop trailing parts until it fits; always keep the timer head.
		for j := len(parts) - 1; j > 0; j-- {
			trimmed := parts[0]
			for k := 1; k < j; k++ {
				trimmed += sep + parts[k]
			}
			if lipgloss.Width(trimmed) <= width {
				return trimmed
			}
		}
		break
	}
	if lipgloss.Width(line) > width {
		return truncate(line, width)
	}
	return line
}

func (z *zoneView) renderCycleDotsOnly() string {
	cur := z.snap.CycleIndex
	total := z.snap.Cycles
	var b strings.Builder
	for i := 0; i < total; i++ {
		switch {
		case i < cur:
			b.WriteString(lipgloss.NewStyle().Foreground(colWork).Render("●"))
		case i == cur:
			b.WriteString(lipgloss.NewStyle().Foreground(colAccent).Render("◉"))
		default:
			b.WriteString(z.styles.Dim.Render("○"))
		}
		if i < total-1 {
			b.WriteString(" ")
		}
	}
	return b.String()
}
