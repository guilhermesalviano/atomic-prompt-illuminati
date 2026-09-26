package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guilhermesalviano/korchestrate/internal/agent"
	"github.com/guilhermesalviano/korchestrate/internal/artifact"
	"github.com/guilhermesalviano/korchestrate/internal/models"
)

func TestCompactDashboardFitsAndKeepsControls(t *testing.T) {
	for _, size := range [][2]int{{24, 16}, {32, 16}, {40, 20}, {60, 16}, {80, 24}, {1, 1}} {
		a := NewApp(testConfig(), t.TempDir())
		a.width, a.height = size[0], size[1]
		e := newEntry("Update a long feature description", "/repo")
		e.Gate = &gateReq{kind: gateRetry, step: "executor", cause: errors.New("temporary error")}
		a.entries = []*Entry{e}
		for _, sidebar := range []bool{false, true} {
			a.showSidebar = sidebar
			view := a.View()
			if lipgloss.Height(view) != size[1] {
				t.Fatalf("%v: height %d", size, lipgloss.Height(view))
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > size[0] {
					t.Fatalf("%v: overflow %q", size, line)
				}
			}
			if size[0] >= 24 && !sidebar {
				for _, want := range []string{"retry", "stop", "Activity"} {
					if !strings.Contains(view, want) {
						t.Fatalf("%v: missing %s:\n%s", size, want, view)
					}
				}
			}
		}
	}
}

func TestSidebarToggleAndCompactSelection(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 40, 24
	a.entries = []*Entry{newEntry("first", "/repo"), newEntry("second", "/repo")}
	if strings.Contains(a.View(), "WORKTREES") {
		t.Fatal("sidebar should start closed")
	}
	a.handleKey(key("b"))
	if view := a.View(); !strings.Contains(view, "WORKTREES") || strings.Contains(view, "PLANNER") {
		t.Fatalf("expected full-width run list:\n%s", view)
	}
	a.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	a.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if a.showSidebar || a.cursor != 1 {
		t.Fatal("enter should open the selected run")
	}
	a.width = 120
	a.showSidebar = true
	a.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	if a.showSidebar {
		t.Fatal("resizing to narrow should close sidebar")
	}
}

func TestCompactSetupFits(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 32, 20
	a.setup.active = true
	for _, mode := range []setupMode{setupStages, setupProvider, setupModel, setupEffort} {
		a.setup.mode = mode
		view := a.View()
		if lipgloss.Height(view) != a.height || lipgloss.Width(view) > a.width {
			t.Fatalf("mode %v overflows:\n%s", mode, view)
		}
	}
}

func TestRetryAndPublishKeys(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	e := newEntry("x", "/repo")
	e.Run = &artifact.Run{Worktree: "/repo", Branch: "main"}
	s := &Session{entry: e, app: a, publish: make(chan publishRequest, 1), done: make(chan struct{})}
	e.Session = s
	a.entries = []*Entry{e}
	req := &gateReq{kind: gateRetry, retryReply: make(chan bool, 1)}
	e.Gate = req
	_, cmd := a.handleKey(key("p"))
	if cmd == nil || !e.publishing || e.Gate != req {
		t.Fatal("p must queue publish without dismissing retry")
	}
	called := false
	s.SetPublishHandler(func() error { called = true; return nil })
	s.ProcessControls()
	a.Update(cmd())
	if !called || e.publishing {
		t.Fatal("publish did not complete")
	}
	a.handleKey(key("t"))
	if e.Gate != nil || !<-req.retryReply {
		t.Fatal("t must retry")
	}
	agentReq := &gateReq{kind: gateAgent, agentReply: make(chan string, 1)}
	e.Gate = agentReq
	a.handleKey(key("t"))
	if got := <-agentReq.agentReply; got != "retry" {
		t.Fatal(got)
	}
}

func TestGateServicesPublishWithoutLosingAnswer(t *testing.T) {
	s := &Session{publish: make(chan publishRequest, 1)}
	published := make(chan error, 1)
	s.SetPublishHandler(func() error { return nil })
	s.publish <- publishRequest{reply: published}
	answer := make(chan bool, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		select {
		case <-published:
			answer <- true
		case <-ctx.Done():
		}
	}()
	got, err := awaitSession(ctx, s, (<-chan bool)(answer))
	if err != nil || !got {
		t.Fatalf("gate: %v %v", got, err)
	}
}

func TestRetryRefreshesFailedStage(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	e := newEntry("x", "/repo")
	e.Stages[agent.Executor].status = "executing (iter 1)"
	a.entries = []*Entry{e}
	req := &gateReq{kind: gateRetry, step: "executor", retryReply: make(chan bool, 1)}
	a.Update(gateEventMsg{entry: e, req: req})
	if !e.Stages[agent.Executor].failed || e.Stages[agent.Executor].done {
		t.Fatalf("retry gate should mark the executor failed: %+v", e.Stages[agent.Executor])
	}
	a.handleKey(key("t"))
	if e.Gate != nil || !<-req.retryReply {
		t.Fatal("t must retry")
	}
	si := e.Stages[agent.Executor]
	if si.failed || si.done || si.status != "retrying…" {
		t.Fatalf("retry should refresh the stage: %+v", si)
	}
}

func TestRetryFailedRunResumesAtActiveTab(t *testing.T) {
	cases := []struct {
		tab  tab
		want agent.Kind
	}{
		{tabReview, agent.Reviewer},
		{tabDiff, agent.Executor},
		{tabPlan, agent.Planner},
		{tabActivity, agent.Reviewer}, // the stage that failed
	}
	for _, c := range cases {
		a := NewApp(testConfig(), t.TempDir())
		run := &artifact.Run{ID: "r", State: artifact.StateFailed, Dir: t.TempDir(), Worktree: t.TempDir(),
			Error: `reviewer failed: invalid review: review.verdict "" must be one of pass|fail`}
		if err := run.Write("plan.json", []byte(`{"summary":"s","steps":[{"description":"d"}],"acceptance_criteria":["a"]}`)); err != nil {
			t.Fatal(err)
		}
		_ = run.Write("reviewer.events.0.jsonl", nil)
		e := entryFromRun(run)
		a.entries = []*Entry{e}
		a.setTab(c.tab)
		var got agent.Kind
		a.OnRetry = func(s *Session, r *artifact.Run, from agent.Kind, _ models.Choices) {
			if s.entry != e || r != run {
				t.Error("retry must resume the selected run")
			}
			got = from
		}
		_, cmd := a.handleKey(key("t"))
		if cmd == nil {
			t.Fatalf("tab %d: t should retry the failed run (notice %q)", c.tab, a.notice)
		}
		cmd()
		if got != c.want {
			t.Errorf("tab %d: resumed at %q, want %q", c.tab, got, c.want)
		}
		if !e.Live || e.ErrText != "" || e.Stages[c.want].failed || e.Stages[c.want].status != "retrying…" {
			t.Errorf("tab %d: entry not reset for retry: live=%v err=%q stage=%+v", c.tab, e.Live, e.ErrText, e.Stages[c.want])
		}
	}
}

func TestRetryNeedsTheWorktree(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	run := &artifact.Run{ID: "r", State: artifact.StateFailed, Dir: t.TempDir(), Worktree: "/nonexistent/wt"}
	a.entries = []*Entry{entryFromRun(run)}
	a.OnRetry = func(*Session, *artifact.Run, agent.Kind, models.Choices) { t.Error("must not retry") }
	if _, cmd := a.handleKey(key("t")); cmd != nil || !strings.Contains(a.notice, "worktree is gone") {
		t.Fatalf("expected a worktree notice, got %q", a.notice)
	}
}

func TestWaitingGatesAllowDiffAndScroll(t *testing.T) {
	for _, kind := range []gateKind{gateRetry, gateCommit, gateAgent, gateWorktree} {
		a := NewApp(testConfig(), t.TempDir())
		e := newEntry("x", "/repo")
		req := &gateReq{kind: kind}
		e.Gate = req
		a.entries = []*Entry{e}
		a.handleKey(key("d"))
		if a.tab != tabDiff || e.Gate != req {
			t.Fatalf("gate %v blocked Diff", kind)
		}
		a.viewH, a.lastMax = 10, 100
		a.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
		if a.scroll == 0 || e.Gate != req {
			t.Fatalf("gate %v blocked scrolling", kind)
		}
	}
}
