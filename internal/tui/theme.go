package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"

	"github.com/guilhermesalviano/korchestrate/internal/agent"
)

// Palette. Every color adapts to light and dark terminals.
var (
	cGold   = lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#F5C542"}
	cViolet = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#A78BFA"}
	cCyan   = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#38BDF8"}
	cGreen  = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	cRed    = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	cAmber  = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	cText   = lipgloss.AdaptiveColor{Light: "#1F2937", Dark: "#E5E7EB"}
	cMuted  = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#7C8190"}
	cFaint  = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#3A3F4B"}
	cInk    = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#111318"}
)

// Gradient endpoints for the brand mark (gold → violet).
const gradFrom, gradTo = "#F5C542", "#A78BFA"

var (
	textStyle   = lipgloss.NewStyle().Foreground(cText)
	boldStyle   = lipgloss.NewStyle().Foreground(cText).Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(cMuted)
	faintStyle  = lipgloss.NewStyle().Foreground(cFaint)
	goldStyle   = lipgloss.NewStyle().Foreground(cGold)
	violetStyle = lipgloss.NewStyle().Foreground(cViolet)
	cyanStyle   = lipgloss.NewStyle().Foreground(cCyan)
	greenStyle  = lipgloss.NewStyle().Foreground(cGreen)
	redStyle    = lipgloss.NewStyle().Foreground(cRed)
	amberStyle  = lipgloss.NewStyle().Foreground(cAmber)

	headingStyle = lipgloss.NewStyle().Foreground(cViolet).Bold(true)
	tabActive    = lipgloss.NewStyle().Foreground(cGold).Bold(true).Underline(true)
	tabInactive  = lipgloss.NewStyle().Foreground(cMuted)
)

// Role accents and glyphs for the three pipeline stages.
var (
	roleColor = map[agent.Kind]lipgloss.AdaptiveColor{
		agent.Planner:  cViolet,
		agent.Executor: cCyan,
		agent.Reviewer: cGold,
	}
	roleGlyph = map[agent.Kind]string{
		agent.Planner:  "◈",
		agent.Executor: "▣",
		agent.Reviewer: "◉",
	}
	roleTag = map[agent.Kind]string{
		agent.Planner:  "PLAN",
		agent.Executor: "EXEC",
		agent.Reviewer: "REVW",
	}
)

var spin = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// gradient paints s one rune at a time between two hex colors.
func gradient(s, from, to string, bold bool) string {
	a, errA := colorful.Hex(from)
	b, errB := colorful.Hex(to)
	runes := []rune(s)
	if errA != nil || errB != nil || len(runes) == 0 {
		return s
	}
	var out strings.Builder
	for i, r := range runes {
		t := 0.0
		if len(runes) > 1 {
			t = float64(i) / float64(len(runes)-1)
		}
		st := lipgloss.NewStyle().Foreground(lipgloss.Color(a.BlendLuv(b, t).Clamped().Hex())).Bold(bold)
		out.WriteString(st.Render(string(r)))
	}
	return out.String()
}

// chip renders a key hint such as " A  approve".
func chip(key, label string, bg lipgloss.AdaptiveColor) string {
	k := lipgloss.NewStyle().Background(bg).Foreground(cInk).Bold(true).Padding(0, 1).Render(key)
	return k + " " + textStyle.Render(label)
}

// keyHint renders a footer shortcut such as "n new".
func keyHint(key, label string) string {
	return goldStyle.Bold(true).Render(key) + " " + mutedStyle.Render(label)
}
