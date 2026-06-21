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
	if z.store == nil {
		return
	}
	notes, _ := z.store.ListAllNotes(1000)
	z.noteRows = buildNoteBrowseRows(notes)
}

func (z *zoneView) notePickerTotalRows() int {
	return len(z.noteRows) + 1 // row 0 = "+ new note"
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

func (z *zoneView) notePickerListHeight() int {
	h := z.notePanelHeight() - 6
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
	n := z.notePickerTotalRows()
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
	skipLabel := false
	if z.editingNoteID != 0 {
		n, err := z.store.UpdateSessionNote(z.editingNoteID, body)
		if err != nil {
			return nil
		}
		id = n.ID
		updateNoteBrowseRow(z.noteRows, n)
		skipLabel = n.Title != ""
	} else {
		n, err := z.store.AddSessionNote(z.snap.SessionID, body)
		if err != nil {
			return nil
		}
		id = n.ID
		z.editingNoteID = n.ID
		z.reloadNotes()
	}
	return z.notePostSaveCmds(id, body, skipLabel)
}

func (z *zoneView) notePostSaveCmds(noteID int64, body string, skipLabel bool) tea.Cmd {
	var cmds []tea.Cmd
	if !skipLabel {
		if cmd := z.scheduleNoteLabel(noteID, body); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := z.scheduleNoteActionableScan(noteID, body); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (z *zoneView) scheduleNoteActionableScan(noteID int64, body string) tea.Cmd {
	return scanNoteActionablesCmd(z.cfg, z.scanningActionables, noteID, body)
}

func (z *zoneView) scanPendingActionablesCmd() tea.Cmd {
	return scanPendingActionablesCmd(z.cfg, z.scanningActionables, noteBrowseNotes(z.noteRows))
}

func (z *zoneView) labelStatusErr() error {
	return noteLabelStatusErr(z.cfg)
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
	for _, row := range z.noteRows {
		if row.kind != noteRowNote {
			continue
		}
		n := row.note.SessionNote
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
	updateNoteBrowseRow(z.noteRows, n)
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

func (z *zoneView) onNoteActionablesExtracted(msg noteActionablesExtractedMsg) {
	onNoteActionablesExtracted(
		z.store, msg,
		&z.actionablesNoteID, &z.actionablesItems,
		&z.extractingActionables, &z.actionablesExtractErr,
	)
}

func (z *zoneView) actionablesNoteBody() string {
	for _, row := range z.noteRows {
		if row.kind == noteRowNote && row.note.ID == z.actionablesNoteID {
			return row.note.Body
		}
	}
	return ""
}

func (z *zoneView) openActionablesPanel(note store.GlobalNote) tea.Cmd {
	z.viewingActionables = true
	z.actionablesNoteID = note.ID
	z.actionablesTitle = noteActionablesTitle(note.SessionNote)
	z.actionablesItems = nil
	z.actionablesExtractErr = ""
	z.extractingActionables = false
	if z.store != nil {
		if tasks, ok, err := z.store.LoadSessionNoteActionables(note.ID); err == nil && ok {
			z.actionablesItems = tasks
			return nil
		}
	}
	z.extractingActionables = true
	return extractNoteActionablesCmd(z.cfg, note.ID, note.Body)
}

func (z *zoneView) reextractActionables() tea.Cmd {
	body := z.actionablesNoteBody()
	if body == "" || z.actionablesNoteID == 0 {
		return nil
	}
	z.actionablesItems = nil
	z.actionablesExtractErr = ""
	z.extractingActionables = true
	return extractNoteActionablesCmd(z.cfg, z.actionablesNoteID, body)
}

func (z *zoneView) closeActionablesPanel() {
	z.viewingActionables = false
	z.actionablesNoteID = 0
	z.actionablesTitle = ""
	z.actionablesItems = nil
	z.extractingActionables = false
	z.actionablesExtractErr = ""
}

func (z *zoneView) handleActionablesPanelKey(key string) tea.Cmd {
	switch key {
	case "esc":
		z.closeActionablesPanel()
	case "r":
		return z.reextractActionables()
	}
	return nil
}

func (z *zoneView) onNoteActionablesScanned(msg noteActionablesScannedMsg) {
	delete(z.scanningActionables, msg.noteID)
	if msg.err != nil || z.store == nil {
		return
	}
	n, err := z.store.SetSessionNoteActionableScan(msg.noteID, msg.hasActionables)
	if err != nil {
		return
	}
	updateNoteBrowseRow(z.noteRows, n)
}

func (z *zoneView) zoneEditingNote() (store.GlobalNote, bool) {
	for _, row := range z.noteRows {
		if row.kind == noteRowNote && row.note.ID == z.editingNoteID {
			return row.note, true
		}
	}
	return store.GlobalNote{}, false
}

func (z *zoneView) revertNoteEditor() {
	if body := z.noteRowBody(z.editingNoteID); body != "" {
		z.noteEditor.Load(body)
		return
	}
	z.noteEditor.Reset()
}

func (z *zoneView) noteRowBody(noteID int64) string {
	for _, row := range z.noteRows {
		if row.kind == noteRowNote && row.note.ID == noteID {
			return row.note.Body
		}
	}
	return ""
}

func (z *zoneView) noteSavedBody() string {
	if z.editingNoteID == 0 {
		return ""
	}
	return z.noteRowBody(z.editingNoteID)
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
	return tea.Batch(z.enrichPendingCmd(), z.scanPendingActionablesCmd())
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
	if z.editingNoteID != 0 {
		z.notePickIdx = notePickIdxForNote(z.noteRows, z.editingNoteID)
	} else {
		z.notePickIdx = 0
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
	z.closeActionablesPanel()
	if z.cfg != nil {
		z.cfg.ResumeNotesSessionID = 0
		_ = z.cfg.Save()
	}
}

func (z *zoneView) deleteSelectedNote() {
	note, ok := noteBrowseNoteAt(z.noteRows, z.notePickIdx)
	if !ok || z.store == nil {
		return
	}
	if err := z.store.DeleteSessionNote(note.ID); err != nil {
		return
	}
	delete(z.enrichingNotes, note.ID)
	delete(z.scanningActionables, note.ID)
	if z.editingNoteID == note.ID {
		z.editingNoteID = 0
	}
	z.reloadNotes()
	if z.notePickIdx > z.notePickerTotalRows()-1 {
		z.notePickIdx = max(0, z.notePickerTotalRows()-1)
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
		pendingCmd := tea.Batch(z.enrichPendingCmd(), z.scanPendingActionablesCmd())
		if saveCmd != nil || pendingCmd != nil {
			return tea.Batch(cmd, saveCmd, pendingCmd)
		}
	case noteActSaveQuit:
		saveCmd = z.saveNoteDraft()
		z.noteEditor.Blur()
		z.noteEditor.Reset()
		z.openNotePicker()
		pendingCmd := tea.Batch(z.enrichPendingCmd(), z.scanPendingActionablesCmd())
		if saveCmd != nil {
			return tea.Batch(cmd, saveCmd, pendingCmd)
		}
		return tea.Batch(cmd, pendingCmd)
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
	if z.viewingActionables {
		return z.handleActionablesPanelKey(key)
	}
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

	switch key {
	case "up", "k":
		z.notePickIdx = moveNotePickIdx(z.noteRows, z.notePickIdx, -1)
		z.ensureNotePickVisible()
	case "down", "j":
		z.notePickIdx = moveNotePickIdx(z.noteRows, z.notePickIdx, 1)
		z.ensureNotePickVisible()
	case "enter", " ", "space":
		if z.notePickIdx == 0 {
			z.newNote()
		} else if note, ok := noteBrowseNoteAt(z.noteRows, z.notePickIdx); ok {
			z.loadNote(note.SessionNote)
		}
	case "esc":
		z.closeNotes(false)
	case "n":
		z.newNote()
	case "d":
		if _, ok := noteBrowseNoteAt(z.noteRows, z.notePickIdx); ok {
			z.confirmingNoteDelete = true
		}
	case "t":
		if note, ok := noteBrowseNoteAt(z.noteRows, z.notePickIdx); ok && note.HasActionables {
			return z.openActionablesPanel(note)
		}
	}
	return nil
}

func (z *zoneView) renderNotes(width, height int) string {
	z.noteEditor.Resize(z.noteEditorWidth(), z.noteEditorHeight())
	if z.viewingActionables {
		return renderNoteActionablesPanel(
			z.actionablesTitle, z.actionablesItems,
			z.extractingActionables, z.actionablesExtractErr,
			width, height, z.styles,
		)
	}
	if z.notePicking {
		z.ensureNotePickVisible()
		return z.renderNotePicker(width, height)
	}
	return z.renderNoteEditor(width, height)
}

func (z *zoneView) noteActionHints() []string {
	s := z.styles
	if z.viewingActionables {
		return noteActionablesActionHints(s)
	}
	if z.notePicking {
		if z.confirmingNoteDelete {
			label := "this note"
			if n, ok := noteBrowseNoteAt(z.noteRows, z.notePickIdx); ok {
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
		return z.notePickerActionHints()
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

func (z *zoneView) notePickerActionHints() []string {
	s := z.styles
	hints := []string{
		s.helpEntry("↑↓", "move"),
		s.helpEntry("enter", "open"),
		s.helpEntry("n", "new"),
		s.helpEntry("d", "delete"),
		s.helpEntry("esc", "back"),
	}
	if note, ok := noteBrowseNoteAt(z.noteRows, z.notePickIdx); ok && note.HasActionables {
		hints = append(hints, s.helpEntry("t", "tasks"))
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
		total := z.notePickerTotalRows()
		count := noteBrowseNoteCount(z.noteRows)
		if total > listH {
			return []string{s.Dim.Render(fmt.Sprintf("%d notes  ·  showing %d–%d rows",
				count, z.notePickOffset+1, min(z.notePickOffset+listH, total)))}
		}
		return []string{s.Dim.Render(fmt.Sprintf("%d notes", count))}
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

	title := accent.Render("notes")
	if n, ok := z.zoneEditingNote(); ok {
		task := sessionTaskLabel(n.TaskTitle, n.ProjectName)
		title += s.Dim.Render("  ·  ") + s.Subtitle.Render(task)
		sub := "editing"
		if n.Title != "" {
			sub = n.Emoji + " " + n.Title
		} else if z.enrichingNotes[n.ID] {
			sub = "labeling…"
		} else {
			sub = "unlabeled"
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
	if row == 0 {
		return notePickerMarker(z.notePickIdx == 0, s) + s.Item.Render("+ new note")
	}
	browseRow := z.noteRows[row-1]
	switch browseRow.kind {
	case noteRowSession:
		return renderSessionDivider(browseRow.session, innerW, s)
	default:
		n := browseRow.note.SessionNote
		selected := row == z.notePickIdx
		editing := n.ID == z.editingNoteID
		pendingLabel := z.enrichingNotes[n.ID]
		pendingScan := z.scanningActionables[n.ID]
		return renderNoteBrowseLine(n, selected, editing, pendingLabel, pendingScan, innerW, s)
	}
}

func (z *zoneView) renderNotePicker(width, height int) string {
	s := z.styles
	panelW := z.notePanelWidth()
	panelH := z.notePanelHeight()
	innerW := panelW - 4
	if innerW < 20 {
		innerW = 20
	}
	listH := z.notePickerListHeight()
	total := z.notePickerTotalRows()

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
		head += noteActionableSuffix(n, false, s)
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
