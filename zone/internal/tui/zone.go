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
	cfg    config.Config
	client *session.Client

	snap          session.Snapshot
	width, height int

	confirmingSkip bool
	disconnected   bool

	// Task switcher overlay.
	picking bool
	tasks   []store.Task // picker entries; index 0 is the "no task" option
	pickIdx int

	// Session notes overlay.
	noting         bool
	noteEditor     vimNoteEditor
	notes          []store.SessionNote
	editingNoteID  int64 // 0 = composing a new note
	notePicking    bool
	notePickIdx    int
	enrichingNotes map[int64]bool // notes waiting on LM Studio
	noteLabelErr   string         // last labeling error (shown in picker)
}

type noteEnrichedMsg struct {
	noteID int64
	note   store.SessionNote
	err    error
}

func newZone(st *store.Store, client *session.Client, s Styles, cfg config.Config, initial session.Snapshot) *zoneView {
	return &zoneView{
		store:          st,
		cfg:            cfg,
		styles:         s,
		client:         client,
		snap:           initial,
		enrichingNotes: map[int64]bool{},
	}
}

func (z *zoneView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tickMsg:
		z.refresh()
		return nil
	case noteEnrichedMsg:
		z.onNoteEnriched(msg)
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

	if z.confirmingSkip {
		switch key {
		case "y", "s", "enter":
			z.confirmingSkip = false
			if z.client != nil {
				z.apply(z.client.Skip())
			}
		default:
			z.confirmingSkip = false
		}
		return nil
	}

	// Number keys toggle ambient sound layers (overlapping allowed).
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		if z.client != nil {
			z.apply(z.client.Track(int(key[0] - '1')))
		}
		return nil
	}

	switch key {
	case "space", " ":
		if z.client != nil {
			z.apply(z.client.Toggle())
		}
	case "p":
		if z.client != nil {
			z.apply(z.client.Preview())
		}
	case "t":
		z.openPicker()
	case "n":
		z.openNotes()
		return z.enrichPendingCmd()
	case "s":
		// During prepare, starting early is harmless, so skip straight in.
		if z.snap.Phase == "prepare" {
			if z.client != nil {
				z.apply(z.client.Skip())
			}
		} else {
			z.confirmingSkip = true
		}
	case "+", "=":
		if z.client != nil {
			z.apply(z.client.SetVolume(clampFloat(z.snap.Volume+0.1, 0, 1)))
		}
	case "-", "_":
		if z.client != nil {
			z.apply(z.client.SetVolume(clampFloat(z.snap.Volume-0.1, 0, 1)))
		}
	case "esc":
		// End the session entirely (stops the daemon).
		if z.client != nil {
			_, _ = z.client.End()
		}
		return z.detach()
	case "b", "q":
		// Detach: leave the session running in the background.
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
	return func() tea.Msg { return gotoDashboardMsg{} }
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
	case "esc", "q":
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
		return z.renderWithTimerBar(z.renderPicker(width, height), width, height)
	}
	if z.noting {
		return z.renderWithTimerBar(z.renderNotes(width, height), width, height)
	}
	if z.snap.Phase == "prepare" {
		return z.renderPrepare(width, height)
	}

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
	sessionLine := s.Dim.Render("session  ") + s.StatValue.Render(formatDur(z.snap.Accrued)) +
		s.Dim.Render(" focus  ·  ") + s.StatValue.Render(formatDur(z.snap.WallSec)) + s.Dim.Render(" elapsed")
	statLine := s.Dim.Render("today in the zone: ") + s.StatValue.Render(formatDur(z.snap.TodayTotal))
	soundLine := z.renderSounds(width)

	footer := wrapHints([]string{
		s.helpEntry("space", "pause"),
		s.helpEntry("t", "task"),
		s.helpEntry("n", "notes"),
		s.helpEntry("s", "skip"),
		s.helpEntry("1-9", "sounds"),
		s.helpEntry("+/-", "volume"),
		s.helpEntry("b", "background"),
		s.helpEntry("esc", "end"),
	}, s.Dim.Render("  ·  "), width)
	if z.confirmingSkip {
		footer = s.Break.Render("skip this block?") + " " +
			s.helpEntry("y", "yes") + s.Dim.Render("  ·  ") + s.helpEntry("n", "no")
	}

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
		sessionLine,
		statLine,
		"",
		soundLine,
		"",
		s.Dim.Render("running in the background — safe to close this terminal"),
		"",
		footer,
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

// renderPrepare is the warm, calming settle-in screen shown before focus begins.
func (z *zoneView) renderPrepare(width, height int) string {
	s := z.styles
	accent := lipgloss.NewStyle().Foreground(colAccent).Bold(true)

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

	sounds := lipgloss.JoinVertical(lipgloss.Left,
		s.Dim.Render("the sounds to listen for"),
		s.Work.Render("↗ a soft rising chime")+s.Dim.Render("   means focus has begun"),
		accent.Render("★ a bright finishing chime")+s.Dim.Render("   means your session is complete"),
		s.Help.Render("press ")+s.HelpKey.Render("p")+s.Help.Render(" to hear them now"),
	)

	var countdown string
	if z.snap.Running {
		countdown = s.Dim.Render("focus begins in  ") + accent.Render(formatClock(z.snap.Remaining))
	} else {
		countdown = s.Dim.Render("paused at  ") + s.Dim.Render(formatClock(z.snap.Remaining))
	}

	footer := wrapHints([]string{
		s.helpEntry("p", "hear the sounds"),
		s.helpEntry("t", "task"),
		s.helpEntry("n", "notes"),
		s.helpEntry("space", "pause"),
		s.helpEntry("s", "start now"),
		s.helpEntry("b", "background"),
		s.helpEntry("esc", "cancel"),
	}, s.Dim.Render("  ·  "), width)

	block := lipgloss.JoinVertical(lipgloss.Center,
		hi,
		sub,
		"",
		about,
		"",
		tips,
		"",
		sounds,
		"",
		countdown,
		"",
		s.Dim.Render("this runs in the background — you can safely close the terminal"),
		"",
		footer,
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
	lines = append(lines, "", s.Help.Render("↑↓ move · enter select · esc cancel"))

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
			b.WriteString("  ")
		}
	}
	label := z.styles.Dim.Render(fmt.Sprintf("cycle %d/%d", min(cur+1, total), total))
	return b.String() + "   " + label
}

// renderSounds shows the toggleable ambient layers with their numbers and state.
func (z *zoneView) renderSounds(width int) string {
	s := z.styles
	if len(z.snap.Tracks) == 0 {
		return ""
	}
	var parts []string
	for i, t := range z.snap.Tracks {
		num := s.HelpKey.Render(fmt.Sprintf("%d", i+1))
		var state string
		if t.Enabled {
			state = s.Work.Render("● " + t.Label)
		} else {
			state = s.Dim.Render("○ " + t.Label)
		}
		parts = append(parts, num+" "+state)
	}
	return s.Dim.Render("sound  ") + wrapHints(parts, s.Dim.Render("   "), width)
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

// renderWithTimerBar stacks overlay content above a persistent session timer bar.
func (z *zoneView) renderWithTimerBar(main string, width, height int) string {
	bar := z.renderTimerBar(width)
	barH := lipgloss.Height(bar)
	mainH := height - barH
	if mainH < 1 {
		mainH = 1
	}
	placed := lipgloss.Place(width, mainH, lipgloss.Center, lipgloss.Center, main)
	return lipgloss.JoinVertical(lipgloss.Left, placed, bar)
}

func (z *zoneView) renderTimerBar(width int) string {
	s := z.styles
	var phase lipgloss.Style
	var label string
	switch z.snap.Phase {
	case "break":
		label = "◌ BREAK"
		phase = lipgloss.NewStyle().Foreground(colBreak).Bold(true)
	case "prepare":
		label = "◎ PREPARE"
		phase = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	default:
		label = "● FOCUS"
		phase = lipgloss.NewStyle().Foreground(colWork).Bold(true)
	}
	if !z.snap.Running && z.snap.Phase != "prepare" {
		label = "⏸ PAUSED"
		phase = s.Dim
	}

	left := phase.Render(label) + s.Dim.Render("  ") + s.StatValue.Render(formatClock(z.snap.Remaining))
	right := ""
	if z.snap.TaskTitle != "" {
		right = s.Dim.Render(truncate(z.snap.TaskTitle, 40))
	}

	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().
		Width(width).
		BorderTop(true).
		BorderForeground(colDim).
		Padding(0, 1).
		Render(line)
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
