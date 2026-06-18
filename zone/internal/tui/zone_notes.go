package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/llm"
	"github.com/zkfriendly/zone/internal/store"
)

func (z *zoneView) openNotes() {
	if z.store == nil || z.snap.SessionID == 0 {
		return
	}
	z.noteLabelErr = ""
	z.reloadNotes()
	z.editingNoteID = 0

	w := z.noteEditorWidth()
	h := z.noteEditorHeight()
	z.noteEditor = newVimNoteEditor(w, h)
	z.noting = true
	z.openNotePicker()
}

func (z *zoneView) reloadNotes() {
	if z.store == nil || z.snap.SessionID == 0 {
		return
	}
	z.notes, _ = z.store.ListSessionNotes(z.snap.SessionID)
}

func (z *zoneView) noteEditorWidth() int {
	w := z.width - 14
	if w < 30 {
		w = 30
	}
	if w > 72 {
		w = 72
	}
	return w
}

func (z *zoneView) noteEditorHeight() int {
	h := z.height - 16
	if h < 4 {
		h = 4
	}
	if h > 12 {
		h = 12
	}
	return h
}

func (z *zoneView) saveNoteDraft() tea.Cmd {
	if z.store == nil || z.snap.SessionID == 0 {
		return nil
	}
	body := strings.TrimSpace(z.noteEditor.Value())
	if body == "" {
		return nil
	}
	var id int64
	if z.editingNoteID != 0 {
		n, err := z.store.UpdateSessionNote(z.editingNoteID, body)
		if err != nil {
			return nil
		}
		id = n.ID
		for i := range z.notes {
			if z.notes[i].ID == n.ID {
				z.notes[i] = n
				break
			}
		}
		if n.Title != "" {
			return nil
		}
	} else {
		n, err := z.store.AddSessionNote(z.snap.SessionID, body)
		if err != nil {
			return nil
		}
		id = n.ID
		z.notes = append([]store.SessionNote{n}, z.notes...)
		z.editingNoteID = n.ID
	}
	return z.enrichNoteCmd(id, body)
}

func (z *zoneView) enrichNoteCmd(noteID int64, body string) tea.Cmd {
	if !z.cfg.LMStudioEnabled || z.cfg.LMStudioURL == "" {
		return nil
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	z.enrichingNotes[noteID] = true
	baseURL := z.cfg.LMStudioURL
	model := z.cfg.LMStudioModel
	return func() tea.Msg {
		meta, err := llm.EnrichNote(baseURL, model, body)
		if err != nil {
			return noteEnrichedMsg{noteID: noteID, err: err}
		}
		return noteEnrichedMsg{noteID: noteID, note: store.SessionNote{
			ID:    noteID,
			Title: meta.Title,
			Emoji: meta.Emoji,
		}}
	}
}

func (z *zoneView) enrichPendingCmd() tea.Cmd {
	if !z.cfg.LMStudioEnabled {
		return nil
	}
	var cmds []tea.Cmd
	for _, n := range z.notes {
		if n.Title == "" && strings.TrimSpace(n.Body) != "" && !z.enrichingNotes[n.ID] {
			cmds = append(cmds, z.enrichNoteCmd(n.ID, n.Body))
		}
	}
	return tea.Batch(cmds...)
}

func (z *zoneView) onNoteEnriched(msg noteEnrichedMsg) {
	delete(z.enrichingNotes, msg.noteID)
	if msg.err != nil {
		z.noteLabelErr = msg.err.Error()
		return
	}
	if z.store == nil {
		return
	}
	n, err := z.store.SetSessionNoteMeta(msg.noteID, msg.note.Title, msg.note.Emoji)
	if err != nil {
		z.noteLabelErr = err.Error()
		return
	}
	z.noteLabelErr = ""
	for i := range z.notes {
		if z.notes[i].ID == n.ID {
			z.notes[i] = n
			return
		}
	}
}

func (z *zoneView) newNote() {
	_ = z.saveNoteDraft()
	z.editingNoteID = 0
	z.noteEditor.Reset()
	z.noteEditor.Resize(z.noteEditorWidth(), z.noteEditorHeight())
	z.notePicking = false
}

func (z *zoneView) loadNote(n store.SessionNote) {
	z.editingNoteID = n.ID
	z.noteEditor.Load(n.Body)
	z.noteEditor.Resize(z.noteEditorWidth(), z.noteEditorHeight())
	z.notePicking = false
}

func (z *zoneView) openNotePicker() {
	z.notePickIdx = 0
	for i, n := range z.notes {
		if n.ID == z.editingNoteID {
			z.notePickIdx = i + 1 // +1 for the "new note" row
			break
		}
	}
	z.notePicking = true
}

func (z *zoneView) closeNotes(save bool) {
	if save {
		_ = z.saveNoteDraft()
	}
	z.noteEditor.Blur()
	z.noteEditor.Reset()
	z.noting = false
	z.notePicking = false
	z.editingNoteID = 0
}

func (z *zoneView) handleNotesKey(msg tea.KeyPressMsg) tea.Cmd {
	if z.notePicking {
		return z.handleNotePickerKey(msg.String())
	}

	k := msg.String()
	if k == "esc" && z.noteEditor.mode == noteModeNormal {
		z.openNotePicker()
		return z.enrichPendingCmd()
	}

	cmd, act := z.noteEditor.Update(msg)
	var saveCmd tea.Cmd
	switch act {
	case noteActSave:
		saveCmd = z.saveNoteDraft()
	case noteActSaveQuit:
		saveCmd = z.saveNoteDraft()
		z.noteEditor.Blur()
		z.noteEditor.Reset()
		z.openNotePicker()
		if saveCmd != nil {
			return tea.Batch(cmd, saveCmd, z.enrichPendingCmd())
		}
		return tea.Batch(cmd, z.enrichPendingCmd())
	case noteActQuit:
		z.closeNotes(false)
	case noteActBrowse:
		z.openNotePicker()
		saveCmd = z.enrichPendingCmd()
	case noteActNew:
		z.newNote()
	}
	if saveCmd != nil {
		return tea.Batch(cmd, saveCmd)
	}
	return cmd
}

func (z *zoneView) handleNotePickerKey(key string) tea.Cmd {
	n := len(z.notes) + 1 // row 0 = new note
	switch key {
	case "up", "k":
		z.notePickIdx = clampInt(z.notePickIdx-1, 0, n-1)
	case "down", "j":
		z.notePickIdx = clampInt(z.notePickIdx+1, 0, n-1)
	case "enter", " ", "space":
		saveCmd := z.saveNoteDraft()
		if z.notePickIdx == 0 {
			z.newNote()
		} else {
			z.loadNote(z.notes[z.notePickIdx-1])
		}
		return saveCmd
	case "esc":
		z.closeNotes(false)
	case "n":
		_ = z.saveNoteDraft()
		z.newNote()
	}
	return nil
}

func (z *zoneView) renderNotes(width, height int) string {
	if z.notePicking {
		return z.renderNotePicker(width, height)
	}
	return z.renderNoteEditor(width, height)
}

func noteListLabel(n store.SessionNote, pending bool, width int, s Styles) string {
	when := n.CreatedAt.Format("15:04")
	if pending {
		return s.Dim.Render(when) + "  " + s.Dim.Render("… labeling")
	}
	if n.Title != "" {
		head := n.Emoji + " " + n.Title
		return s.Dim.Render(when) + "  " + truncate(head, width-16)
	}
	body := strings.TrimSpace(n.Body)
	body = strings.ReplaceAll(body, "\n", " ")
	return s.Dim.Render(when) + "  " + truncate(body, width-16)
}

func (z *zoneView) renderNoteEditor(width, height int) string {
	s := z.styles
	accent := lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	title := accent.Render("session notes")
	if z.editingNoteID != 0 {
		sub := "editing"
		for _, n := range z.notes {
			if n.ID == z.editingNoteID {
				if n.Title != "" {
					sub = n.Emoji + " " + n.Title
				} else if z.enrichingNotes[n.ID] {
					sub = "labeling…"
				}
				break
			}
		}
		if strings.Contains(sub, " ") && !strings.HasSuffix(sub, "…") {
			title += "  " + s.StatValue.Render(sub)
		} else {
			title += s.Dim.Render("  ·  " + sub)
		}
	} else {
		title += s.Dim.Render("  ·  new")
	}

	modeLine := modeStyle(z.noteEditor.mode, s).Render(z.noteEditor.ModeLabel())
	footer := wrapHints([]string{
		s.helpEntry(":w", "save"),
		s.helpEntry(":wq/ZZ", "save & browse"),
		s.helpEntry(":e/esc", "browse"),
		s.helpEntry(":new", "new note"),
	}, s.Dim.Render("  ·  "), width)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			title,
			"",
			z.noteEditor.View(),
			"",
			modeLine,
			footer,
		))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func (z *zoneView) renderNotePicker(width, height int) string {
	s := z.styles
	var lines []string
	lines = append(lines, s.PaneTitle.Render("Open note"), "")

	marker := func(active bool) string {
		if active {
			return s.Work.Render("● ")
		}
		return "  "
	}

	lines = append(lines, marker(z.notePickIdx == 0)+s.Item.Render("+ new note"))
	for i, n := range z.notes {
		label := noteListLabel(n, z.enrichingNotes[n.ID], width, s)
		if i+1 == z.notePickIdx {
			plain := label
			if n.Title != "" {
				plain = n.Emoji + " " + n.Title
			} else if z.enrichingNotes[n.ID] {
				plain = "… labeling"
			} else {
				body := strings.TrimSpace(strings.ReplaceAll(n.Body, "\n", " "))
				plain = truncate(body, width-20)
			}
			lines = append(lines, marker(true)+s.ItemSel.Render(" "+plain+" "))
		} else {
			cur := ""
			if n.ID == z.editingNoteID {
				cur = s.Work.Render("● ")
			}
			lines = append(lines, cur+label)
		}
	}

	lines = append(lines, "", s.Help.Render("↑↓ move · enter open · n new · esc close"))
	if z.noteLabelErr != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(colRed).Render("label: "+truncate(z.noteLabelErr, width-8)))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 3).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// renderNotesReadOnly shows saved notes for a past session (history/stats).
func renderNotesReadOnly(notes []store.SessionNote, width, height int, s Styles) string {
	title := s.Title.Render("session notes")
	if len(notes) == 0 {
		body := s.Dim.Render("no notes for this session")
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).
			Padding(1, 3).
			Render(lipgloss.JoinVertical(lipgloss.Center, title, "", body, "", s.Help.Render("esc close")))
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}

	var lines []string
	for _, n := range notes {
		when := n.CreatedAt.Format("Jan 02 15:04")
		head := when
		if n.Title != "" {
			head = when + "  " + n.Emoji + " " + s.StatValue.Render(n.Title)
		}
		lines = append(lines, s.Dim.Render(head))
		for _, line := range strings.Split(n.Body, "\n") {
			lines = append(lines, "  "+line)
		}
		lines = append(lines, "")
	}

	contentW := width - 12
	if contentW < 30 {
		contentW = 30
	}
	contentH := height - 10
	if contentH < 6 {
		contentH = 6
	}

	scroll := lipgloss.NewStyle().Width(contentW).Height(contentH).Render(strings.Join(lines, "\n"))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			title+s.Dim.Render(fmt.Sprintf("  ·  %d", len(notes))),
			"",
			scroll,
			"",
			s.Help.Render("esc close"),
		))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
