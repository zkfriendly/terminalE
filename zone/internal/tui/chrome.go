package tui

import (
	"charm.land/lipgloss/v2"
)

const chromeSep = "  │  "

// renderActionBar renders keyboard shortcuts across the top of the screen.
func renderActionBar(s Styles, entries []string, width int) string {
	if len(entries) == 0 {
		return ""
	}
	content := wrapHints(entries, s.Dim.Render(chromeSep), width)
	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(colAccent).
		Render(content)
}

// renderInfoBar renders status and contextual data at the bottom of the screen.
func renderInfoBar(s Styles, entries []string, width int) string {
	var filtered []string
	for _, e := range entries {
		if e != "" {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	content := wrapHints(filtered, s.Dim.Render("  ·  "), width)
	return lipgloss.NewStyle().
		Width(width).
		BorderTop(true).
		BorderForeground(colAccent).
		Background(lipgloss.Color("#24283b")).
		Padding(0, 1).
		Render(content)
}

// composeShellLayout stacks action bar, page nav, body, and bottom info bar.
func composeShellLayout(actionBar, navStrip, body, bottomBar string, width, height int) string {
	topH := lipgloss.Height(actionBar)
	navH := lipgloss.Height(navStrip)
	bottomH := lipgloss.Height(bottomBar)
	bodyH := height - topH - navH - bottomH
	if bodyH < 1 {
		bodyH = 1
	}
	bodyBox := lipgloss.NewStyle().
		Width(width).
		Height(bodyH).
		Render(body)

	var parts []string
	if actionBar != "" {
		parts = append(parts, actionBar)
	}
	if navStrip != "" {
		parts = append(parts, navStrip)
	}
	parts = append(parts, bodyBox)
	if bottomBar != "" {
		parts = append(parts, bottomBar)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// composeChrome stacks top action bar, body, and bottom info bar vertically.
func composeChrome(topBar, body, bottomBar string, width, height int) string {
	topH := lipgloss.Height(topBar)
	bottomH := lipgloss.Height(bottomBar)
	bodyH := height - topH - bottomH
	if bodyH < 1 {
		bodyH = 1
	}
	bodyBox := lipgloss.NewStyle().
		Width(width).
		Height(bodyH).
		Render(body)

	var parts []string
	if topBar != "" {
		parts = append(parts, topBar)
	}
	parts = append(parts, bodyBox)
	if bottomBar != "" {
		parts = append(parts, bottomBar)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
