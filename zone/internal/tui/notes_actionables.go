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

type noteActionablesExtractedMsg struct {
	noteID int64
	tasks  []string
	err    error
}

func noteActionablesTitle(n store.SessionNote) string {
	if n.Title != "" {
		return n.Emoji + " " + n.Title
	}
	body := strings.TrimSpace(strings.ReplaceAll(n.Body, "\n", " "))
	return truncate(body, 48)
}

func extractNoteActionablesCmd(cfg *config.Config, noteID int64, body string) tea.Cmd {
	if err := noteLabelStatusErr(cfg); err != nil {
		return func() tea.Msg {
			return noteActionablesExtractedMsg{noteID: noteID, err: err}
		}
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	baseURL := cfg.LMStudioURL
	model := cfg.LMStudioModel
	return func() tea.Msg {
		tasks, err := llm.ExtractActionables(baseURL, model, body)
		if err != nil {
			return noteActionablesExtractedMsg{noteID: noteID, err: err}
		}
		return noteActionablesExtractedMsg{noteID: noteID, tasks: tasks}
	}
}

func onNoteActionablesExtracted(
	store *store.Store,
	msg noteActionablesExtractedMsg,
	noteID *int64,
	items *[]string,
	extracting *bool,
	errMsg *string,
) {
	*extracting = false
	if msg.noteID != *noteID {
		return
	}
	if msg.err != nil {
		*errMsg = msg.err.Error()
		return
	}
	*errMsg = ""
	*items = msg.tasks
	if store != nil {
		_ = store.SaveSessionNoteActionables(msg.noteID, msg.tasks)
	}
}

func noteActionablesActionHints(s Styles) []string {
	return []string{
		s.helpEntry("r", "re-extract"),
		s.helpEntry("esc", "back"),
	}
}

func renderNoteActionablesPanel(title string, tasks []string, extracting bool, errMsg string, width, height int, s Styles) string {
	panelW := width - 8
	if panelW < 30 {
		panelW = 30
	}
	panelH := height - 6
	if panelH < 10 {
		panelH = 10
	}
	innerW := panelW - 4
	if innerW < 20 {
		innerW = 20
	}

	accent := lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	var lines []string
	lines = append(lines, accent.Render("extracted tasks"))
	lines = append(lines, s.Dim.Render("  ·  "+title), "")

	switch {
	case extracting:
		lines = append(lines, s.Dim.Render("extracting…"))
	case errMsg != "":
		lines = append(lines, lipgloss.NewStyle().Foreground(colRed).Render(truncate(errMsg, innerW)))
	case len(tasks) == 0:
		lines = append(lines, s.Dim.Render("no clear tasks found"))
	default:
		for i, task := range tasks {
			num := s.Dim.Render(fmt.Sprintf("%2d.", i+1))
			lines = append(lines, num+" "+truncate(task, innerW-4))
		}
	}

	box := lipgloss.NewStyle().
		Width(panelW).
		Height(panelH).
		Padding(0, 2).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
