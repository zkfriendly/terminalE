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
	return w
}

func (z *zoneView) noteEditorHeight() int {
	h := z.height - 16
	if h < 4 {
		h = 4
	}
	return h
}

func (z *zoneView) notePanelWidth() int {
	w := z.width - 8
	if w < 30 {
		w = 30
	}
	return w
}

func (z *zoneView) notePanelHeight() int {
	h := z.height - 6
	if h < 10 {
		h = 10
	}
	return h
}

// notePickerListHeight is how many note rows fit in the browse panel.
func (z *zoneView) notePickerListHeight() int {
	h := z.notePanelHeight() - 6 // title, spacing, padding
	if z.noteLabelErr != "" {
		h -= 2
	}
	if h < 1 {
		h = 1
	}
	return h
}

func (z *zoneView) ensureNotePickVisible() {
	listH := z.notePickerListHeight()
	n := len(z.notes) + 1
	if z.notePickIdx < z.notePickOffset {
		z.notePickOffset = z.notePickIdx
	}
	if z.notePickIdx >= z.notePickOffset+listH {
		z.notePickOffset = z.notePickIdx - listH + 1
	}
	maxOffset := max(0, n-listH)
	if z.notePickOffset > maxOffset {
		z.notePickOffset = maxOffset
	}
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
	return z.scheduleNoteLabel(id, body)
}

func (z *zoneView) labelStatusErr() error {
	if z.cfg == nil {
		return fmt.Errorf("labeling unavailable (config missing)")
	}
	if !z.cfg.LMStudioEnabled {
		return fmt.Errorf("LM Studio labeling disabled — set lm_studio_enabled to true in config.json")
	}
	if z.cfg.LMStudioURL == "" {
		return fmt.Errorf("lm_studio_url is empty in config.json")
	}
	return nil
}

func (z *zoneView) labelErrorMsg(noteID int64, err error) tea.Msg {
	return noteEnrichedMsg{noteID: noteID, err: err}
}

func (z *zoneView) scheduleNoteLabel(noteID int64, body string) tea.Cmd {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	if err := z.labelStatusErr(); err != nil {
		return func() tea.Msg { return z.labelErrorMsg(noteID, err) }
	}
	return z.enrichNoteCmd(noteID, body)
}

func (z *zoneView) enrichNoteCmd(noteID int64, body string) tea.Cmd {
	if err := z.labelStatusErr(); err != nil {
		return func() tea.Msg { return z.labelErrorMsg(noteID, err) }
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
	var cmds []tea.Cmd
	for _, n := range z.notes {
		if n.Title != "" || strings.TrimSpace(n.Body) == "" || z.enrichingNotes[n.ID] {
			continue
		}
		cmds = append(cmds, z.scheduleNoteLabel(n.ID, n.Body))
	}
	return tea.Batch(cmds...)
}

func (z *zoneView) persistNoteMeta(noteID int64, title, emoji string) {
	if z.store == nil {
		return
	}
	n, err := z.store.SetSessionNoteMeta(noteID, title, emoji)
	if err != nil {
		z.noteLabelErr = err.Error()
		return
	}
	for i := range z.notes {
		if z.notes[i].ID == noteID {
			z.notes[i] = n
			return
		}
	}
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
	z.noteLabelErr = ""
	z.persistNoteMeta(msg.noteID, msg.note.Title, msg.note.Emoji)
}

func (z *zoneView) revertNoteEditor() {
	if z.editingNoteID != 0 {
		for _, n := range z.notes {
			if n.ID == z.editingNoteID {
				z.noteEditor.Load(n.Body)
				return
			}
		}
	}
	z.noteEditor.Reset()
}

func (z *zoneView) noteSavedBody() string {
	if z.editingNoteID == 0 {
		return ""
	}
	for _, n := range z.notes {
		if n.ID == z.editingNoteID {
			return n.Body
		}
	}
	return ""
}

func (z *zoneView) noteIsDirty() bool {
	return strings.TrimSpace(z.noteEditor.Value()) != strings.TrimSpace(z.noteSavedBody())
}

func (z *zoneView) tryBrowseNotes() tea.Cmd {
	if z.noteIsDirty() {
		return nil
	}
	z.revertNoteEditor()
	z.openNotePicker()
	return z.enrichPendingCmd()
}

func (z *zoneView) newNote() {
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
	z.notePickOffset = 0
	z.ensureNotePickVisible()
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
	z.confirmingNoteDelete = false
	if z.cfg != nil {
		z.cfg.ResumeNotesSessionID = 0
		_ = z.cfg.Save()
	}
}

func (z *zoneView) deleteSelectedNote() {
	if z.store == nil || z.notePickIdx == 0 {
		return
	}
	note := z.notes[z.notePickIdx-1]
	if err := z.store.DeleteSessionNote(note.ID); err != nil {
		return
	}
	delete(z.enrichingNotes, note.ID)
	if z.editingNoteID == note.ID {
		z.editingNoteID = 0
	}
	z.notes = append(z.notes[:z.notePickIdx-1], z.notes[z.notePickIdx:]...)
	if z.notePickIdx > len(z.notes) {
		z.notePickIdx = len(z.notes)
	}
	z.ensureNotePickVisible()
}

func (z *zoneView) handleNotesKey(msg tea.KeyPressMsg) tea.Cmd {
	if z.notePicking {
		return z.handleNotePickerKey(msg.String())
	}

	k := msg.String()
	if k == "esc" && z.noteEditor.mode == noteModeNormal {
		return z.tryBrowseNotes()
	}

	cmd, act := z.noteEditor.Update(msg)
	var saveCmd tea.Cmd
	switch act {
	case noteActSave:
		saveCmd = z.saveNoteDraft()
		pendingCmd := z.enrichPendingCmd()
		if saveCmd != nil || pendingCmd != nil {
			return tea.Batch(cmd, saveCmd, pendingCmd)
		}
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
		saveCmd = z.tryBrowseNotes()
	case noteActNew:
		z.newNote()
	}
	if saveCmd != nil {
		return tea.Batch(cmd, saveCmd)
	}
	return cmd
}

func (z *zoneView) handleNotePickerKey(key string) tea.Cmd {
	if z.confirmingNoteDelete {
		switch key {
		case "y", "enter":
			z.confirmingNoteDelete = false
			z.deleteSelectedNote()
		case "n", "esc":
			z.confirmingNoteDelete = false
		default:
			z.confirmingNoteDelete = false
		}
		return nil
	}

	n := len(z.notes) + 1 // row 0 = new note
	switch key {
	case "up", "k":
		z.notePickIdx = clampInt(z.notePickIdx-1, 0, n-1)
		z.ensureNotePickVisible()
	case "down", "j":
		z.notePickIdx = clampInt(z.notePickIdx+1, 0, n-1)
		z.ensureNotePickVisible()
	case "enter", " ", "space":
		if z.notePickIdx == 0 {
			z.newNote()
		} else {
			z.loadNote(z.notes[z.notePickIdx-1])
		}
	case "esc":
		z.closeNotes(false)
	case "n":
		z.newNote()
	case "d":
		if z.notePickIdx > 0 {
			z.confirmingNoteDelete = true
		}
	}
	return nil
}

func (z *zoneView) renderNotes(width, height int) string {
	// Keep the editor sized to the current terminal so it grows/shrinks
	// with window resizes that happen while notes are open.
	z.noteEditor.Resize(z.noteEditorWidth(), z.noteEditorHeight())
	if z.notePicking {
		z.ensureNotePickVisible()
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
	return s.Dim.Render(when) + "  " + lipgloss.NewStyle().Foreground(colRed).Render("unlabeled")
}

func (z *zoneView) noteActionHints() []string {
	s := z.styles
	if z.notePicking {
		if z.confirmingNoteDelete {
			label := "this note"
			if z.notePickIdx > 0 {
				n := z.notes[z.notePickIdx-1]
				if n.Title != "" {
					label = n.Emoji + " " + n.Title
				} else {
					body := strings.TrimSpace(strings.ReplaceAll(n.Body, "\n", " "))
					label = truncate(body, 40)
				}
			}
			return []string{
				s.Break.Render("delete "+label+" permanently?") + " " +
					s.helpEntry("y", "yes") + s.Dim.Render("  ·  ") + s.helpEntry("n", "no"),
			}
		}
		return []string{
			s.helpEntry("↑↓", "move"),
			s.helpEntry("enter", "open"),
			s.helpEntry("n", "new"),
			s.helpEntry("d", "delete"),
			s.helpEntry("esc", "back"),
		}
	}
	hints := []string{
		s.helpEntry(":w", "save"),
		s.helpEntry(":wq/ZZ", "save & browse"),
		s.helpEntry(":new", "new note"),
	}
	if !z.noteIsDirty() {
		hints = append(hints, s.helpEntry(":e/esc", "browse"))
	}
	return hints
}

func (z *zoneView) noteInfoHints() []string {
	s := z.styles
	if !z.noting {
		return nil
	}
	if z.notePicking {
		listH := z.notePickerListHeight()
		total := len(z.notes) + 1
		if total > listH {
			return []string{s.Dim.Render(fmt.Sprintf("%d–%d of %d notes",
				z.notePickOffset+1, min(z.notePickOffset+listH, total), total))}
		}
		return []string{s.Dim.Render(fmt.Sprintf("%d notes", len(z.notes)))}
	}
	var hints []string
	hints = append(hints, modeStyle(z.noteEditor.mode, s).Render(z.noteEditor.ModeLabel()))
	if z.noteIsDirty() {
		hints = append(hints, s.Break.Render("unsaved changes"))
	}
	if z.noteLabelErr != "" {
		hints = append(hints, lipgloss.NewStyle().Foreground(colRed).Render("label: "+truncate(z.noteLabelErr, 48)))
	} else if err := z.labelStatusErr(); err != nil {
		hints = append(hints, s.Dim.Render(truncate(err.Error(), 48)))
	}
	return hints
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
				} else {
					sub = "unlabeled"
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

	var lines []string
	lines = append(lines, title, "", z.noteEditor.View())

	box := lipgloss.NewStyle().
		Padding(0, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func (z *zoneView) renderNotePickerRow(row, innerW int, s Styles) string {
	marker := func(active bool) string {
		if active {
			return s.Work.Render("● ")
		}
		return "  "
	}
	if row == 0 {
		return marker(z.notePickIdx == 0) + s.Item.Render("+ new note")
	}
	n := z.notes[row-1]
	label := noteListLabel(n, z.enrichingNotes[n.ID], innerW, s)
	if row == z.notePickIdx {
		plain := label
		if n.Title != "" {
			plain = n.Emoji + " " + n.Title
		} else if z.enrichingNotes[n.ID] {
			plain = "… labeling"
		} else {
			plain = "unlabeled"
		}
		return marker(true) + s.ItemSel.Render(" "+plain+" ")
	}
	cur := ""
	if n.ID == z.editingNoteID {
		cur = s.Work.Render("● ")
	}
	return cur + label
}

func (z *zoneView) renderNotePicker(width, height int) string {
	s := z.styles
	panelW := z.notePanelWidth()
	panelH := z.notePanelHeight()
	innerW := panelW - 4 // horizontal padding
	if innerW < 20 {
		innerW = 20
	}
	listH := z.notePickerListHeight()
	total := len(z.notes) + 1

	var lines []string
	lines = append(lines, s.PaneTitle.Render("Open note"), "")

	for row := z.notePickOffset; row < z.notePickOffset+listH && row < total; row++ {
		lines = append(lines, z.renderNotePickerRow(row, innerW, s))
	}
	for len(lines) < 2+listH {
		lines = append(lines, "")
	}

	box := lipgloss.NewStyle().
		Width(panelW).
		Height(panelH).
		Padding(0, 2).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// renderNotesReadOnly shows saved notes for a past session (history/stats).
func renderNotesReadOnly(notes []store.SessionNote, width, height int, s Styles) string {
	title := s.Title.Render("session notes")
	if len(notes) == 0 {
		body := s.Dim.Render("no notes for this session")
		box := lipgloss.NewStyle().
			Padding(0, 2).
			Render(lipgloss.JoinVertical(lipgloss.Center, title, "", body))
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}

	var lines []string
	for _, n := range notes {
		when := n.CreatedAt.Format("Jan 02 15:04")
		head := when
		if n.Title != "" {
			head = when + "  " + n.Emoji + " " + s.StatValue.Render(n.Title)
		} else {
			head = when + "  " + lipgloss.NewStyle().Foreground(colRed).Render("unlabeled")
		}
		lines = append(lines, s.Dim.Render(head))
		for _, line := range strings.Split(n.Body, "\n") {
			lines = append(lines, "  "+line)
		}
		lines = append(lines, "")
	}

	contentW := width - 4
	if contentW < 30 {
		contentW = 30
	}
	contentH := height - 6
	if contentH < 6 {
		contentH = 6
	}

	scroll := lipgloss.NewStyle().Width(contentW).Height(contentH).Render(strings.Join(lines, "\n"))
	box := lipgloss.NewStyle().
		Padding(0, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			title+s.Dim.Render(fmt.Sprintf("  ·  %d", len(notes))),
			"",
			scroll,
		))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
