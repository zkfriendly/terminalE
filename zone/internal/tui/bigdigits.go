package tui

import "strings"

// 5-row block glyphs for the focus timer. Each glyph is 3 cells wide; rendering
// doubles cells horizontally for a chunkier look.
var bigGlyphs = map[rune][]string{
	'0': {"███", "█ █", "█ █", "█ █", "███"},
	'1': {"  █", "  █", "  █", "  █", "  █"},
	'2': {"███", "  █", "███", "█  ", "███"},
	'3': {"███", "  █", "███", "  █", "███"},
	'4': {"█ █", "█ █", "███", "  █", "  █"},
	'5': {"███", "█  ", "███", "  █", "███"},
	'6': {"███", "█  ", "███", "█ █", "███"},
	'7': {"███", "  █", "  █", "  █", "  █"},
	'8': {"███", "█ █", "███", "█ █", "███"},
	'9': {"███", "█ █", "███", "  █", "███"},
	':': {"   ", " █ ", "   ", " █ ", "   "},
}

// bigText renders s (digits and colons) as 5-line block art.
func bigText(s string) string {
	rows := make([]string, 5)
	for i, ch := range s {
		glyph, ok := bigGlyphs[ch]
		if !ok {
			glyph = bigGlyphs[':']
		}
		for r := 0; r < 5; r++ {
			// Double each cell horizontally.
			wide := strings.Builder{}
			for _, c := range glyph[r] {
				wide.WriteRune(c)
				wide.WriteRune(c)
			}
			rows[r] += wide.String()
			if i < len(s)-1 {
				rows[r] += "  " // gap between glyphs
			}
		}
	}
	return strings.Join(rows, "\n")
}
