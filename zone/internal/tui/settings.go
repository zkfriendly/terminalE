package tui

import (
	"fmt"
	"strconv"
	"strings"

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
}

// settingsView edits config.json from the terminal.
type settingsView struct {
	cfg        *config.Config
	styles     Styles
	configPath string

	width, height int
	cursor        int
	offset        int
	prompt        panelInput
	err           string

	fields []settingField
}

func newSettings(cfg *config.Config, s Styles, configPath string) *settingsView {
	return &settingsView{
		cfg:        cfg,
		styles:     s,
		configPath: configPath,
		prompt:     newPanelInput("  ", "", 200),
		fields:     settingsFields(),
	}
}

func settingsFields() []settingField {
	return []settingField{
		{section: "Session", label: "Prepare (min)", kind: settingInt},
		{label: "Work (min)", kind: settingInt},
		{label: "Break (min)", kind: settingInt},
		{label: "Total session (min)", kind: settingInt},
		{section: "Notes", label: "Local LLM labeling", kind: settingBool},
		{label: "Local LLM URL", kind: settingString},
		{label: "Local LLM model", kind: settingString},
	}
}

func (v *settingsView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if v.prompt.IsFocused() {
			return v.updateInput(msg)
		}
		return v.updateNormal(msg)
	case tea.PasteMsg:
		if v.prompt.IsFocused() {
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
	cmd, act := v.prompt.handleMsg(msg)
	switch act {
	case panelInputCancel:
		v.prompt.Reset()
		return cmd
	case panelInputSubmit:
		val := strings.TrimSpace(v.prompt.Value())
		v.prompt.Reset()
		if val == "" {
			return cmd
		}
		if err := v.applyInput(val); err != nil {
			v.err = err.Error()
		} else {
			v.save()
		}
		return cmd
	}
	return cmd
}

func (v *settingsView) startInput() tea.Cmd {
	f := v.currentField()
	if f == nil {
		return nil
	}
	v.prompt.SetPlaceholder(f.label)
	v.prompt.SetValue(v.fieldValue(*f))
	return v.prompt.focusCmd()
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
	case "Local LLM labeling":
		return boolLabel(v.cfg.LMStudioEnabled)
	case "Local LLM URL":
		return v.cfg.LMStudioURL
	case "Local LLM model":
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
	case "Local LLM labeling":
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
	case "Local LLM URL":
		v.cfg.LMStudioURL = val
	case "Local LLM model":
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

	if v.prompt.IsFocused() {
		rows = append(rows, "", v.prompt.View())
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
	if v.prompt.IsFocused() {
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
	return v.prompt.IsFocused()
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
