package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/guilhermesalviano/korchestrate/internal/artifact"
	"github.com/guilhermesalviano/korchestrate/internal/contracts"
	"github.com/guilhermesalviano/korchestrate/internal/models"
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestFullViewHidesSidebarAndAside(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 180, 40
	a.showSidebar = true
	a.entries = []*Entry{newEntry("add pagination", "/tmp/repo")}

	if view := a.View(); !strings.Contains(view, "WORKTREES") || !strings.Contains(view, "DIFF") {
		t.Fatalf("normal view should show the run list and diff aside:\n%s", view)
	}
	a.Update(runes("o"))
	view := a.View()
	if strings.Contains(view, "WORKTREES") || strings.Contains(view, "DIFF") {
		t.Fatalf("full view should only show the run:\n%s", view)
	}
	for _, want := range []string{"add pagination", "PLANNER", "exit full view"} {
		if !strings.Contains(view, want) {
			t.Errorf("full view missing %q:\n%s", want, view)
		}
	}
	a.Update(runes("o"))
	if view := a.View(); !strings.Contains(view, "WORKTREES") {
		t.Fatalf("o should restore the layout:\n%s", view)
	}
}

func TestFullViewNeedsARun(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.Update(runes("o"))
	if a.full || a.notice == "" {
		t.Fatalf("full=%v notice=%q, want a hint and no full view", a.full, a.notice)
	}
}

func TestCtrlATogglesModeForNewRuns(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 120, 30
	a.inputFocus = true

	if !strings.Contains(a.View(), "asks before each step") {
		t.Fatalf("default mode should be shown in the prompt box:\n%s", a.View())
	}
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if !a.autopilot || !strings.Contains(a.View(), "opens a PR") {
		t.Fatalf("ctrl+a while typing should switch to autopilot:\n%s", a.View())
	}

	var gotAutopilot bool
	a.OnStart = func(s *Session, _, _ string, _ models.Choices, _ *contracts.Plan) { gotAutopilot = s.Autopilot() }
	a.startRun("", "ship it")()
	if !gotAutopilot || !a.entries[0].Autopilot {
		t.Fatal("the new run should start in autopilot")
	}
	if !strings.Contains(a.View(), "AUTOPILOT") {
		t.Fatalf("an autopilot run should be badged:\n%s", a.View())
	}

	a.inputFocus = false
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if a.autopilot || !a.entries[0].Autopilot {
		t.Fatal("ctrl+a should switch back to default without changing started runs")
	}
}

func TestAutopilotRunEndsWithMessage(t *testing.T) {
	a := NewApp(testConfig(), t.TempDir())
	a.width, a.height = 140, 40
	a.autopilot = true
	a.startRun("", "ship it")
	e := a.entries[0]

	run := &artifact.Run{ID: "r1", Branch: "ship-it", State: artifact.StateDone, Commit: "abcdef1234567", Pushed: true,
		Autopilot: true, PR: "https://github.com/o/r/pull/9"}
	a.Update(doneEventMsg{entry: e, run: run})
	if !strings.Contains(a.notice, "autopilot finished") || !strings.Contains(a.notice, "pull/9") {
		t.Fatalf("notice = %q", a.notice)
	}
	a.notice = ""
	if view := a.View(); !strings.Contains(view, "pull/9") {
		t.Fatalf("the finished run should keep showing its end message:\n%s", view)
	}

	a.startRun("", "break it")
	failed := &artifact.Run{ID: "r2", State: artifact.StateFailed, Error: "review did not pass", Autopilot: true}
	a.Update(doneEventMsg{entry: a.entries[0], run: failed, err: errors.New("review did not pass")})
	if !strings.Contains(a.notice, "autopilot stopped") {
		t.Fatalf("notice = %q", a.notice)
	}
}

func TestHistoricalAutopilotRunKeepsMode(t *testing.T) {
	e := entryFromRun(&artifact.Run{ID: "r", State: artifact.StateDone, Autopilot: true})
	if !e.Autopilot {
		t.Fatal("entries loaded from disk should keep their mode")
	}
}
