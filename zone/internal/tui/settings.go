package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/config"
)

type settingKind int

const (
	settingInt settingKind = iota
	settingBool
	settingString
)

type settingField struct {
	section string
	label   string
	kind    settingKind
	choices []string
}

// settingsView edits config.json from the terminal.
type settingsView struct {
	cfg        *config.Config
	styles     Styles
	configPath string

	width, height int
	cursor        int
	offset        int
	editing       bool
	input         textinput.Model
	err           string

	fields []settingField
}

func newSettings(cfg *config.Config, s Styles, configPath string) *settingsView {
	ti := textinput.New()
	ti.Prompt = "  "
	ti.CharLimit = 200
	return &settingsView{
		cfg:        cfg,
		styles:     s,
		configPath: configPath,
		input:      ti,
		fields:     settingsFields(),
	}
}

func settingsFields() []settingField {
	return []settingField{
		{section: "Session", label: "Prepare (min)", kind: settingInt},
		{label: "Work (min)", kind: settingInt},
		{label: "Break (min)", kind: settingInt},
		{label: "Total session (min)", kind: settingInt},
		{section: "Notes", label: "LM Studio labeling", kind: settingBool},
		{label: "LM Studio URL", kind: settingString},
		{label: "LM Studio model", kind: settingString},
	}
}

func (v *settingsView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if v.editing {
			return v.updateInput(msg)
		}
		return v.updateNormal(msg)
	case tea.PasteMsg:
		if v.editing {
			return v.updateInput(msg)
		}
	}
	return nil
}

func (v *settingsView) updateNormal(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "tab":
		return shellToggleFocusCmd()
	case "up", "k":
		v.cursor--
	case "down", "j":
		v.cursor++
	case "pgup":
		v.cursor -= v.pageSize()
	case "pgdown":
		v.cursor += v.pageSize()
	case "g", "home":
		v.cursor = 0
	case "G", "end":
		v.cursor = len(v.fields) - 1
	case " ", "space":
		v.toggleOrCycle()
	case "enter":
		if f := v.currentField(); f != nil {
			switch f.kind {
			case settingInt, settingString:
				return v.startInput()
			case settingBool:
				v.toggleOrCycle()
			}
		}
	}
	v.clampCursor()
	v.ensureCursorVisible()
	return nil
}

func (v *settingsView) updateInput(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
	case "esc":
		v.editing = false
		v.input.Blur()
		v.input.Reset()
		return nil
	case "enter":
		val := strings.TrimSpace(v.input.Value())
		v.editing = false
		v.input.Blur()
		v.input.Reset()
		if val == "" {
			return nil
		}
		if err := v.applyInput(val); err != nil {
			v.err = err.Error()
		} else {
			v.save()
		}
		return nil
		}
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return cmd
}

func (v *settingsView) startInput() tea.Cmd {
	f := v.currentField()
	if f == nil {
		return nil
	}
	v.editing = true
	v.input.Placeholder = f.label
	v.input.SetValue(v.fieldValue(*f))
	return v.input.Focus()
}

func (v *settingsView) currentField() *settingField {
	if v.cursor < 0 || v.cursor >= len(v.fields) {
		return nil
	}
	return &v.fields[v.cursor]
}

func (v *settingsView) fieldValue(f settingField) string {
	switch f.label {
	case "Prepare (min)":
		return strconv.Itoa(v.cfg.PrepareMinutes)
	case "Work (min)":
		return strconv.Itoa(v.cfg.WorkMinutes)
	case "Break (min)":
		return strconv.Itoa(v.cfg.BreakMinutes)
	case "Total session (min)":
		return strconv.Itoa(v.cfg.TotalMinutes)
	case "LM Studio labeling":
		return boolLabel(v.cfg.LMStudioEnabled)
	case "LM Studio URL":
		return v.cfg.LMStudioURL
	case "LM Studio model":
		return v.cfg.LMStudioModel
	default:
		return ""
	}
}

func boolLabel(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func (v *settingsView) toggleOrCycle() {
	f := v.currentField()
	if f == nil {
		return
	}
	switch f.label {
	case "LM Studio labeling":
		v.cfg.LMStudioEnabled = !v.cfg.LMStudioEnabled
	default:
		return
	}
	v.save()
}

func (v *settingsView) applyInput(val string) error {
	f := v.currentField()
	if f == nil {
		return nil
	}
	switch f.label {
	case "Prepare (min)":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("prepare minutes must be >= 0")
		}
		v.cfg.PrepareMinutes = n
	case "Work (min)":
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			return fmt.Errorf("work minutes must be > 0")
		}
		v.cfg.WorkMinutes = n
	case "Break (min)":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("break minutes must be >= 0")
		}
		v.cfg.BreakMinutes = n
	case "Total session (min)":
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			return fmt.Errorf("total minutes must be > 0")
		}
		v.cfg.TotalMinutes = n
	case "LM Studio URL":
		v.cfg.LMStudioURL = val
	case "LM Studio model":
		v.cfg.LMStudioModel = val
	}
	return nil
}

func (v *settingsView) save() {
	if err := v.cfg.Save(); err != nil {
		v.err = err.Error()
	} else {
		v.err = ""
	}
}

func (v *settingsView) pageSize() int {
	n := v.height - 8
	if n < 1 {
		n = 1
	}
	return n
}

func (v *settingsView) clampCursor() {
	if len(v.fields) == 0 {
		v.cursor = 0
		return
	}
	v.cursor = clampInt(v.cursor, 0, len(v.fields)-1)
}

func (v *settingsView) ensureCursorVisible() {
	page := v.pageSize()
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if v.cursor >= v.offset+page {
		v.offset = v.cursor - page + 1
	}
	maxOff := len(v.fields) - page
	if maxOff < 0 {
		maxOff = 0
	}
	v.offset = clampInt(v.offset, 0, maxOff)
}

func (v *settingsView) renderBody(width, height int) string {
	v.width, v.height = width, height
	if width == 0 {
		return "loading settings..."
	}
	s := v.styles

	page := v.pageSize()
	end := v.offset + page
	if end > len(v.fields) {
		end = len(v.fields)
	}

	var rows []string
	lastSection := ""
	for i := v.offset; i < end; i++ {
		f := v.fields[i]
		if f.section != "" && f.section != lastSection {
			if len(rows) > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, s.PaneTitle.Render(f.section))
			lastSection = f.section
		}
		rows = append(rows, v.renderRow(i, f))
	}

	if v.editing {
		rows = append(rows, "", v.input.View())
	}

	body := strings.Join(rows, "\n")
	if body == "" {
		body = s.Dim.Render("no settings")
	}

	out := body
	if v.err != "" {
		out = lipgloss.NewStyle().Foreground(colRed).Render("error: "+v.err) + "\n" + out
	}
	return out
}

func (v *settingsView) actionHints() []string {
	if v.editing {
		return []string{v.styles.helpEntry("enter", "save"), v.styles.helpEntry("esc", "cancel")}
	}
	return []string{
		v.styles.helpEntry("↑↓", "move"),
		v.styles.helpEntry("enter", "edit"),
		v.styles.helpEntry("space", "toggle"),
	}
}

func (v *settingsView) infoHints() []string {
	return []string{v.styles.Dim.Render(truncate(v.configPath, 60))}
}

func (v *settingsView) escIsLocal() bool {
	return v.editing
}

func (v *settingsView) render(width, height int) string {
	return v.renderBody(width, height)
}

func (v *settingsView) renderRow(i int, f settingField) string {
	s := v.styles
	val := v.fieldValue(f)
	label := padRight(f.label, 22)
	line := label + "  " + s.StatValue.Render(val)
	if i == v.cursor {
		return s.ItemSel.Render(" " + line + " ")
	}
	return s.Item.Render("  " + line)
}
