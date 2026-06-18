package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/store"
)

// historyView is a scrollable list of every focus session.
type historyView struct {
	store  *store.Store
	styles Styles

	width, height int
	sessions      []store.SessionSummary
	offset        int
	cursor        int // selected row in sessions

	// Read-only notes overlay for a past session.
	viewingNotes bool
	notes        []store.SessionNote
}

func newHistory(st *store.Store, s Styles) *historyView {
	v := &historyView{store: st, styles: s}
	v.reload()
	return v
}

func (v *historyView) reload() {
	v.sessions, _ = v.store.RecentSessions(500)
	v.clampCursor()
	v.clampOffset()
}

func (v *historyView) clampCursor() {
	if len(v.sessions) == 0 {
		v.cursor = 0
		return
	}
	v.cursor = clampInt(v.cursor, 0, len(v.sessions)-1)
}

func (v *historyView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if v.viewingNotes {
			if msg.String() == "esc" {
				v.viewingNotes = false
				v.notes = nil
			}
			return nil
		}
		switch msg.String() {
		case "esc":
			return func() tea.Msg { return gotoStatsMsg{} }
		case "q":
			return func() tea.Msg { return gotoDashboardMsg{} }
		case "up", "k":
			v.cursor--
		case "down", "j":
			v.cursor++
		case "pgup":
			v.cursor -= v.pageSize()
		case "pgdown", " ", "space":
			v.cursor += v.pageSize()
		case "g", "home":
			v.cursor = 0
		case "G", "end":
			v.cursor = len(v.sessions) - 1
		case "n":
			v.openNotes()
		case "r":
			v.reload()
		}
		v.clampCursor()
		v.ensureCursorVisible()
	}
	return nil
}

func (v *historyView) openNotes() {
	if v.cursor < 0 || v.cursor >= len(v.sessions) {
		return
	}
	sid := v.sessions[v.cursor].ID
	v.notes, _ = v.store.ListSessionNotes(sid)
	v.viewingNotes = true
}

func (v *historyView) ensureCursorVisible() {
	page := v.pageSize()
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if v.cursor >= v.offset+page {
		v.offset = v.cursor - page + 1
	}
	v.clampOffset()
}

// pageSize is the number of rows visible at once.
func (v *historyView) pageSize() int {
	// header(1) + blank(1) + column head(1) + blank(1) + footer(2) ~= 6 chrome rows.
	n := v.height - 6
	if n < 1 {
		n = 1
	}
	return n
}

func (v *historyView) clampOffset() {
	maxOff := len(v.sessions) - v.pageSize()
	if maxOff < 0 {
		maxOff = 0
	}
	v.offset = clampInt(v.offset, 0, maxOff)
	// Keep cursor on screen when the list shrinks.
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if v.cursor >= v.offset+v.pageSize() && v.pageSize() > 0 {
		v.offset = v.cursor - v.pageSize() + 1
	}
}

func (v *historyView) render(width, height int) string {
	v.width, v.height = width, height
	if width == 0 {
		return "loading history..."
	}
	if v.viewingNotes {
		return renderNotesReadOnly(v.notes, width, height, v.styles)
	}
	s := v.styles

	header := s.Title.Render("zone · session history") + "  " +
		s.Dim.Render(fmt.Sprintf("%d sessions", len(v.sessions)))

	if len(v.sessions) == 0 {
		body := s.Dim.Render("no sessions yet — press f on the dashboard to start one")
		footer := "\n" + wrapHints([]string{s.helpEntry("esc", "back")}, s.Dim.Render("  ·  "), width)
		return lipgloss.JoinVertical(lipgloss.Left, header, "", body, footer)
	}

	colHead := s.Dim.Render(fmt.Sprintf("%-16s  %-26s  %-9s  %-9s  %-5s  %s",
		"when", "task", "focus", "wall", "notes", "status"))

	page := v.pageSize()
	end := v.offset + page
	if end > len(v.sessions) {
		end = len(v.sessions)
	}

	var rows []string
	for i, ss := range v.sessions[v.offset:end] {
		rows = append(rows, v.row(ss, v.offset+i == v.cursor))
	}

	scroll := ""
	if len(v.sessions) > page {
		scroll = s.Dim.Render(fmt.Sprintf("  showing %d–%d of %d", v.offset+1, end, len(v.sessions)))
	}

	footer := "\n" + wrapHints([]string{
		s.helpEntry("↑↓", "select"),
		s.helpEntry("n", "view notes"),
		s.helpEntry("g/G", "top/bottom"),
		s.helpEntry("r", "refresh"),
		s.helpEntry("esc", "stats"),
		s.helpEntry("q", "dashboard"),
	}, s.Dim.Render("  ·  "), width)

	return lipgloss.JoinVertical(lipgloss.Left,
		header+scroll,
		"",
		colHead,
		strings.Join(rows, "\n"),
		footer,
	)
}

func (v *historyView) row(ss store.SessionSummary, selected bool) string {
	s := v.styles
	when := ss.StartedAt.Format("Jan 02 15:04")

	task := "general focus"
	if ss.TaskTitle != "" {
		task = ss.TaskTitle
		if ss.ProjectName != "" {
			task += " · " + ss.ProjectName
		}
	}

	notes := "-"
	if ss.NoteCount > 0 {
		notes = fmt.Sprintf("%d", ss.NoteCount)
	}

	line := padRight(when, 16) + "  " +
		padRight(truncate(task, 26), 26) + "  " +
		padRight(formatDur(ss.WorkedSec), 9) + "  " +
		padRight(formatDur(ss.WallSec), 9) + "  " +
		padRight(notes, 5) + "  " +
		ss.Status

	if selected {
		return s.ItemSel.Render(" " + line + " ")
	}
	return s.Dim.Render(padRight(when, 16)) + "  " +
		padRight(truncate(task, 26), 26) + "  " +
		s.StatValue.Render(padRight(formatDur(ss.WorkedSec), 9)) + "  " +
		s.Dim.Render(padRight(formatDur(ss.WallSec), 9)) + "  " +
		s.Dim.Render(padRight(notes, 5)) + "  " +
		statusBadge(s, ss.Status)
}
