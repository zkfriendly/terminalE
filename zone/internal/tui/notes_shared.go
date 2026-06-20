package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/llm"
	"github.com/zkfriendly/zone/internal/store"
)

func notePickerMarker(active bool, s Styles) string {
	if active {
		return s.Work.Render("● ")
	}
	return "  "
}

func notePickerPlainLabel(n store.SessionNote, pendingLabel, pendingScan bool, s Styles) string {
	label := notePickerPlainLabelCore(n, pendingLabel)
	return label + noteActionableSuffix(n, pendingScan, s)
}

func notePickerPlainLabelCore(n store.SessionNote, pendingLabel bool) string {
	if n.Title != "" {
		return n.Emoji + " " + n.Title
	}
	if pendingLabel {
		return "… labeling"
	}
	return "unlabeled"
}

func noteActionableSuffix(n store.SessionNote, pendingScan bool, s Styles) string {
	if pendingScan {
		return "  " + s.Dim.Render("… tasks")
	}
	if n.HasActionables {
		return "  " + lipgloss.NewStyle().Foreground(colAccent).Render("◆ tasks")
	}
	return ""
}

func renderNoteBrowseLine(n store.SessionNote, selected, editing, pendingLabel, pendingScan bool, innerW int, s Styles) string {
	if selected {
		plain := notePickerPlainLabel(n, pendingLabel, pendingScan, s)
		return notePickerMarker(true, s) + s.ItemSel.Render(" "+plain+" ")
	}
	label := noteListLabel(n, pendingLabel, pendingScan, innerW, s)
	if editing {
		return s.Work.Render("● ") + label
	}
	return label
}

func noteListLabel(n store.SessionNote, pendingLabel, pendingScan bool, width int, s Styles) string {
	when := n.CreatedAt.Format("15:04")
	suffix := noteActionableSuffix(n, pendingScan, s)
	if pendingLabel {
		return s.Dim.Render(when) + "  " + s.Dim.Render("… labeling") + suffix
	}
	if n.Title != "" {
		head := n.Emoji + " " + n.Title
		return s.Dim.Render(when) + "  " + truncate(head, width-16-lipgloss.Width(suffix)) + suffix
	}
	return s.Dim.Render(when) + "  " + lipgloss.NewStyle().Foreground(colRed).Render("unlabeled") + suffix
}

func noteLabelStatusErr(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("labeling unavailable (config missing)")
	}
	if !cfg.LMStudioEnabled {
		return fmt.Errorf("LM Studio labeling disabled — set lm_studio_enabled to true in config.json")
	}
	if cfg.LMStudioURL == "" {
		return fmt.Errorf("lm_studio_url is empty in config.json")
	}
	return nil
}

type noteActionablesScannedMsg struct {
	noteID         int64
	hasActionables bool
	err            error
}

func scanNoteActionablesCmd(cfg *config.Config, scanning map[int64]bool, noteID int64, body string) tea.Cmd {
	if err := noteLabelStatusErr(cfg); err != nil {
		return nil
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	scanning[noteID] = true
	baseURL := cfg.LMStudioURL
	model := cfg.LMStudioModel
	return func() tea.Msg {
		has, err := llm.DetectActionables(baseURL, model, body)
		if err != nil {
			return noteActionablesScannedMsg{noteID: noteID, err: err}
		}
		return noteActionablesScannedMsg{noteID: noteID, hasActionables: has}
	}
}

func scanPendingActionablesCmd(cfg *config.Config, scanning map[int64]bool, notes []store.SessionNote) tea.Cmd {
	var cmds []tea.Cmd
	for _, n := range notes {
		if strings.TrimSpace(n.Body) == "" || !n.NeedsActionableScan() || scanning[n.ID] {
			continue
		}
		cmds = append(cmds, scanNoteActionablesCmd(cfg, scanning, n.ID, n.Body))
	}
	return tea.Batch(cmds...)
}

func sessionTaskLabel(taskTitle, projectName string) string {
	if taskTitle == "" {
		return "general focus"
	}
	if projectName != "" {
		return taskTitle + " · " + projectName
	}
	return taskTitle
}

func renderSessionDivider(n store.GlobalNote, width int, s Styles) string {
	when := n.SessionStarted.Format("Jan 02 15:04")
	task := sessionTaskLabel(n.TaskTitle, n.ProjectName)
	label := when + "  ·  " + task
	line := s.Dim.Render("── ") + s.Subtitle.Render(label) + s.Dim.Render(" ──")
	return lipgloss.NewStyle().Width(width).Render(line)
}

type noteBrowseKind int

const (
	noteRowSession noteBrowseKind = iota
	noteRowNote
)

type noteBrowseRow struct {
	kind    noteBrowseKind
	session store.GlobalNote
	note    store.GlobalNote
}

func buildNoteBrowseRows(notes []store.GlobalNote) []noteBrowseRow {
	var rows []noteBrowseRow
	var lastSession int64 = -1
	for _, n := range notes {
		if n.SessionID != lastSession {
			rows = append(rows, noteBrowseRow{kind: noteRowSession, session: n})
			lastSession = n.SessionID
		}
		rows = append(rows, noteBrowseRow{kind: noteRowNote, note: n})
	}
	return rows
}

func updateNoteBrowseRow(rows []noteBrowseRow, n store.SessionNote) {
	for i := range rows {
		if rows[i].kind != noteRowNote || rows[i].note.ID != n.ID {
			continue
		}
		row := rows[i]
		row.note.SessionNote = n
		rows[i] = row
		return
	}
}

func noteBrowseNoteCount(rows []noteBrowseRow) int {
	n := 0
	for _, row := range rows {
		if row.kind == noteRowNote {
			n++
		}
	}
	return n
}

func noteBrowseNoteAt(rows []noteBrowseRow, pickIdx int) (store.GlobalNote, bool) {
	if pickIdx <= 0 || pickIdx > len(rows) {
		return store.GlobalNote{}, false
	}
	row := rows[pickIdx-1]
	if row.kind != noteRowNote {
		return store.GlobalNote{}, false
	}
	return row.note, true
}

func notePickIdxForNote(rows []noteBrowseRow, noteID int64) int {
	for i, row := range rows {
		if row.kind == noteRowNote && row.note.ID == noteID {
			return i + 1 // row 0 is "+ new note"
		}
	}
	return 0
}

func moveNotePickIdx(rows []noteBrowseRow, idx, delta int) int {
	total := len(rows) + 1
	if total <= 1 {
		return 0
	}
	start := idx
	for i := 0; i < total; i++ {
		next := idx + delta
		if next < 0 {
			next = total - 1
		}
		if next >= total {
			next = 0
		}
		idx = next
		if idx == 0 {
			return idx
		}
		if rows[idx-1].kind == noteRowNote {
			return idx
		}
		if idx == start {
			return idx
		}
	}
	return idx
}

func noteBrowseNotes(rows []noteBrowseRow) []store.SessionNote {
	var notes []store.SessionNote
	for _, row := range rows {
		if row.kind == noteRowNote {
			notes = append(notes, row.note.SessionNote)
		}
	}
	return notes
}
