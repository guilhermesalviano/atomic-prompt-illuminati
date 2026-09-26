package contracts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const structuredMD = `# Add pagination

The users list grows unbounded; page it.

## Overview

Paginate the users endpoint and the list UI.

## Assumptions

- Page size is fixed at 20

## Steps

1. Add limit/offset to the query in ` + "`internal/store/users.go`" + `
2. Thread page params through the handler
   - validate the param first
3. Render a pager in ` + "`web/src/Users.tsx`" + `

## Acceptance Criteria

- [ ] /users?page=2 returns items 21-40
- [ ] the pager disables on the last page

## Out of Scope

- cursor pagination
`

func TestPlanFromMarkdownStructured(t *testing.T) {
	plan, err := PlanFromMarkdown("plan.md", []byte(structuredMD))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if want := "Paginate the users endpoint and the list UI."; plan.Summary != want {
		t.Fatalf("summary = %q, want %q", plan.Summary, want)
	}
	if len(plan.Assumptions) != 1 || !strings.Contains(plan.Assumptions[0], "Page size") {
		t.Fatalf("assumptions = %v", plan.Assumptions)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("steps = %d, want 3: %+v", len(plan.Steps), plan.Steps)
	}
	if plan.Steps[0].ID != "1" || !strings.Contains(plan.Steps[0].Description, "limit/offset") {
		t.Fatalf("first step = %+v", plan.Steps[0])
	}
	if len(plan.Steps[0].Files) != 1 || plan.Steps[0].Files[0] != "internal/store/users.go" {
		t.Fatalf("step files = %v, want internal/store/users.go", plan.Steps[0].Files)
	}
	if !strings.Contains(plan.Steps[1].Description, "validate the param") {
		t.Fatalf("nested bullet not folded into the parent step: %q", plan.Steps[1].Description)
	}
	if len(plan.AcceptanceCriteria) != 2 || !strings.Contains(plan.AcceptanceCriteria[0], "page=2") {
		t.Fatalf("acceptance = %v", plan.AcceptanceCriteria)
	}
	if len(plan.OutOfScope) != 1 {
		t.Fatalf("out of scope = %v", plan.OutOfScope)
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestPlanFromMarkdownMinimal(t *testing.T) {
	src := "# Fix the flaky test\n\n- fix `run_test.go`\n- rerun CI\n"
	plan, err := PlanFromMarkdown("todo.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if plan.Summary != "Fix the flaky test" {
		t.Fatalf("summary = %q, want the H1", plan.Summary)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(plan.Steps))
	}
	if len(plan.AcceptanceCriteria) != 1 || !strings.Contains(plan.AcceptanceCriteria[0], "todo.md") {
		t.Fatalf("acceptance fallback = %v", plan.AcceptanceCriteria)
	}
}

func TestPlanFromMarkdownParagraphsOnly(t *testing.T) {
	src := "## Implementation\n\nFirst do the thing.\n\nThen do the other thing.\n"
	plan, err := PlanFromMarkdown("notes.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %d, want one per paragraph: %+v", len(plan.Steps), plan.Steps)
	}
	if !strings.Contains(plan.Steps[0].Description, "First do the thing") {
		t.Fatalf("unexpected steps: %+v", plan.Steps)
	}
}

func TestPlanFromMarkdownProseAndEmpty(t *testing.T) {
	// Prose without lists still yields steps: one per paragraph.
	plan, err := PlanFromMarkdown("prose.md", []byte("# A plan\n\nJust prose, nothing actionable.\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(plan.Steps) != 1 || !strings.Contains(plan.Steps[0].Description, "Just prose") {
		t.Fatalf("steps = %+v", plan.Steps)
	}
	if _, err := PlanFromMarkdown("empty.md", []byte("# Only a title\n")); err == nil {
		t.Fatal("expected an error when no steps can be derived")
	}
}

func TestLoadPlansMergeAndJSON(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	j := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(a, []byte("# A\n\n## Steps\n\n- first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("# B\n\n## Steps\n\n- second\n- third\n\n## Acceptance Criteria\n\n- both files done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j, []byte(`{"summary":"json plan","steps":[{"id":"9","description":"from json"}],"acceptance_criteria":["json criterion"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	merged, err := LoadPlans([]string{a, b})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if merged.Summary != "A" {
		t.Fatalf("summary = %q, want the first file's", merged.Summary)
	}
	if len(merged.Steps) != 3 || merged.Steps[0].ID != "1" || merged.Steps[2].ID != "3" {
		t.Fatalf("steps not renumbered: %+v", merged.Steps)
	}
	// File A has no criteria of its own, so its fallback travels with the merge.
	if len(merged.AcceptanceCriteria) != 2 || !strings.Contains(strings.Join(merged.AcceptanceCriteria, "|"), "both files done") {
		t.Fatalf("acceptance = %v", merged.AcceptanceCriteria)
	}

	plan, err := LoadPlanFile(j)
	if err != nil {
		t.Fatalf("json plan: %v", err)
	}
	if plan.Summary != "json plan" || plan.Steps[0].ID != "9" {
		t.Fatalf("unexpected json plan: %+v", plan)
	}
	if _, err := LoadPlanFile(filepath.Join(dir, "missing.md")); err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestExtractPlanFiles(t *testing.T) {
	prompt := "use @docs/plan.md and @specs/api.json please,\nignoring @docs/plan.md dupes and mail@example.com"
	paths, rest := ExtractPlanFiles(prompt)
	if len(paths) != 2 || paths[0] != "docs/plan.md" || paths[1] != "specs/api.json" {
		t.Fatalf("paths = %v", paths)
	}
	if strings.Contains(rest, "@docs") || strings.Contains(rest, "@specs") {
		t.Fatalf("mentions not stripped: %q", rest)
	}
	if !strings.Contains(rest, "mail@example.com") {
		t.Fatal("email address must not be treated as a mention")
	}
	if rest != "use and please,\nignoring dupes and mail@example.com" {
		t.Fatalf("rest = %q", rest)
	}

	paths, rest = ExtractPlanFiles("no mentions here")
	if paths != nil || rest != "no mentions here" {
		t.Fatalf("paths=%v rest=%q", paths, rest)
	}

	paths, _ = ExtractPlanFiles("see (@docs/plan.md).")
	if len(paths) != 1 || paths[0] != "docs/plan.md" {
		t.Fatalf("punctuation not trimmed: %v", paths)
	}
}
