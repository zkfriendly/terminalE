package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type noteSearchState struct {
	pattern  string
	back     bool
	matches  []selPos
	matchIdx int
}

func findNoteMatches(text, pattern string) []selPos {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil
	}
	pl := []rune(strings.ToLower(pattern))
	if len(pl) == 0 {
		return nil
	}
	var matches []selPos
	for li, line := range noteLines(text) {
		lr := []rune(strings.ToLower(line))
		for ci := 0; ci <= len(lr)-len(pl); ci++ {
			if string(lr[ci:ci+len(pl)]) == string(pl) {
				matches = append(matches, selPos{line: li, col: ci})
			}
		}
	}
	return matches
}

func searchStartIndex(matches []selPos, cur selPos, back, fromCursor bool) int {
	if len(matches) == 0 {
		return -1
	}
	if !fromCursor {
		if back {
			return len(matches) - 1
		}
		return 0
	}
	best := -1
	for i, m := range matches {
		if back {
			if posBefore(m, cur) && (best < 0 || posAfter(matches[best], m)) {
				best = i
			}
			continue
		}
		if posAfterOrEqual(m, cur) && (best < 0 || posBefore(m, matches[best])) {
			best = i
		}
	}
	if best >= 0 {
		return best
	}
	if back {
		return len(matches) - 1
	}
	return 0
}

func posBefore(a, b selPos) bool {
	return a.line < b.line || (a.line == b.line && a.col < b.col)
}

func posAfter(a, b selPos) bool {
	return a.line > b.line || (a.line == b.line && a.col > b.col)
}

func posAfterOrEqual(a, b selPos) bool {
	return !posBefore(a, b)
}

func (e *vimNoteEditor) beginSearch(back bool) {
	e.mode = noteModeSearch
	e.searchBack = back
	e.searchLine = ""
	e.clearPending()
	e.ta.Blur()
}

func (e *vimNoteEditor) cancelSearch() {
	e.mode = noteModeNormal
	e.searchLine = ""
	e.ta.Focus()
}

func (e *vimNoteEditor) runSearch(fromCursor bool) {
	pattern := strings.TrimSpace(e.searchLine)
	e.mode = noteModeNormal
	e.searchLine = ""
	e.ta.Focus()
	if pattern == "" {
		return
	}
	matches := findNoteMatches(e.ta.Value(), pattern)
	cur := selPos{e.ta.Line(), e.ta.Column()}
	idx := searchStartIndex(matches, cur, e.searchBack, fromCursor)
	e.lastSearch = noteSearchState{
		pattern:  pattern,
		back:     e.searchBack,
		matches:  matches,
		matchIdx: idx,
	}
	if idx >= 0 {
		e.gotoPos(matches[idx])
	}
}

func (e *vimNoteEditor) gotoPos(p selPos) {
	e.ta.MoveToBegin()
	for i := 0; i < p.line; i++ {
		e.feed("down")
	}
	e.ta.SetCursorColumn(p.col)
}

func (e *vimNoteEditor) searchNext(reverse bool) {
	s := e.lastSearch
	if s.pattern == "" || len(s.matches) == 0 {
		return
	}
	dir := 1
	if s.back {
		dir = -1
	}
	if reverse {
		dir = -dir
	}
	idx := s.matchIdx
	if idx < 0 {
		idx = 0
	} else {
		idx = (idx + dir + len(s.matches)) % len(s.matches)
	}
	e.lastSearch.matchIdx = idx
	e.gotoPos(s.matches[idx])
}

func (e *vimNoteEditor) handleSearchKey(msg tea.KeyPressMsg) (tea.Cmd, noteAction) {
	k := msg.String()
	switch k {
	case "esc":
		e.cancelSearch()
		return nil, noteActNone
	case "enter":
		e.runSearch(true)
		return nil, noteActNone
	case "backspace":
		if len(e.searchLine) > 0 {
			e.searchLine = e.searchLine[:len(e.searchLine)-1]
		}
		if e.searchLine == "" {
			e.cancelSearch()
		}
		return nil, noteActNone
	default:
		if len(k) == 1 && k[0] >= 32 {
			e.searchLine += k
		}
		return nil, noteActNone
	}
}

func (e *vimNoteEditor) searchLabel() string {
	if e.searchBack {
		return "?" + e.searchLine
	}
	return "/" + e.searchLine
}

func (e *vimNoteEditor) searchInfoHint() string {
	s := e.lastSearch
	if s.pattern == "" {
		return ""
	}
	n := len(s.matches)
	if n == 0 {
		return "no matches for " + s.pattern
	}
	idx := s.matchIdx + 1
	if idx <= 0 {
		idx = 1
	}
	return s.pattern + "  ·  " + itoa(idx) + "/" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
