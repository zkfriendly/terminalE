package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/llm"
)

func renderLLMStatus(s Styles, cfg config.Config) string {
	label := s.HelpKey.Render("llm")
	if !cfg.LLMEnabled {
		return label + " " + s.Dim.Render("off")
	}

	snap := llm.Snapshot()
	parts := []string{label, s.Dim.Render(llm.StatusLabel(cfg))}

	if snap.InFlight > 0 {
		parts = append(parts, s.Accent.Render(fmt.Sprintf("%d working", snap.InFlight)))
	}
	if snap.OK > 0 {
		parts = append(parts, s.Work.Render(fmt.Sprintf("%d ok", snap.OK)))
	}
	if snap.Err > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(colRed).Render(fmt.Sprintf("%d err", snap.Err)))
	}
	if snap.LastMS > 0 {
		parts = append(parts, s.Dim.Render(formatLLMDuration(snap.LastMS)))
	}

	return joinLLMParts(s, parts)
}

func joinLLMParts(s Styles, parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += s.Dim.Render(" · ") + p
	}
	return out
}

func formatLLMDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

func wrapFooterWithLLM(s Styles, cfg config.Config, entries []string, sep string, width int) string {
	all := append([]string{renderLLMStatus(s, cfg)}, entries...)
	return wrapHints(all, sep, width)
}
