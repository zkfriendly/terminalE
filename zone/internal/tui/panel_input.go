package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type panelInputAction int

const (
	panelInputNone panelInputAction = iota
	panelInputSubmit
	panelInputCancel
	panelInputTab
)

// panelInput wraps bubbles textinput with vim-style insert mode: while focused, all
// keys are consumed by the field. esc blurs back to normal mode; enter submits.
type panelInput struct {
	input     textinput.Model
	focused   bool
	blurOnTab bool
}

func newPanelInput(prompt, placeholder string, charLimit int) panelInput {
	return newPanelInputOpts(prompt, placeholder, charLimit, false)
}

func newPanelInputTabBlur(prompt, placeholder string, charLimit int) panelInput {
	return newPanelInputOpts(prompt, placeholder, charLimit, true)
}

func newPanelInputOpts(prompt, placeholder string, charLimit int, blurOnTab bool) panelInput {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.Placeholder = placeholder
	if charLimit > 0 {
		ti.CharLimit = charLimit
	}
	return panelInput{input: ti, blurOnTab: blurOnTab}
}

func (p panelInput) Value() string   { return p.input.Value() }
func (p panelInput) View() string    { return p.input.View() }
func (p panelInput) IsFocused() bool { return p.focused }

func (p *panelInput) SetValue(v string) { p.input.SetValue(v) }

func (p *panelInput) SetPlaceholder(v string) { p.input.Placeholder = v }

func (p *panelInput) SetPrompt(v string) { p.input.Prompt = v }

func (p *panelInput) Reset() {
	p.input.Reset()
	p.focused = false
}

func (p *panelInput) focusCmd() tea.Cmd {
	p.focused = true
	return p.input.Focus()
}

// markFocused activates input routing without a tea program (tests only).
func (p *panelInput) markFocused() {
	p.focused = true
}

func (p *panelInput) blur() {
	p.focused = false
	p.input.Blur()
}

// handleMsg routes msg to the field while focused. All keys are consumed; nothing
// propagates to list navigation or other shortcuts until esc blurs the field.
func (p *panelInput) handleMsg(msg tea.Msg) (tea.Cmd, panelInputAction) {
	if !p.focused {
		return nil, panelInputNone
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			p.blur()
			return nil, panelInputCancel
		case "enter":
			return nil, panelInputSubmit
		case "tab":
			if p.blurOnTab {
				p.blur()
				return nil, panelInputTab
			}
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd, panelInputNone
}
