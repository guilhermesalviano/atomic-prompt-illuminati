package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/guibs/atomic-prompt-illuminati/internal/agent"
	"github.com/guibs/atomic-prompt-illuminati/internal/ui"
)

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	listTitle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	promptStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	gateStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	failStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	runStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	boxStyle      = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Align(lipgloss.Center)
	boxActive = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Align(lipgloss.Center)
	boxDone = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42")).
		Align(lipgloss.Center)
	boxFail = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Align(lipgloss.Center)
)

var spin = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"}

const arrow = " ──▶ "

// View implements tea.Model.
func (a *App) View() string {
	w, h := a.width, a.height
	if w < 60 {
		w = 60
	}
	if h < 12 {
		h = 12
	}
	leftW := 32
	rightW := w - leftW - 1
	if rightW < 34 {
		rightW = 34
	}
	bodyH := h - 3

	left := a.renderList(leftW, bodyH)
	right := a.renderRight(rightW, bodyH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	return body + "\n" + a.renderInput(w)
}

func (a *App) renderList(w, h int) string {
	lines := []string{listTitle.Render("WORKTREES")}
	if len(a.entries) == 0 {
		lines = append(lines, dimStyle.Render("(none yet)"), "",
			dimStyle.Render("type a prompt below"), dimStyle.Render("and press enter"))
		return clipLines(strings.Join(lines, "\n"), w, h)
	}
	for i, e := range a.entries {
		glyph, _ := e.stateLabel()
		name := truncate(e.title(), w-5)
		line := glyph + " " + name
		if i == a.cursor {
			lines = append(lines, selectedStyle.Render("▸ "+line))
		} else {
			lines = append(lines, "  "+line)
		}
	}
	return clipLines(strings.Join(lines, "\n"), w, h)
}

func (a *App) renderRight(w, h int) string {
	e := a.current()
	if e == nil {
		return clipLines(dimStyle.Render("Select a worktree or start a new run."), w, h)
	}
	state := ""
	if e.State != "" {
		state = dimStyle.Render("  " + string(e.State))
	}
	header := headerStyle.Render(truncate(e.title(), w-14)) + state

	flow := a.renderFlow(e, w)
	lines := []string{header, "", flow, ""}

	if e.ErrText != "" {
		lines = append(lines, failStyle.Render(truncate("error: "+e.ErrText, w)), "")
	}

	used := 0
	for _, l := range lines {
		used += len(strings.Split(l, "\n"))
	}
	detailH := h - used
	if detailH < 3 {
		detailH = 3
	}
	lines = append(lines, a.renderDetail(e, w, detailH))
	return clipLines(strings.Join(lines, "\n"), w, h)
}

func (a *App) modelsFor() map[agent.Kind]string {
	return map[agent.Kind]string{
		agent.Planner:  a.cfg.Models.Planner.Model,
		agent.Executor: a.cfg.Models.Executor.Model,
		agent.Reviewer: a.cfg.Models.Reviewer.Model,
	}
}

func (a *App) renderFlow(e *Entry, w int) string {
	kinds := []agent.Kind{agent.Planner, agent.Executor, agent.Reviewer}
	models := a.modelsFor()
	boxW := (w - 2*lipgloss.Width(arrow) - 6) / 3
	if boxW < 12 {
		return a.renderFlowVertical(e, kinds, models, w)
	}
	boxes := make([]string, 0, 5)
	for i, k := range kinds {
		if i > 0 {
			boxes = append(boxes, arrow)
		}
		boxes = append(boxes, stageBox(k, models[k], e.Stages[k], a.frame, boxW))
	}
	flow := lipgloss.JoinHorizontal(lipgloss.Center, boxes...)
	if e.Iter > 0 {
		loop := "↺ fix loop · iteration " + strconv.Itoa(e.Iter)
		flow += "\n" + dimStyle.Render(center(loop, lipgloss.Width(flow)))
	}
	return flow
}

func (a *App) renderFlowVertical(e *Entry, kinds []agent.Kind, models map[agent.Kind]string, w int) string {
	var rows []string
	for i, k := range kinds {
		glyph, state := stageStatus(e.Stages[k], a.frame)
		label := "[" + strings.ToUpper(string(k)) + "]"
		rows = append(rows, fmt.Sprintf("%-10s %-16s %s %s",
			label, truncate(models[k], 16), glyph, truncate(state, w-32)))
		if i < len(kinds)-1 {
			rows = append(rows, "    │", "    ▼")
		}
	}
	return strings.Join(rows, "\n")
}

func stageBox(k agent.Kind, model string, si *StageInfo, frame, w int) string {
	glyph, state := stageStatus(si, frame)
	content := fmt.Sprintf("%s\n%s\n%s %s",
		strings.ToUpper(string(k)), truncate(model, w), glyph, truncate(state, w-3))
	style := boxStyle
	switch {
	case si != nil && si.failed:
		style = boxFail
	case si != nil && si.done:
		style = boxDone
	case si != nil && si.status != "":
		style = boxActive
	}
	return style.Width(w).Render(content)
}

func stageStatus(si *StageInfo, frame int) (string, string) {
	if si == nil {
		return "·", "pending"
	}
	switch {
	case si.failed:
		return "✗", oneWord(si.status)
	case si.done:
		return "✓", "done"
	case si.status != "":
		return spin[frame%len(spin)], oneWord(si.status)
	default:
		return "·", "pending"
	}
}

func oneWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "pending"
	}
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

func (a *App) renderDetail(e *Entry, w, h int) string {
	if e.Gate != nil {
		var banner, body string
		switch e.Gate.kind {
		case gatePlan:
			banner = gateStyle.Render("[a]pprove   [r]eject") + "\n"
			body = ui.RenderPlan(e.Gate.plan)
		case gateReview:
			body = ui.RenderReview(e.Gate.review)
			if e.Gate.review != nil && e.Gate.review.Pass() {
				banner = gateStyle.Render("[a]pprove   [f]ix   [r]eject") + "\n"
			} else {
				banner = gateStyle.Render("[f]ix   [r]eject   [a]ccept anyway") + "\n"
			}
		}
		return banner + clipLines(body, w, h-2)
	}
	logs := a.logs[e]
	if len(logs) == 0 {
		if e.Review != nil {
			return clipLines(ui.RenderReview(e.Review), w, h)
		}
		if e.Plan != nil {
			return clipLines(ui.RenderPlan(e.Plan), w, h)
		}
		return dimStyle.Render("no output yet")
	}
	start := len(logs) - h
	if start < 0 {
		start = 0
	}
	return clipLines(strings.Join(logs[start:], "\n"), w, h)
}

func (a *App) renderInput(w int) string {
	var val string
	if a.inputFocus {
		val = string(a.input) + "▏"
	} else {
		val = dimStyle.Render(string(a.input))
	}
	line := promptStyle.Render("> ") + val
	hint := "n new prompt   ↑/↓ select   a/f/r gate   q quit"
	if a.inputFocus {
		hint = "enter run   esc cancel"
	}
	return truncate(line, w) + "\n" + dimStyle.Render(truncate(hint, w))
}

// clipLines truncates every line to width w and keeps at most h lines.
func clipLines(s string, w, h int) string {
	raw := strings.Split(s, "\n")
	if len(raw) > h {
		raw = raw[:h]
	}
	for i, l := range raw {
		raw[i] = truncate(l, w)
	}
	return strings.Join(raw, "\n")
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}

func center(s string, w int) string {
	sw := lipgloss.Width(s)
	if w <= sw {
		return s
	}
	pad := (w - sw) / 2
	return strings.Repeat(" ", pad) + s
}
