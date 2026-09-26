package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guilhermesalviano/korchestrate/internal/artifact"
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
