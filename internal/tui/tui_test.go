package tui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guilhermesalviano/korchestrate/internal/agent"
	"github.com/guilhermesalviano/korchestrate/internal/artifact"
	"github.com/guilhermesalviano/korchestrate/internal/config"
	"github.com/guilhermesalviano/korchestrate/internal/contracts"
	"github.com/guilhermesalviano/korchestrate/internal/ui"
)

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Repo = "/tmp/repo"
	cfg.ArtifactsDir = "/tmp/runs"
	return cfg
}

func TestViewRendersPanes(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 140, 40
	a.showSidebar = true
	a.showAside = false

	e := newEntry("add rate limiting", "/tmp/repo")
	e.State = artifact.StateExecuting
	e.Stages[agent.Planner].done = true
	e.Stages[agent.Executor].status = "executing (iter 1)"
	e.Iter = 1
	a.entries = []*Entry{e}
	a.cursor = 0

	view := a.View()
	for _, want := range []string{"WORKTREES", "PLANNER", "EXECUTOR", "REVIEWER", "executing", "fix loop"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestViewHorizontalFlow(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 160, 40
	e := newEntry("wide", "/tmp/repo")
	a.entries = []*Entry{e}
	a.cursor = 0
	view := a.View()
	// On a wide terminal all three stages share one line.
	found := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "PLANNER") {
			if !strings.Contains(line, "REVIEWER") {
				t.Fatalf("expected horizontal flow on one line:\nFULL:\n%s", view)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("PLANNER not found in view:\n%s", view)
	}
}

func TestViewVerticalFlow(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 70, 40
	e := newEntry("narrow", "/tmp/repo")
	a.entries = []*Entry{e}
	a.cursor = 0
	view := a.View()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "PLANNER") && strings.Contains(line, "REVIEWER") {
			t.Fatalf("expected vertical flow on a narrow terminal:\n%s", line)
		}
	}
}

func TestViewEmpty(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 80, 24
	view := a.View()
	if strings.Contains(view, "WORKTREES") || !strings.Contains(view, "type a prompt") {
		t.Fatalf("unexpected empty view:\n%s", view)
	}
}

func TestParseIterationAndApplyStage(t *testing.T) {
	if n, ok := parseIteration("executing iteration 3 with gpt-6-sol"); !ok || n != 3 {
		t.Fatalf("parseIteration = %d,%v", n, ok)
	}
	e := newEntry("x", "/repo")
	applyStage(e, agent.Reviewer, "review failed")
	if !e.Stages[agent.Reviewer].failed || !e.Stages[agent.Executor].done {
		t.Fatalf("stage state wrong: %+v", e.Stages)
	}
	applyStage(e, agent.Reviewer, "review passed")
	if e.Stages[agent.Reviewer].failed || !e.Stages[agent.Reviewer].done {
		t.Fatalf("pass did not clear failure: %+v", e.Stages)
	}
}

func TestEntryFromRunLoadsArtifacts(t *testing.T) {
	base := t.TempDir()
	r, err := artifact.New(base, "/repo", "do a thing")
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := json.Marshal(map[string]any{
		"summary":             "s",
		"steps":               []map[string]string{{"id": "1", "description": "d"}},
		"acceptance_criteria": []string{"c"},
	})
	if err := r.Write("plan.json", plan); err != nil {
		t.Fatal(err)
	}
	review, _ := json.Marshal(map[string]any{"verdict": "pass", "summary": "ok"})
	if err := r.Write("review.json", review); err != nil {
		t.Fatal(err)
	}
	if err := r.SetState(artifact.StateDone); err != nil {
		t.Fatal(err)
	}

	e := entryFromRun(r)
	if e.Plan == nil || e.Review == nil {
		t.Fatalf("artifacts not loaded: plan=%v review=%v", e.Plan, e.Review)
	}
	if !e.Stages[agent.Planner].done || !e.Stages[agent.Reviewer].done {
		t.Fatalf("stages not hydrated for done run: %+v", e.Stages)
	}
}

func TestStartRunPrependsEntry(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 100, 30
	cmd := a.startRun("wt", "first")
	if len(a.entries) != 1 || a.cursor != 0 {
		t.Fatalf("entry not added: %d", len(a.entries))
	}
	if cmd == nil {
		t.Fatal("expected startup command")
	}
	a.startRun("wt2", "second")
	if len(a.entries) != 2 || !strings.HasPrefix(a.entries[0].Prompt, "second") {
		t.Fatalf("newest entry not first: %+v", a.entries)
	}
}

func TestFailedRunMarksStageFromArtifacts(t *testing.T) {
	base := t.TempDir()
	r, err := artifact.New(base, "/repo", "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Write("executor.events.0.jsonl", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if err := r.Fail(errors.New("codex exited 1")); err != nil {
		t.Fatal(err)
	}
	e := entryFromRun(r)
	if !e.Stages[agent.Planner].done || !e.Stages[agent.Executor].failed || e.Stages[agent.Reviewer].failed {
		t.Fatalf("want planner done, executor failed: %+v %+v %+v",
			e.Stages[agent.Planner], e.Stages[agent.Executor], e.Stages[agent.Reviewer])
	}
}

func TestGateIgnoresKeysItDoesNotOffer(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	e := newEntry("x", "/repo")
	a.entries = []*Entry{e}

	failed := &contracts.Review{Verdict: "fail", Summary: "no"}
	e.Gate = &gateReq{kind: gateReview, review: failed, reply: make(chan ui.Decision, 1)}
	a.answer(ui.Approve)
	if e.Gate == nil {
		t.Fatal("approve must not resolve a failed review gate")
	}
	req := e.Gate
	a.answer(ui.Fix)
	if e.Gate != nil || <-req.reply != ui.Fix {
		t.Fatal("fix should resolve the failed review gate")
	}

	e.Gate = &gateReq{kind: gatePlan, reply: make(chan ui.Decision, 1)}
	a.answer(ui.Fix)
	if e.Gate == nil {
		t.Fatal("fix must not resolve a plan gate")
	}
}

func TestCommitGateChoices(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	e := newEntry("x", "/repo")
	a.entries = []*Entry{e}

	req := &gateReq{kind: gateCommit, branch: "my-branch", commitReply: make(chan ui.CommitDecision, 1)}
	e.Gate = req
	a.answer(ui.Approve)
	if e.Gate == nil {
		t.Fatal("approve must not resolve a commit gate")
	}
	a.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if e.Gate != nil || <-req.commitReply != ui.CommitAndPush {
		t.Fatal("p should resolve the commit gate with commit+push")
	}
}

func TestWorktreeGateChoices(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 100, 30
	e := newEntry("x", "/repo")
	a.entries = []*Entry{e}

	req := &gateReq{kind: gateWorktree, branch: "main-2", worktreeReply: make(chan ui.WorktreeDecision, 1)}
	e.Gate = req
	a.answer(ui.Approve)
	if e.Gate == nil {
		t.Fatal("approve must not resolve a worktree gate")
	}
	a.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if e.Gate != nil || <-req.worktreeReply != ui.WorktreeCreate {
		t.Fatal("c should resolve the worktree gate with create")
	}

	req = &gateReq{kind: gateWorktree, branch: "main-2", worktreeReply: make(chan ui.WorktreeDecision, 1)}
	e.Gate = req
	a.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if e.Gate != nil || <-req.worktreeReply != ui.WorktreeReuse {
		t.Fatal("enter should resolve the worktree gate with reuse")
	}

	e.Gate = &gateReq{kind: gateWorktree, branch: "main-2"}
	if v := a.View(); !strings.Contains(v, "main-2") || !strings.Contains(v, "keep existing") {
		t.Fatalf("view should offer keep-or-create for the existing branch:\n%s", v)
	}
}

func TestSummarizeEvent(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{`{"type":"item.started","item":{"type":"command_execution","command":"go test ./..."}}`, "$ go test ./..."},
		{`{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"a.go","kind":"update"}]}}`, "update a.go"},
		{`{"type":"tool_use","part":{"tool":"read","state":{"title":"main.go"}}}`, "read main.go"},
		{`{"type":"step_finish","part":{}}`, ""},
		{"plain \x1b[31mred\x1b[0m\ttext", "plain red    text"},
	}
	for _, c := range cases {
		l, ok := summarizeEvent(agent.Event{Kind: agent.Executor, Stream: "stdout", Line: c.line})
		if c.want == "" {
			if ok {
				t.Errorf("%s: expected event to be dropped, got %q", c.line, l.text)
			}
			continue
		}
		if !ok || !strings.Contains(l.text, c.want) {
			t.Errorf("%s: got %q, want it to contain %q", c.line, l.text, c.want)
		}
	}
}

func TestViewFillsTerminalExactly(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	e := newEntry("a long prompt that should wrap across more than one line in the header of the main pane", "/repo")
	e.Plan = &contracts.Plan{Summary: "s", Steps: []contracts.PlanStep{{ID: "1", Description: "d"}}, AcceptanceCriteria: []string{"c"}}
	e.Review = &contracts.Review{Verdict: "fail", Summary: "bad", Issues: []contracts.Issue{{Severity: "blocker", Description: "boom"}}}
	e.Diff = "diff --git a/x b/x\n+added\n-removed\n"
	e.Gate = &gateReq{kind: gateReview, review: e.Review}
	e.ErrText = "something broke"
	for i := 0; i < 50; i++ {
		e.push(logLine{kind: agent.Executor, text: "line"})
	}
	a.entries = []*Entry{e}
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 40}, {200, 60}} {
		a.width, a.height = size[0], size[1]
		for tb := tab(0); tb < tabCount; tb++ {
			a.setTab(tb)
			lines := strings.Split(a.View(), "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d tab %d: %d lines", size[0], size[1], tb, len(lines))
			}
			for i, l := range lines {
				if lw := lipgloss.Width(l); lw > size[0] {
					t.Errorf("%dx%d tab %d line %d: width %d", size[0], size[1], tb, i, lw)
				}
			}
		}
	}
}

func TestDiffAsideOnWideTerminals(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	e := newEntry("x", "/repo")
	e.Diff = "diff --git a/f.go b/f.go\n+added line\n-removed line\n"
	a.entries = []*Entry{e}

	a.width, a.height = 180, 40
	if v := a.View(); !strings.Contains(v, "+added line") || !strings.Contains(v, "DIFF") {
		t.Fatalf("aside missing on wide terminal:\n%s", v)
	}
	a.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if v := a.View(); strings.Contains(v, "+added line") {
		t.Fatal("d should hide the aside")
	}

	a.showAside = true
	a.width = 100
	if v := a.View(); strings.Contains(v, "+added line") {
		t.Fatal("aside should not squeeze a narrow terminal")
	}
	a.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if a.tab != tabDiff {
		t.Fatal("d on a narrow terminal should open the Diff tab")
	}
}

func TestLiveDiffIgnoredAfterFinish(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	e := newEntry("x", "/repo")
	e.Diff = "final"
	e.Live = false
	a.entries = []*Entry{e}
	a.Update(diffMsg{entry: e, diff: ""})
	if e.Diff != "final" {
		t.Fatal("late snapshot overwrote the finished run's diff")
	}
}
