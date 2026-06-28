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

type notesChatTurn struct {
	role    string
	content string
}

type notesSearchAnswerMsg struct {
	reqID    int
	question string
	answer   string
	results  []store.GlobalNote
	err      error
}

const (
	searchFocusQuery = iota
	searchFocusChat
	searchFocusResults
)

// notesSearchState powers note search + LLM chat in the shell Notes tab and the focus-zone note browser.
type notesSearchState struct {
	active bool
	focus  int

	queryInput panelInput
	chatInput  panelInput

	query        string
	results      []store.GlobalNote
	resultsPrelim bool
	resultCursor int
	chat         []notesChatTurn
	loading      bool
	err          string
	reqID        int
	onDismiss    func()
}

type noteSearchOpenHandler func(n store.GlobalNote)

func isSearchKey(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "/", "ctrl+f":
		return true
	}
	k := msg.Key()
	return k.Text == "/" || k.Code == '/'
}

func newSearchInput() panelInput {
	return newPanelInputTabBlur("ask  ", "what about whir do I know?", 500)
}

func newChatInput() panelInput {
	return newPanelInputTabBlur("  ", "ask a follow-up…", 500)
}

func noteSnippetsFromGlobal(notes []store.GlobalNote) []llm.NoteSnippet {
	out := make([]llm.NoteSnippet, len(notes))
	for i, n := range notes {
		out[i] = llm.NoteSnippet{
			ID:          n.ID,
			Title:       n.Title,
			Emoji:       n.Emoji,
			Body:        n.Body,
			TaskTitle:   n.TaskTitle,
			ProjectName: n.ProjectName,
			CreatedAt:   n.CreatedAt,
		}
	}
	return out
}

func (s *notesSearchState) open() tea.Cmd {
	s.active = true
	s.focus = searchFocusQuery
	s.query = ""
	s.results = nil
	s.resultsPrelim = false
	s.resultCursor = 0
	s.chat = nil
	s.loading = false
	s.err = ""
	s.queryInput = newSearchInput()
	s.chatInput = newChatInput()
	return s.queryInput.focusCmd()
}

func (s *notesSearchState) close() {
	s.active = false
	s.resultsPrelim = false
	s.queryInput.blur()
	s.chatInput.blur()
	if s.onDismiss != nil {
		s.onDismiss()
		s.onDismiss = nil
	}
}

func (s *notesSearchState) suspend() {
	s.active = false
	s.queryInput.blur()
	s.chatInput.blur()
}

func (s *notesSearchState) resume() tea.Cmd {
	s.active = true
	s.focus = searchFocusChat
	return s.chatInput.focusCmd()
}

func preliminaryNotesSearch(st *store.Store, query string) []store.GlobalNote {
	if st == nil {
		return nil
	}
	hits, err := st.SearchNotes(query, notesSearchResultLimit)
	if err != nil {
		return nil
	}
	return hits
}

func (s *notesSearchState) runSearch(st *store.Store, cfg *config.Config, query string) tea.Cmd {
	query = strings.TrimSpace(query)
	if query == "" || st == nil {
		return nil
	}
	s.query = query
	s.chat = nil
	s.err = ""
	s.results = preliminaryNotesSearch(st, query)
	s.resultsPrelim = true
	if len(s.results) > 0 {
		s.resultCursor = 0
	}

	s.focus = searchFocusChat
	s.chatInput.SetValue("")
	s.loading = true
	s.reqID++
	reqID := s.reqID
	return tea.Batch(searchNotesCmd(st, cfg, reqID, query, nil), s.chatInput.focusCmd())
}

func (s *notesSearchState) sendFollowUp(cfg *config.Config) tea.Cmd {
	msg := strings.TrimSpace(s.chatInput.Value())
	if msg == "" || s.loading {
		return nil
	}
	s.chatInput.SetValue("")
	history := s.chatHistory()
	s.loading = true
	s.reqID++
	reqID := s.reqID
	return s.answerCmd(cfg, reqID, msg, history)
}

func (s *notesSearchState) chatHistory() []llm.ChatTurn {
	var hist []llm.ChatTurn
	for _, t := range s.chat {
		hist = append(hist, llm.ChatTurn{Role: t.role, Content: t.content})
	}
	return hist
}

func (s *notesSearchState) answerCmd(cfg *config.Config, reqID int, question string, history []llm.ChatTurn) tea.Cmd {
	if err := noteLabelStatusErr(cfg); err != nil {
		return func() tea.Msg {
			return notesSearchAnswerMsg{reqID: reqID, question: question, err: err}
		}
	}
	ctx := llm.FormatNoteSnippets(noteSnippetsFromGlobal(s.results))
	baseURL := cfg.LMStudioURL
	model := cfg.LMStudioModel
	return func() tea.Msg {
		answer, err := llm.AnswerNotesQuestion(baseURL, model, question, ctx, history)
		if err != nil {
			return notesSearchAnswerMsg{reqID: reqID, question: question, err: err}
		}
		return notesSearchAnswerMsg{reqID: reqID, question: question, answer: answer}
	}
}

func (s *notesSearchState) onAnswer(msg notesSearchAnswerMsg) tea.Cmd {
	if msg.reqID != s.reqID {
		return nil
	}
	s.loading = false
	s.resultsPrelim = false
	if len(msg.results) > 0 {
		s.results = msg.results
		s.resultCursor = 0
	}
	if msg.err != nil {
		s.err = msg.err.Error()
	}
	if msg.answer != "" {
		s.err = ""
		s.chat = append(s.chat,
			notesChatTurn{role: "user", content: msg.question},
			notesChatTurn{role: "assistant", content: msg.answer},
		)
	}
	if !s.active {
		return nil
	}
	if s.focus != searchFocusChat {
		return nil
	}
	s.focus = searchFocusChat
	return s.chatInput.focusCmd()
}

func (s *notesSearchState) selectedResult() (store.GlobalNote, bool) {
	if s.resultCursor < 0 || s.resultCursor >= len(s.results) {
		return store.GlobalNote{}, false
	}
	return s.results[s.resultCursor], true
}

func (s *notesSearchState) openSelected(onOpen noteSearchOpenHandler) {
	if n, ok := s.selectedResult(); ok && onOpen != nil {
		s.suspend()
		onOpen(n)
	}
}

func (s *notesSearchState) inputFocused() bool {
	return s.queryInput.IsFocused() || s.chatInput.IsFocused()
}

func (s *notesSearchState) blurToResults() {
	s.queryInput.blur()
	s.chatInput.blur()
	if len(s.results) > 0 {
		s.focus = searchFocusResults
		s.resultCursor = clampInt(s.resultCursor, 0, len(s.results)-1)
		return
	}
	s.focus = searchFocusResults
	s.resultCursor = 0
}

func (s *notesSearchState) moveResult(delta int) {
	if len(s.results) == 0 {
		return
	}
	s.resultCursor = clampInt(s.resultCursor+delta, 0, len(s.results)-1)
}

func (s *notesSearchState) handleInput(msg tea.KeyPressMsg, st *store.Store, cfg *config.Config, onOpen noteSearchOpenHandler) tea.Cmd {
	return s.handleMsg(msg, st, cfg, onOpen)
}

func (s *notesSearchState) handleMsg(msg tea.Msg, st *store.Store, cfg *config.Config, onOpen noteSearchOpenHandler) tea.Cmd {
	if s.queryInput.IsFocused() {
		return s.handleQueryMsg(msg, st, cfg)
	}
	if s.chatInput.IsFocused() {
		return s.handleChatMsg(msg, st, cfg, onOpen)
	}
	return s.handleResultsKey(msg, onOpen)
}

func (s *notesSearchState) handleQueryMsg(msg tea.Msg, st *store.Store, cfg *config.Config) tea.Cmd {
	cmd, act := s.queryInput.handleMsg(msg)
	switch act {
	case panelInputCancel:
		s.blurToResults()
	case panelInputSubmit:
		if cmd2 := s.runSearch(st, cfg, s.queryInput.Value()); cmd2 != nil {
			return tea.Batch(cmd, cmd2)
		}
	case panelInputTab:
		if len(s.results) > 0 {
			s.blurToResults()
		} else {
			s.focus = searchFocusChat
			return tea.Batch(cmd, s.chatInput.focusCmd())
		}
	}
	return cmd
}

func (s *notesSearchState) handleChatMsg(msg tea.Msg, st *store.Store, cfg *config.Config, onOpen noteSearchOpenHandler) tea.Cmd {
	cmd, act := s.chatInput.handleMsg(msg)
	switch act {
	case panelInputCancel:
		s.blurToResults()
	case panelInputSubmit:
		if cmd2 := s.sendFollowUp(cfg); cmd2 != nil {
			return tea.Batch(cmd, cmd2)
		}
	case panelInputTab:
		if len(s.results) > 0 {
			s.blurToResults()
		} else {
			s.focus = searchFocusQuery
			return tea.Batch(cmd, s.queryInput.focusCmd())
		}
	}
	return cmd
}

func (s *notesSearchState) handleResultsKey(msg tea.Msg, onOpen noteSearchOpenHandler) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "esc":
		if s.inputFocused() {
			s.blurToResults()
		} else {
			s.close()
		}
	case "tab":
		s.focus = searchFocusChat
		return s.chatInput.focusCmd()
	case "up", "k":
		s.moveResult(-1)
	case "down", "j":
		s.moveResult(1)
	case "enter", "o":
		s.openSelected(onOpen)
	case "/":
		s.focus = searchFocusQuery
		return s.queryInput.focusCmd()
	case "i":
		s.focus = searchFocusChat
		return s.chatInput.focusCmd()
	}
	return nil
}

func (s *notesSearchState) render(width, height int, styles Styles) string {
	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}
	innerW := panelW - 4

	var sections []string
	sections = append(sections, styles.PaneTitle.Render("Search notes"))

	if s.focus == searchFocusQuery {
		sections = append(sections, s.queryInput.View())
	} else if s.query != "" {
		sections = append(sections, styles.Dim.Render("ask  ")+styles.StatValue.Render(s.query))
	} else {
		sections = append(sections, styles.Dim.Render("ask  …"))
	}

	sections = append(sections, "")

	sections = append(sections, styles.Dim.Render("answer"))
	if s.loading && len(s.chat) == 0 {
		sections = append(sections, styles.Dim.Render("searching notes…"))
	} else if s.loading && len(s.chat) > 0 {
		sections = append(sections, styles.Dim.Render("thinking…"))
	} else if len(s.chat) == 0 && s.err == "" && s.query == "" {
		sections = append(sections, styles.Dim.Render("ask a question about your notes"))
	} else {
		for _, turn := range s.chat {
			if turn.role == "user" {
				sections = append(sections, styles.HelpKey.Render("you")+"  "+turn.content)
			} else {
				for _, line := range wrapText(turn.content, innerW) {
					sections = append(sections, line)
				}
			}
			sections = append(sections, "")
		}
		if s.loading {
			sections = append(sections, styles.Dim.Render("thinking…"))
		}
	}
	if s.err != "" {
		sections = append(sections, lipgloss.NewStyle().Foreground(colRed).Render("error: "+truncate(s.err, innerW)))
	}

	sections = append(sections, "")
	if s.focus == searchFocusChat {
		sections = append(sections, s.chatInput.View())
	} else if s.query != "" {
		sections = append(sections, styles.Dim.Render("  ask a follow-up…"))
	}

	sections = append(sections, "")

	countLabel := "matching notes"
	if s.query != "" {
		if s.loading && s.resultsPrelim {
			countLabel = fmt.Sprintf("preliminary matches (%d)", len(s.results))
		} else {
			countLabel = fmt.Sprintf("matching notes (%d)", len(s.results))
		}
	}
	sections = append(sections, styles.Dim.Render(countLabel))
	if s.loading && s.resultsPrelim {
		sections = append(sections, styles.Dim.Render("refining with AI…"))
	}
	if s.query != "" && len(s.results) == 0 && !s.loading {
		sections = append(sections, styles.Dim.Render("no notes matched — try different words"))
	}
	maxResults := 6
	for i, n := range s.results {
		if i >= maxResults {
			sections = append(sections, styles.Dim.Render(fmt.Sprintf("… and %d more", len(s.results)-maxResults)))
			break
		}
		selected := s.focus == searchFocusResults && i == s.resultCursor
		line := renderSearchResultLine(n, innerW-2, styles)
		if selected {
			line = styles.ItemSel.Render(" "+line+" ")
		} else {
			line = "  " + line
		}
		sections = append(sections, line)
	}

	box := lipgloss.NewStyle().
		Width(panelW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(0, 1).
		Render(strings.Join(sections, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderSearchResultLine(n store.GlobalNote, innerW int, s Styles) string {
	when := n.CreatedAt.Format("Jan 2")
	label := notePickerPlainLabelCore(n.SessionNote, false)
	ctx := sessionTaskLabel(n.TaskTitle, n.ProjectName)
	if ctx != "" {
		label += s.Dim.Render("  ·  " + truncate(ctx, 24))
	}
	return truncate(label, innerW-8) + s.Dim.Render("  "+when)
}

func wrapText(text string, width int) []string {
	if width < 10 {
		width = 10
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		for len(para) > width {
			cut := width
			if idx := strings.LastIndex(para[:cut], " "); idx > width/2 {
				cut = idx
			}
			lines = append(lines, strings.TrimSpace(para[:cut]))
			para = strings.TrimSpace(para[cut:])
		}
		if para != "" {
			lines = append(lines, para)
		}
	}
	return lines
}

func searchActionHints(s Styles, focus int, hasResults bool) []string {
	var hints []string
	switch focus {
	case searchFocusQuery:
		hints = []string{s.helpEntry("esc", "normal"), s.helpEntry("enter", "search")}
		if hasResults {
			hints = append(hints, s.helpEntry("tab", "notes list"))
		}
	case searchFocusChat:
		hints = []string{s.helpEntry("esc", "normal"), s.helpEntry("enter", "send")}
		if hasResults {
			hints = append(hints, s.helpEntry("tab", "notes list"))
		}
	case searchFocusResults:
		hints = []string{
			s.helpEntry("esc", "close"),
			s.helpEntry("↑↓", "move"),
			s.helpEntry("enter", "open note"),
			s.helpEntry("o", "open note"),
			s.helpEntry("/", "edit query"),
			s.helpEntry("i", "follow-up"),
			s.helpEntry("tab", "follow-up"),
		}
	}
	return hints
}

func searchInfoHints(s Styles, cfg *config.Config, query string, nResults int, loading, preliminary bool) []string {
	info := s.Dim.Render("search")
	if query != "" {
		if loading && preliminary {
			info += s.Dim.Render(fmt.Sprintf("  ·  %d preliminary", nResults))
			info += s.Dim.Render("  ·  refining…")
		} else {
			info += s.Dim.Render(fmt.Sprintf("  ·  %d hits", nResults))
		}
	}
	if err := noteLabelStatusErr(cfg); err != nil {
		info += s.Dim.Render("  ·  " + truncate(err.Error(), 40))
	}
	return []string{info}
}
