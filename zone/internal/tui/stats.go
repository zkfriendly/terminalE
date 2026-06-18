package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/store"
)

// streakThreshold: a day counts toward the streak with >= 25 min of work.
const streakThreshold = 25 * 60

// statsView shows aggregate focus stats and recent history.
type statsView struct {
	store  *store.Store
	styles Styles

	width, height int

	today    int
	week     int
	streak   int
	projects []store.ProjectStat
	tasks    []store.TaskStat
	sessions []store.SessionSummary
}

func newStats(st *store.Store, s Styles) *statsView {
	v := &statsView{store: st, styles: s}
	v.reload()
	return v
}

func (v *statsView) reload() {
	now := time.Now()
	dayStart := startOfDay(now)
	weekStart := dayStart.AddDate(0, 0, -6)

	v.today, _ = v.store.TodayWorkSeconds()
	v.week, _ = v.store.WorkSecondsSince(weekStart)
	v.streak, _ = v.store.Streak(streakThreshold)
	v.projects, _ = v.store.ProjectBreakdown(weekStart)
	v.tasks, _ = v.store.TaskBreakdown(dayStart, 6)
	v.sessions, _ = v.store.RecentSessions(8)
}

func (v *statsView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q":
			return func() tea.Msg { return gotoDashboardMsg{} }
		case "h":
			return func() tea.Msg { return gotoHistoryMsg{} }
		case "r":
			v.reload()
		}
	}
	return nil
}

func (v *statsView) render(width, height int) string {
	v.width, v.height = width, height
	if width == 0 {
		return "loading stats..."
	}
	s := v.styles

	header := s.Title.Render("zone · stats")

	cards := lipgloss.JoinHorizontal(lipgloss.Top,
		v.statCard("today", formatDur(v.today)),
		" ",
		v.statCard("last 7 days", formatDur(v.week)),
		" ",
		v.statCard("streak", fmt.Sprintf("%d days", v.streak)),
	)

	left := lipgloss.JoinVertical(lipgloss.Left,
		s.PaneTitle.Render("Projects · last 7 days"),
		"",
		v.renderBars(),
	)
	right := lipgloss.JoinVertical(lipgloss.Left,
		s.PaneTitle.Render("Top tasks · today"),
		"",
		v.renderTaskList(),
	)
	colW := (width - 6) / 2
	if colW < 20 {
		colW = 20
	}
	cols := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(colW).Render(left),
		"  ",
		lipgloss.NewStyle().Width(colW).Render(right),
	)

	history := lipgloss.JoinVertical(lipgloss.Left,
		s.PaneTitle.Render("Recent sessions"),
		"",
		v.renderSessions(),
	)

	footer := "\n" + wrapHints([]string{
		s.helpEntry("h", "full history"),
		s.helpEntry("r", "refresh"),
		s.helpEntry("esc", "back"),
	}, s.Dim.Render("  ·  "), width)

	return lipgloss.JoinVertical(lipgloss.Left,
		header, "", cards, "", cols, "", history, footer,
	)
}

func (v *statsView) statCard(label, value string) string {
	s := v.styles
	body := s.StatLabel.Render(label) + "\n" + s.StatValue.Render(value)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colDim).
		Padding(0, 2).
		Width(18).
		Render(body)
}

func (v *statsView) renderBars() string {
	s := v.styles
	if len(v.projects) == 0 {
		return s.Dim.Render("no tracked time yet")
	}
	maxSec := 0
	for _, p := range v.projects {
		if p.WorkSec > maxSec {
			maxSec = p.WorkSec
		}
	}
	const barW = 18
	var lines []string
	for _, p := range v.projects {
		filled := 0
		if maxSec > 0 {
			filled = p.WorkSec * barW / maxSec
		}
		bar := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color)).
			Render(strings.Repeat("█", filled) + strings.Repeat("░", barW-filled))
		name := truncate(p.ProjectName, 14)
		lines = append(lines, fmt.Sprintf("%-14s %s %s", name, bar, s.Dim.Render(formatDur(p.WorkSec))))
	}
	return strings.Join(lines, "\n")
}

func (v *statsView) renderTaskList() string {
	s := v.styles
	if len(v.tasks) == 0 {
		return s.Dim.Render("nothing tracked today")
	}
	var lines []string
	for _, t := range v.tasks {
		dot := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Color)).Render("●")
		lines = append(lines, fmt.Sprintf("%s %-22s %s",
			dot, truncate(t.TaskTitle, 22), s.StatValue.Render(formatDur(t.WorkSec))))
	}
	return strings.Join(lines, "\n")
}

func (v *statsView) renderSessions() string {
	s := v.styles
	if len(v.sessions) == 0 {
		return s.Dim.Render("no sessions yet")
	}
	var lines []string
	for _, ss := range v.sessions {
		when := ss.StartedAt.Format("Mon 15:04")
		status := statusBadge(s, ss.Status)

		// A session may have no current task (general focus) or span several.
		var label string
		if ss.TaskTitle == "" {
			label = s.Dim.Render("general focus")
		} else {
			dot := lipgloss.NewStyle().Foreground(lipgloss.Color(ss.ProjectColor)).Render("●")
			label = dot + " " + truncate(ss.TaskTitle, 20) + " · " + s.Dim.Render(truncate(ss.ProjectName, 14))
		}
		line := fmt.Sprintf("%s  %s  %s %s  %s",
			s.Dim.Render(when),
			label,
			s.StatValue.Render(formatDur(ss.WorkedSec)),
			s.Dim.Render("("+formatDur(ss.WallSec)+" wall)"),
			status,
		)
		if ss.NoteCount > 0 {
			line += "  " + s.Dim.Render(fmt.Sprintf("%d notes", ss.NoteCount))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func statusBadge(s Styles, status string) string {
	switch status {
	case store.SessionCompleted:
		return s.Work.Render("done")
	case store.SessionAbandoned:
		return lipgloss.NewStyle().Foreground(colRed).Render("ended")
	default:
		return s.Accent.Render("active")
	}
}

func truncate(str string, n int) string {
	r := []rune(str)
	if len(r) <= n {
		return str
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// startOfDay mirrors the store helper for local use in the stats view.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
