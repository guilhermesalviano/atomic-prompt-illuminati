// Package contracts holds the JSON hand-off types between pipeline stages and
// the embedded schemas / prompts used to constrain each agent.
package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PlanStep is one unit of work produced by the planner.
type PlanStep struct {
	ID           string   `json:"id"`
	Description  string   `json:"description"`
	Files        []string `json:"files,omitempty"`
	Verification string   `json:"verification,omitempty"`
}

// Plan is the structured output of the planner stage.
type Plan struct {
	Summary            string     `json:"summary"`
	Assumptions        []string   `json:"assumptions,omitempty"`
	Files              []string   `json:"files,omitempty"`
	Steps              []PlanStep `json:"steps"`
	AcceptanceCriteria []string   `json:"acceptance_criteria"`
	OutOfScope         []string   `json:"out_of_scope,omitempty"`
}

// Validate reports whether the plan satisfies the minimum contract.
func (p *Plan) Validate() error {
	if strings.TrimSpace(p.Summary) == "" {
		return errors.New("plan.summary is empty")
	}
	if len(p.Steps) == 0 {
		return errors.New("plan.steps is empty")
	}
	for i, s := range p.Steps {
		if strings.TrimSpace(s.Description) == "" {
			return fmt.Errorf("plan.steps[%d].description is empty", i)
		}
	}
	if len(p.AcceptanceCriteria) == 0 {
		return errors.New("plan.acceptance_criteria is empty")
	}
	return nil
}

// Acceptance is one criterion check inside a review.
type Acceptance struct {
	Criterion string `json:"criterion"`
	Met       bool   `json:"met"`
	Evidence  string `json:"evidence,omitempty"`
}

// Issue is a problem the reviewer found.
type Issue struct {
	Severity    string `json:"severity"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion,omitempty"`
}

// Review is the structured output of the reviewer stage.
type Review struct {
	Verdict    string       `json:"verdict"`
	Summary    string       `json:"summary"`
	Acceptance []Acceptance `json:"acceptance,omitempty"`
	Issues     []Issue      `json:"issues,omitempty"`
	Tests      []string     `json:"tests,omitempty"`
}

// ValidVerdicts are the accepted review verdict values.
var ValidVerdicts = map[string]bool{"pass": true, "fail": true}

// Validate reports whether the review satisfies the minimum contract.
func (r *Review) Validate() error {
	v := strings.ToLower(strings.TrimSpace(r.Verdict))
	if !ValidVerdicts[v] {
		return fmt.Errorf("review.verdict %q must be one of pass|fail", r.Verdict)
	}
	if strings.TrimSpace(r.Summary) == "" {
		return errors.New("review.summary is empty")
	}
	for i, is := range r.Issues {
		if strings.TrimSpace(is.Description) == "" {
			return fmt.Errorf("review.issues[%d].description is empty", i)
		}
	}
	return nil
}

// Pass reports whether the review approved the work.
func (r *Review) Pass() bool {
	return strings.EqualFold(strings.TrimSpace(r.Verdict), "pass")
}

// BlockerLines renders the failing issues as a compact instruction list for the
// executor fix loop.
func (r *Review) BlockerLines() string {
	var b strings.Builder
	for _, is := range r.Issues {
		fmt.Fprintf(&b, "- [%s] %s", is.Severity, is.Description)
		if is.File != "" {
			fmt.Fprintf(&b, " (%s", is.File)
			if is.Line > 0 {
				fmt.Fprintf(&b, ":%d", is.Line)
			}
			b.WriteString(")")
		}
		if is.Suggestion != "" {
			fmt.Fprintf(&b, " — %s", is.Suggestion)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ExecReport is the structured output of the executor stage.
type ExecReport struct {
	Status       string   `json:"status"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Summary      string   `json:"summary"`
	Commands     []string `json:"commands,omitempty"`
	KnownGaps    []string `json:"known_gaps,omitempty"`
}

// ExtractJSON returns the first balanced JSON object found in text. It tolerates
// markdown fences and surrounding prose, which is required for agents (such as
// opencode) that cannot be constrained by a native output schema.
func ExtractJSON(text string) (json.RawMessage, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("empty output")
	}
	// Fast path: the whole payload is JSON.
	if json.Valid([]byte(text)) && strings.HasPrefix(text, "{") {
		return json.RawMessage(text), nil
	}
	// Scan for a balanced object, ignoring braces inside strings.
	for start := strings.IndexByte(text, '{'); start >= 0; {
		if end, ok := matchObject(text, start); ok {
			candidate := text[start:end]
			if json.Valid([]byte(candidate)) {
				return json.RawMessage(candidate), nil
			}
		}
		next := strings.IndexByte(text[start+1:], '{')
		if next < 0 {
			break
		}
		start = start + 1 + next
	}
	return nil, errors.New("no valid JSON object found in output")
}

func matchObject(s string, start int) (int, bool) {
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

// DecodeObject decodes raw JSON into dst, ignoring extra fields agents may add.
func DecodeObject(raw json.RawMessage, dst any) error {
	return json.Unmarshal(bytes.TrimSpace(raw), dst)
}
