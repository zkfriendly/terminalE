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

// allNotesView is the global notes app: browse every session's notes and edit in place.
type allNotesView struct {
	store  *store.Store
	cfg    *config.Config
	styles Styles

	width, height int
	rows          []noteBrowseRow
	offset        int
	cursor        int

	picking              bool
	editor               vimNoteEditor
	editingNoteID        int64
	enrichingNotes       map[int64]bool
	scanningActionables  map[int64]bool
	noteLabelErr         string
	confirmingNoteDelete bool
}

func newAllNotes(st *store.Store, cfg *config.Config, s Styles) *allNotesView {
	v := &allNotesView{
		store:          st,
		cfg:            cfg,
		styles:         s,
		picking:        true,
		enrichingNotes:      map[int64]bool{},
		scanningActionables: map[int64]bool{},
	}
	v.editor = newVimNoteEditor(80, 20)
	v.reload()
	return v
}

func (v *allNotesView) pendingScanCmd() tea.Cmd {
	return scanPendingActionablesCmd(v.cfg, v.scanningActionables, noteBrowseNotes(v.rows))
}

func (v *allNotesView) initScanCmd() tea.Cmd {
	return v.pendingScanCmd()
}

func (v *allNotesView) reload() {
	notes, _ := v.store.ListAllNotes(1000)
	v.rows = buildNoteBrowseRows(notes)
	if len(v.rows) > 0 {
		v.cursor = v.firstNoteRow()
	} else {
		v.cursor = 0
	}
	v.clampCursor()
	v.clampOffset()
}

func (v *allNotesView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if v.picking {
			return v.handleBrowseKey(msg.String())
		}
		return v.handleEditorKey(msg)
	}
	return nil
}

func (v *allNotesView) handleBrowseKey(key string) tea.Cmd {
	if v.confirmingNoteDelete {
		switch key {
		case "y", "enter":
			v.confirmingNoteDelete = false
			v.deleteSelectedNote()
		case "n", "esc":
			v.confirmingNoteDelete = false
		default:
			v.confirmingNoteDelete = false
		}
		return nil
	}

	switch key {
	case "tab":
		return shellToggleFocusCmd()
	case "up", "k":
		v.move(-1)
	case "down", "j":
		v.move(1)
	case "pgup":
		v.move(-v.listHeight())
	case "pgdown", " ", "space":
		v.move(v.listHeight())
	case "g", "home":
		v.cursor = v.firstNoteRow()
	case "G", "end":
		v.cursor = v.lastNoteRow()
	case "enter":
		v.openSelectedNote()
	case "d":
		if _, ok := v.selectedNote(); ok {
			v.confirmingNoteDelete = true
		}
	case "r":
		v.reload()
		return v.pendingScanCmd()
	}
	v.clampCursor()
	v.ensureCursorVisible()
	return nil
}

func (v *allNotesView) handleEditorKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "esc" && v.editor.mode == noteModeNormal {
		return v.tryBrowse()
	}

	cmd, act := v.editor.Update(msg)
	var saveCmd tea.Cmd
	switch act {
	case noteActSave:
		saveCmd = v.saveDraft()
		pendingCmd := tea.Batch(v.enrichPendingCmd(), v.pendingScanCmd())
		if saveCmd != nil || pendingCmd != nil {
			return tea.Batch(cmd, saveCmd, pendingCmd)
		}
	case noteActSaveQuit:
		saveCmd = v.saveDraft()
		v.editor.Blur()
		v.openBrowse()
		pendingCmd := tea.Batch(v.enrichPendingCmd(), v.pendingScanCmd())
		if saveCmd != nil {
			return tea.Batch(cmd, saveCmd, pendingCmd)
		}
		return tea.Batch(cmd, pendingCmd)
	case noteActBrowse:
		saveCmd = v.tryBrowse()
	case noteActQuit:
		v.revertEditor()
		v.openBrowse()
	}
	if saveCmd != nil {
		return tea.Batch(cmd, saveCmd)
	}
	return cmd
}

func (v *allNotesView) openBrowse() {
	v.picking = true
	v.syncCursorToEditingNote()
	v.offset = 0
	v.ensureCursorVisible()
}

func (v *allNotesView) openSelectedNote() {
	n, ok := v.selectedNote()
	if !ok {
		return
	}
	v.editingNoteID = n.ID
	v.editor.Load(n.Body)
	v.editor.Resize(v.editorWidth(), v.editorHeight())
	v.picking = false
}

func (v *allNotesView) syncCursorToEditingNote() {
	if v.editingNoteID == 0 {
		return
	}
	for i, row := range v.rows {
		if row.kind == noteRowNote && row.note.ID == v.editingNoteID {
			v.cursor = i
			return
		}
	}
}

func (v *allNotesView) tryBrowse() tea.Cmd {
	if v.noteIsDirty() {
		return nil
	}
	v.revertEditor()
	v.openBrowse()
	return tea.Batch(v.enrichPendingCmd(), v.pendingScanCmd())
}

func (v *allNotesView) saveDraft() tea.Cmd {
	if v.store == nil || v.editingNoteID == 0 {
		return nil
	}
	body := strings.TrimSpace(v.editor.Value())
	if body == "" {
		return nil
	}
	n, err := v.store.UpdateSessionNote(v.editingNoteID, body)
	if err != nil {
		return nil
	}
	v.updateRowNote(n)
	return v.notePostSaveCmds(n.ID, body, n.Title != "")
}

func (v *allNotesView) notePostSaveCmds(noteID int64, body string, skipLabel bool) tea.Cmd {
	var cmds []tea.Cmd
	if !skipLabel {
		if cmd := v.scheduleNoteLabel(noteID, body); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := scanNoteActionablesCmd(v.cfg, v.scanningActionables, noteID, body); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (v *allNotesView) updateRowNote(n store.SessionNote) {
	updateNoteBrowseRow(v.rows, n)
}

func (v *allNotesView) noteSavedBody() string {
	if v.editingNoteID == 0 {
		return ""
	}
	for _, row := range v.rows {
		if row.kind == noteRowNote && row.note.ID == v.editingNoteID {
			return row.note.Body
		}
	}
	return ""
}

func (v *allNotesView) noteIsDirty() bool {
	return strings.TrimSpace(v.editor.Value()) != strings.TrimSpace(v.noteSavedBody())
}

func (v *allNotesView) revertEditor() {
	if v.editingNoteID != 0 {
		for _, row := range v.rows {
			if row.kind == noteRowNote && row.note.ID == v.editingNoteID {
				v.editor.Load(row.note.Body)
				return
			}
		}
	}
	v.editor.Reset()
}

func (v *allNotesView) deleteSelectedNote() {
	n, ok := v.selectedNote()
	if !ok || v.store == nil {
		return
	}
	if err := v.store.DeleteSessionNote(n.ID); err != nil {
		return
	}
	delete(v.enrichingNotes, n.ID)
	delete(v.scanningActionables, n.ID)
	if v.editingNoteID == n.ID {
		v.editingNoteID = 0
	}
	v.reload()
	v.move(0)
	v.ensureCursorVisible()
}

func (v *allNotesView) scheduleNoteLabel(noteID int64, body string) tea.Cmd {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	if err := noteLabelStatusErr(v.cfg); err != nil {
		return func() tea.Msg {
			return noteEnrichedMsg{noteID: noteID, err: err}
		}
	}
	return v.enrichNoteCmd(noteID, body)
}

func (v *allNotesView) enrichNoteCmd(noteID int64, body string) tea.Cmd {
	if err := noteLabelStatusErr(v.cfg); err != nil {
		return func() tea.Msg {
			return noteEnrichedMsg{noteID: noteID, err: err}
		}
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	v.enrichingNotes[noteID] = true
	baseURL := v.cfg.LMStudioURL
	model := v.cfg.LMStudioModel
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

func (v *allNotesView) enrichPendingCmd() tea.Cmd {
	var cmds []tea.Cmd
	for _, row := range v.rows {
		if row.kind != noteRowNote {
			continue
		}
		n := row.note.SessionNote
		if n.Title != "" || strings.TrimSpace(n.Body) == "" || v.enrichingNotes[n.ID] {
			continue
		}
		cmds = append(cmds, v.scheduleNoteLabel(n.ID, n.Body))
	}
	return tea.Batch(cmds...)
}

func (v *allNotesView) onNoteActionablesScanned(msg noteActionablesScannedMsg) {
	delete(v.scanningActionables, msg.noteID)
	if msg.err != nil || v.store == nil {
		return
	}
	n, err := v.store.SetSessionNoteActionableScan(msg.noteID, msg.hasActionables)
	if err != nil {
		return
	}
	v.updateRowNote(n)
}

func (v *allNotesView) onNoteEnriched(msg noteEnrichedMsg) {
	delete(v.enrichingNotes, msg.noteID)
	if msg.err != nil {
		v.noteLabelErr = msg.err.Error()
		return
	}
	if v.store == nil {
		return
	}
	v.noteLabelErr = ""
	n, err := v.store.SetSessionNoteMeta(msg.noteID, msg.note.Title, msg.note.Emoji)
	if err != nil {
		v.noteLabelErr = err.Error()
		return
	}
	v.updateRowNote(n)
}

func (v *allNotesView) firstNoteRow() int {
	for i, row := range v.rows {
		if row.kind == noteRowNote {
			return i
		}
	}
	return 0
}

func (v *allNotesView) lastNoteRow() int {
	for i := len(v.rows) - 1; i >= 0; i-- {
		if v.rows[i].kind == noteRowNote {
			return i
		}
	}
	return 0
}

func (v *allNotesView) move(delta int) {
	if len(v.rows) == 0 {
		return
	}
	start := v.cursor
	for i := 0; i < len(v.rows); i++ {
		next := v.cursor + delta
		if next < 0 {
			next = len(v.rows) - 1
		}
		if next >= len(v.rows) {
			next = 0
		}
		v.cursor = next
		if v.rows[v.cursor].kind == noteRowNote {
			return
		}
		if v.cursor == start {
			return
		}
	}
}

func (v *allNotesView) selectedNote() (store.GlobalNote, bool) {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return store.GlobalNote{}, false
	}
	row := v.rows[v.cursor]
	if row.kind != noteRowNote {
		return store.GlobalNote{}, false
	}
	return row.note, true
}

func (v *allNotesView) noteCount() int {
	n := 0
	for _, row := range v.rows {
		if row.kind == noteRowNote {
			n++
		}
	}
	return n
}

func (v *allNotesView) clampCursor() {
	if len(v.rows) == 0 {
		v.cursor = 0
		return
	}
	v.cursor = clampInt(v.cursor, 0, len(v.rows)-1)
}

func (v *allNotesView) listHeight() int {
	if v.picking {
		return v.notePickerListHeight()
	}
	n := v.height - 1
	if n < 1 {
		n = 1
	}
	return n
}

func (v *allNotesView) ensureCursorVisible() {
	page := v.listHeight()
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if v.cursor >= v.offset+page {
		v.offset = v.cursor - page + 1
	}
	v.clampOffset()
}

func (v *allNotesView) clampOffset() {
	maxOff := len(v.rows) - v.listHeight()
	if maxOff < 0 {
		maxOff = 0
	}
	v.offset = clampInt(v.offset, 0, maxOff)
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if v.cursor >= v.offset+v.listHeight() && v.listHeight() > 0 {
		v.offset = v.cursor - v.listHeight() + 1
	}
}

func (v *allNotesView) notePanelWidth() int {
	w := v.width - 8
	if w < 30 {
		w = 30
	}
	return w
}

func (v *allNotesView) notePanelHeight() int {
	h := v.height - 6
	if h < 10 {
		h = 10
	}
	return h
}

func (v *allNotesView) notePickerListHeight() int {
	h := v.notePanelHeight() - 6
	if v.noteLabelErr != "" {
		h -= 2
	}
	if h < 1 {
		h = 1
	}
	return h
}

func (v *allNotesView) editorWidth() int {
	w := v.width - 14
	if w < 30 {
		w = 30
	}
	return w
}

func (v *allNotesView) editorHeight() int {
	h := v.height - 16
	if h < 4 {
		h = 4
	}
	return h
}

func (v *allNotesView) renderBody(width, height int) string {
	v.width, v.height = width, height
	if width == 0 {
		return "loading notes..."
	}
	if v.picking {
		v.ensureCursorVisible()
		return v.renderBrowse(width, height)
	}
	v.editor.Resize(v.editorWidth(), v.editorHeight())
	return v.renderEditor(width, height)
}

func (v *allNotesView) renderBrowse(width, height int) string {
	s := v.styles
	panelW := v.notePanelWidth()
	panelH := v.notePanelHeight()
	innerW := panelW - 4
	if innerW < 20 {
		innerW = 20
	}
	listH := v.notePickerListHeight()

	if len(v.rows) == 0 {
		body := s.Dim.Render("no notes yet") + "\n\n" + s.Dim.Render("take notes during a focus session (press n in the zone)")
		box := lipgloss.NewStyle().
			Width(panelW).
			Height(panelH).
			Padding(0, 2).
			Render(body)
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}

	var lines []string
	lines = append(lines, s.PaneTitle.Render("Open note"), "")

	end := v.offset + listH
	if end > len(v.rows) {
		end = len(v.rows)
	}
	for i, row := range v.rows[v.offset:end] {
		selected := v.offset+i == v.cursor
		switch row.kind {
		case noteRowSession:
			lines = append(lines, renderSessionDivider(row.session, innerW, s))
		default:
			n := row.note.SessionNote
			editing := n.ID == v.editingNoteID
			pendingLabel := v.enrichingNotes[n.ID]
			pendingScan := v.scanningActionables[n.ID]
			lines = append(lines, renderNoteBrowseLine(n, selected, editing, pendingLabel, pendingScan, innerW, s))
		}
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

func (v *allNotesView) renderEditor(width, height int) string {
	s := v.styles
	accent := lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	title := accent.Render("notes")
	if n, ok := v.editingNote(); ok {
		task := sessionTaskLabel(n.TaskTitle, n.ProjectName)
		title += s.Dim.Render("  ·  ") + s.Subtitle.Render(task)
		sub := "editing"
		if n.Title != "" {
			sub = n.Emoji + " " + n.Title
		} else if v.enrichingNotes[n.ID] {
			sub = "labeling…"
		} else {
			sub = "unlabeled"
		}
		if strings.Contains(sub, " ") && !strings.HasSuffix(sub, "…") {
			title += "  " + s.StatValue.Render(sub)
		} else {
			title += s.Dim.Render("  ·  " + sub)
		}
	}

	var lines []string
	lines = append(lines, title, "", v.editor.View())

	box := lipgloss.NewStyle().
		Padding(0, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func (v *allNotesView) editingNote() (store.GlobalNote, bool) {
	if v.editingNoteID == 0 {
		return store.GlobalNote{}, false
	}
	for _, row := range v.rows {
		if row.kind == noteRowNote && row.note.ID == v.editingNoteID {
			return row.note, true
		}
	}
	return store.GlobalNote{}, false
}

func (v *allNotesView) actionHints() []string {
	s := v.styles
	if v.picking {
		if v.confirmingNoteDelete {
			label := "this note"
			if n, ok := v.selectedNote(); ok {
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
			s.helpEntry("d", "delete"),
			s.helpEntry("g/G", "top/bottom"),
			s.helpEntry("r", "refresh"),
		}
	}
	hints := []string{
		s.helpEntry(":w", "save"),
		s.helpEntry(":wq/ZZ", "save & browse"),
	}
	if !v.noteIsDirty() {
		hints = append(hints, s.helpEntry(":e/esc", "browse"))
	}
	return hints
}

func (v *allNotesView) infoHints() []string {
	s := v.styles
	if !v.picking {
		var hints []string
		hints = append(hints, modeStyle(v.editor.mode, s).Render(v.editor.ModeLabel()))
		if v.noteIsDirty() {
			hints = append(hints, s.Break.Render("unsaved changes"))
		}
		if v.noteLabelErr != "" {
			hints = append(hints, lipgloss.NewStyle().Foreground(colRed).Render("label: "+truncate(v.noteLabelErr, 48)))
		} else if err := noteLabelStatusErr(v.cfg); err != nil {
			hints = append(hints, s.Dim.Render(truncate(err.Error(), 48)))
		}
		return hints
	}
	total := v.noteCount()
	if total == 0 {
		return []string{s.Dim.Render("0 notes")}
	}
	listH := v.notePickerListHeight()
	end := v.offset + listH
	if end > len(v.rows) {
		end = len(v.rows)
	}
	info := s.Dim.Render(fmt.Sprintf("%d notes", total))
	if len(v.rows) > listH {
		info += s.Dim.Render(fmt.Sprintf("  ·  showing %d–%d rows", v.offset+1, end))
	}
	return []string{info}
}

func (v *allNotesView) escIsLocal() bool {
	if v.picking {
		return v.confirmingNoteDelete
	}
	return true
}
