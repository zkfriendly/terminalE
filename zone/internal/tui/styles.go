package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Palette (Tokyo Night inspired).
var (
	colBG      = lipgloss.Color("#1a1b26")
	colFG      = lipgloss.Color("#c0caf5")
	colDim     = lipgloss.Color("#565f89")
	colAccent  = lipgloss.Color("#7aa2f7")
	colWork    = lipgloss.Color("#9ece6a")
	colBreak   = lipgloss.Color("#e0af68")
	colMagenta = lipgloss.Color("#bb9af7")
	colRed     = lipgloss.Color("#f7768e")
	colCyan    = lipgloss.Color("#7dcfff")
)

// Styles bundles the reusable lipgloss styles for the app.
type Styles struct {
	Title        lipgloss.Style
	Subtitle     lipgloss.Style
	Help         lipgloss.Style
	HelpKey      lipgloss.Style
	Dim          lipgloss.Style
	PaneActive   lipgloss.Style
	PaneInactive lipgloss.Style
	PaneTitle    lipgloss.Style
	Item         lipgloss.Style
	ItemSel      lipgloss.Style
	Badge        lipgloss.Style
	StatLabel    lipgloss.Style
	StatValue    lipgloss.Style
	Work         lipgloss.Style
	Break        lipgloss.Style
	Accent       lipgloss.Style
}

func newStyles() Styles {
	return Styles{
		Title:    lipgloss.NewStyle().Bold(true).Foreground(colFG),
		Subtitle: lipgloss.NewStyle().Foreground(colAccent),
		Help:     lipgloss.NewStyle().Foreground(colDim),
		HelpKey:  lipgloss.NewStyle().Foreground(colCyan),
		Dim:      lipgloss.NewStyle().Foreground(colDim),
		PaneActive: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).
			Padding(0, 1),
		PaneInactive: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colDim).
			Padding(0, 1),
		PaneTitle: lipgloss.NewStyle().Bold(true).Foreground(colMagenta),
		Item:      lipgloss.NewStyle().Foreground(colFG),
		ItemSel: lipgloss.NewStyle().
			Foreground(colBG).Background(colAccent).Bold(true),
		Badge:     lipgloss.NewStyle().Foreground(colDim),
		StatLabel: lipgloss.NewStyle().Foreground(colDim),
		StatValue: lipgloss.NewStyle().Bold(true).Foreground(colFG),
		Work:      lipgloss.NewStyle().Bold(true).Foreground(colWork),
		Break:     lipgloss.NewStyle().Bold(true).Foreground(colBreak),
		Accent:    lipgloss.NewStyle().Foreground(colAccent),
	}
}

// helpEntry renders a "key action" hint.
func (s Styles) helpEntry(key, action string) string {
	return s.HelpKey.Render(key) + " " + s.Help.Render(action)
}

// wrapHints joins hint entries with sep, wrapping onto new lines so no line
// exceeds width display cells. ANSI styling is accounted for via lipgloss.Width.
func wrapHints(entries []string, sep string, width int) string {
	if len(entries) == 0 {
		return ""
	}
	if width <= 0 {
		return strings.Join(entries, sep)
	}
	var lines []string
	cur := entries[0]
	for _, e := range entries[1:] {
		if lipgloss.Width(cur+sep+e) > width {
			lines = append(lines, cur)
			cur = e
			continue
		}
		cur += sep + e
	}
	lines = append(lines, cur)
	return strings.Join(lines, "\n")
}

// padRight pads s with spaces to n display cells (ANSI-aware via lipgloss.Width).
func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// formatClock renders seconds as MM:SS or H:MM:SS.
func formatClock(sec int) string {
	if sec < 0 {
		sec = 0
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// formatDur renders a human-friendly duration like "1h 23m" or "12m" or "45s".
func formatDur(sec int) string {
	if sec <= 0 {
		return "0m"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
