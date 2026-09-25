package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/guibs/atomic-prompt-illuminati/internal/contracts"
)

// renderActivity lays out the activity feed: time, stage tag, message.
func renderActivity(logs []logLine, w int) []string {
	const gutter = 14 // "15:04:05 PLAN "
	var out []string
	for _, l := range logs {
		ts := "--:--:--"
		if !l.at.IsZero() {
			ts = l.at.Format("15:04:05")
		}
		tag := mutedStyle.Render("····")
		if t, ok := roleTag[l.kind]; ok {
			tag = lipgloss.NewStyle().Foreground(roleColor[l.kind]).Bold(true).Render(t)
		}
		var st lipgloss.Style
		switch l.level {
		case levelStage:
			st = boldStyle
		case levelDetail:
			st = mutedStyle
		case levelError:
			st = redStyle
		default:
			st = textStyle
		}
		body := wrap(l.text, w-gutter)
		for i, b := range body {
			prefix := strings.Repeat(" ", gutter)
			if i == 0 {
				prefix = faintStyle.Render(ts) + " " + tag + " "
			}
			out = append(out, prefix+st.Render(b))
		}
	}
	return out
}

// renderPlan formats a plan with headings, numbered steps and file badges.
func renderPlan(p *contracts.Plan, w int) []string {
	if p == nil {
		return emptyNote("No plan yet — the planner will fill this in.", w)
	}
	var out []string
	section := func(title string) {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, headingStyle.Render(title))
	}
	section("SUMMARY")
	out = append(out, styleAll(wrap(p.Summary, w), textStyle)...)

	if len(p.Assumptions) > 0 {
		section("ASSUMPTIONS")
		for _, a := range p.Assumptions {
			out = append(out, bullet("•", a, w, mutedStyle, textStyle)...)
		}
	}
	if len(p.Files) > 0 {
		section("FILES")
		out = append(out, styleAll(wrap(strings.Join(p.Files, "  "), w), cyanStyle)...)
	}
	section(fmt.Sprintf("STEPS  %s", mutedStyle.Render(fmt.Sprintf("(%d)", len(p.Steps)))))
	for i, s := range p.Steps {
		num := goldStyle.Bold(true).Render(fmt.Sprintf("%2d", i+1))
		body := wrap(s.Description, w-4)
		for j, b := range body {
			if j == 0 {
				out = append(out, num+"  "+textStyle.Render(b))
			} else {
				out = append(out, "    "+textStyle.Render(b))
			}
		}
		if len(s.Files) > 0 {
			out = append(out, bullet("    ▸", strings.Join(s.Files, ", "), w, faintStyle, cyanStyle)...)
		}
		if s.Verification != "" {
			out = append(out, bullet("    ✓", s.Verification, w, greenStyle, mutedStyle)...)
		}
	}
	section("ACCEPTANCE CRITERIA")
	for _, c := range p.AcceptanceCriteria {
		out = append(out, bullet("☐", c, w, goldStyle, textStyle)...)
	}
	if len(p.OutOfScope) > 0 {
		section("OUT OF SCOPE")
		for _, o := range p.OutOfScope {
			out = append(out, bullet("–", o, w, mutedStyle, mutedStyle)...)
		}
	}
	return out
}

// renderReview formats a review with a verdict badge and severity colors.
func renderReview(r *contracts.Review, w int) []string {
	if r == nil {
		return emptyNote("No review yet — the reviewer runs after the executor.", w)
	}
	bg, word := cRed, " FAIL "
	if r.Pass() {
		bg, word = cGreen, " PASS "
	}
	badge := lipgloss.NewStyle().Background(bg).Foreground(cInk).Bold(true).Render(word)
	out := []string{badge + "  " + mutedStyle.Render(fmt.Sprintf("%d issue(s) · %d/%d criteria met",
		len(r.Issues), metCount(r), len(r.Acceptance)))}
	out = append(out, "")
	out = append(out, styleAll(wrap(r.Summary, w), textStyle)...)

	if len(r.Acceptance) > 0 {
		out = append(out, "", headingStyle.Render("ACCEPTANCE"))
		for _, a := range r.Acceptance {
			mark, ms := "✘", redStyle
			if a.Met {
				mark, ms = "✔", greenStyle
			}
			text := a.Criterion
			if a.Evidence != "" {
				text += " — " + a.Evidence
			}
			out = append(out, bullet(mark, text, w, ms, textStyle)...)
		}
	}
	if len(r.Issues) > 0 {
		out = append(out, "", headingStyle.Render("ISSUES"))
		for _, is := range r.Issues {
			sev := strings.ToUpper(strings.TrimSpace(is.Severity))
			if sev == "" {
				sev = "NOTE"
			}
			sb := lipgloss.NewStyle().Foreground(severityColor(sev)).Bold(true).Render(sev)
			out = append(out, sb)
			out = append(out, bullet(" ", is.Description, w, mutedStyle, textStyle)...)
			if is.File != "" {
				loc := is.File
				if is.Line > 0 {
					loc = fmt.Sprintf("%s:%d", is.File, is.Line)
				}
				out = append(out, "  "+cyanStyle.Render(truncate(loc, w-2)))
			}
			if is.Suggestion != "" {
				out = append(out, bullet("  →", is.Suggestion, w, goldStyle, mutedStyle)...)
			}
		}
	}
	if len(r.Tests) > 0 {
		out = append(out, "", headingStyle.Render("TESTS"))
		for _, t := range r.Tests {
			out = append(out, bullet("•", t, w, mutedStyle, textStyle)...)
		}
	}
	return out
}

// renderDiff colors a unified diff. Long lines are truncated, not wrapped, so
// the columns stay aligned.
func renderDiff(diff string, w int) []string {
	if strings.TrimSpace(diff) == "" {
		return emptyNote("No changes yet — the diff appears once the executor edits the worktree.", w)
	}
	return append([]string{diffSummary(diff)}, diffBody(diff, w)...)
}

// diffSummary renders "N file(s)  +A −D".
func diffSummary(diff string) string {
	files, add, del := diffStats(diff)
	return mutedStyle.Render(fmt.Sprintf("%d file(s)  ", files)) +
		greenStyle.Render(fmt.Sprintf("+%d", add)) + " " + redStyle.Render(fmt.Sprintf("−%d", del))
}

// diffBody colors each line of a unified diff.
func diffBody(diff string, w int) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		l = strings.ReplaceAll(l, "\t", "    ")
		switch {
		case strings.HasPrefix(l, "diff --git"):
			name := l
			if i := strings.LastIndex(l, " b/"); i >= 0 {
				name = l[i+3:]
			}
			out = append(out, "", violetStyle.Bold(true).Render(truncate("▍"+name, w)))
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"),
			strings.HasPrefix(l, "index "), strings.HasPrefix(l, "new file"), strings.HasPrefix(l, "deleted file"):
			out = append(out, faintStyle.Render(truncate(l, w)))
		case strings.HasPrefix(l, "@@"):
			out = append(out, cyanStyle.Render(truncate(l, w)))
		case strings.HasPrefix(l, "+"):
			out = append(out, greenStyle.Render(truncate(l, w)))
		case strings.HasPrefix(l, "-"):
			out = append(out, redStyle.Render(truncate(l, w)))
		default:
			out = append(out, mutedStyle.Render(truncate(l, w)))
		}
	}
	return out
}

func diffStats(diff string) (files, add, del int) {
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git"):
			files++
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			add++
		case strings.HasPrefix(l, "-"):
			del++
		}
	}
	return
}

func metCount(r *contracts.Review) int {
	n := 0
	for _, a := range r.Acceptance {
		if a.Met {
			n++
		}
	}
	return n
}

func severityColor(sev string) lipgloss.AdaptiveColor {
	switch sev {
	case "BLOCKER", "CRITICAL", "HIGH", "ERROR":
		return cRed
	case "MAJOR", "MEDIUM", "WARNING", "WARN":
		return cAmber
	default:
		return cMuted
	}
}

// bullet wraps text behind a marker with a hanging indent.
func bullet(mark, text string, w int, markStyle, textSt lipgloss.Style) []string {
	mw := lipgloss.Width(mark) + 1
	lines := wrap(text, w-mw)
	out := make([]string, len(lines))
	for i, l := range lines {
		if i == 0 {
			out[i] = markStyle.Render(mark) + " " + textSt.Render(l)
		} else {
			out[i] = strings.Repeat(" ", mw) + textSt.Render(l)
		}
	}
	return out
}

func emptyNote(msg string, w int) []string {
	return append([]string{""}, styleAll(wrap(msg, w), mutedStyle.Italic(true))...)
}

func styleAll(lines []string, st lipgloss.Style) []string {
	for i, l := range lines {
		lines[i] = st.Render(l)
	}
	return lines
}

// wrap word-wraps plain text to width w.
func wrap(s string, w int) []string {
	if w < 8 {
		w = 8
	}
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return []string{""}
	}
	return strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
}

func tokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func elapsed(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
