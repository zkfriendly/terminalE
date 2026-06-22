package tui

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/clipperhouse/displaywidth"
	"github.com/rivo/uniseg"
)

var noteSelectionStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#33467c")).
	Foreground(colFG).
	Inline(true)

func (e *vimNoteEditor) View() string {
	if e.mode != noteModeVisual {
		return e.ta.View()
	}
	return e.renderVisualView()
}

func (e *vimNoteEditor) renderVisualView() string {
	styles := e.ta.Styles().Focused
	all := strings.Split(e.buildSelectionView(), "\n")
	if len(all) > 0 && all[len(all)-1] == "" {
		all = all[:len(all)-1]
	}
	top := e.ta.ScrollYOffset()
	height := e.ta.Height()
	if top > len(all) {
		top = len(all)
	}
	end := top + height
	if end > len(all) {
		end = len(all)
	}
	visible := all[top:end]
	for len(visible) < height {
		visible = append(visible, styles.EndOfBuffer.Render(" "))
	}
	return styles.Base.Render(strings.Join(visible, "\n"))
}

func (e *vimNoteEditor) buildSelectionView() string {
	from, to := e.selectionEndpoints()
	lines := noteLines(e.ta.Value())
	width := e.ta.Width()
	styles := e.ta.Styles().Focused
	curLine := e.ta.Line()
	curCol := e.ta.Column()

	textStyle := styles.Text.Inherit(styles.Base).Inline(true)
	cursorLineStyle := styles.CursorLine.Inherit(styles.Base).Inline(true)
	promptStyle := styles.Prompt.Inherit(styles.Base).Inline(true)

	var s strings.Builder
	for l, text := range lines {
		lineRunes := []rune(text)
		wrapped := noteSoftWrap(lineRunes, width)
		rowStyle := textStyle
		if l == curLine {
			rowStyle = cursorLineStyle
		}

		var counter int
		for _, wrappedLine := range wrapped {
			s.WriteString(promptStyle.Render(e.ta.Prompt))

			var rendered strings.Builder
			for i, r := range wrappedLine {
				logCol := counter + i
				ch := string(r)
				selected := posSelected(l, logCol, from, to)
				onCursor := l == curLine && logCol == curCol

				switch {
				case onCursor:
					rendered.WriteString(cursorLineStyle.Render(ch))
				case selected:
					rendered.WriteString(noteSelectionStyle.Render(ch))
				default:
					rendered.WriteString(rowStyle.Render(ch))
				}
			}

			strwidth := uniseg.StringWidth(rendered.String())
			padding := width - strwidth
			if padding > 0 {
				rendered.WriteString(rowStyle.Render(strings.Repeat(" ", padding)))
			}

			s.WriteString(rendered.String())
			s.WriteByte('\n')
			counter += len(wrappedLine)
		}
	}

	for range e.ta.Height() {
		s.WriteString(promptStyle.Render(e.ta.Prompt))
		s.WriteString(styles.EndOfBuffer.Inherit(styles.Base).Inline(true).Render(" "))
		s.WriteByte('\n')
	}
	return s.String()
}

func noteSoftWrap(runes []rune, width int) [][]rune {
	if width <= 0 {
		return [][]rune{runes}
	}
	var (
		lines  = [][]rune{{}}
		word   = []rune{}
		row    int
		spaces int
	)

	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}

		if spaces > 0 {
			if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces > width {
				row++
				lines = append(lines, []rune{})
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], noteRepeatSpaces(spaces)...)
				spaces = 0
				word = nil
			} else {
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], noteRepeatSpaces(spaces)...)
				spaces = 0
				word = nil
			}
		} else if len(word) > 0 {
			lastCharLen := displaywidth.Rune(word[len(word)-1])
			if uniseg.StringWidth(string(word))+lastCharLen > width {
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []rune{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
		}
	}

	if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces >= width {
		lines = append(lines, []rune{})
		lines[row+1] = append(lines[row+1], word...)
		spaces++
		lines[row+1] = append(lines[row+1], noteRepeatSpaces(spaces)...)
	} else {
		lines[row] = append(lines[row], word...)
		spaces++
		lines[row] = append(lines[row], noteRepeatSpaces(spaces)...)
	}

	return lines
}

func noteRepeatSpaces(n int) []rune {
	if n <= 0 {
		return nil
	}
	return []rune(strings.Repeat(" ", n))
}
