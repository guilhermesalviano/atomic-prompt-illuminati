package ui

import (
	"fmt"
	"strings"

	"github.com/guilhermesalviano/korchestrate/internal/contracts"
)

// RenderPlan formats a plan as human-readable text.
func RenderPlan(p *contracts.Plan) string {
	if p == nil {
		return "(no plan)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Summary: %s\n", p.Summary)
	if len(p.Assumptions) > 0 {
		b.WriteString("\nAssumptions:\n")
		for _, a := range p.Assumptions {
			fmt.Fprintf(&b, "  - %s\n", a)
		}
	}
	if len(p.Files) > 0 {
		fmt.Fprintf(&b, "\nFiles: %s\n", strings.Join(p.Files, ", "))
	}
	b.WriteString("\nSteps:\n")
	for i, s := range p.Steps {
		fmt.Fprintf(&b, "  %d. [%s] %s\n", i+1, s.ID, s.Description)
		if len(s.Files) > 0 {
			fmt.Fprintf(&b, "     files: %s\n", strings.Join(s.Files, ", "))
		}
		if s.Verification != "" {
			fmt.Fprintf(&b, "     verify: %s\n", s.Verification)
		}
	}
	b.WriteString("\nAcceptance criteria:\n")
	for _, c := range p.AcceptanceCriteria {
		fmt.Fprintf(&b, "  - %s\n", c)
	}
	if len(p.OutOfScope) > 0 {
		b.WriteString("\nOut of scope:\n")
		for _, o := range p.OutOfScope {
			fmt.Fprintf(&b, "  - %s\n", o)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderReview formats a review as human-readable text.
func RenderReview(r *contracts.Review) string {
	if r == nil {
		return "(no review)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Verdict: %s\n%s\n", strings.ToUpper(r.Verdict), r.Summary)
	if len(r.Acceptance) > 0 {
		b.WriteString("\nAcceptance:\n")
		for _, a := range r.Acceptance {
			mark := "x"
			if a.Met {
				mark = "v"
			}
			fmt.Fprintf(&b, "  [%s] %s", mark, a.Criterion)
			if a.Evidence != "" {
				fmt.Fprintf(&b, " — %s", a.Evidence)
			}
			b.WriteByte('\n')
		}
	}
	if len(r.Issues) > 0 {
		b.WriteString("\nIssues:\n")
		for _, is := range r.Issues {
			loc := is.File
			if is.Line > 0 {
				loc = fmt.Sprintf("%s:%d", is.File, is.Line)
			}
			fmt.Fprintf(&b, "  [%s] %s", is.Severity, is.Description)
			if loc != "" {
				fmt.Fprintf(&b, " (%s)", loc)
			}
			b.WriteByte('\n')
			if is.Suggestion != "" {
				fmt.Fprintf(&b, "       -> %s\n", is.Suggestion)
			}
		}
	}
	if len(r.Tests) > 0 {
		b.WriteString("\nTests: " + strings.Join(r.Tests, ", ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
