package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/guilhermesalviano/korchestrate/internal/artifact"
)

// drain feeds a command's messages back into the app until it stops producing
// follow-ups, the way the Bubble Tea runtime would.
func drain(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return
		}
		_, cmd = a.Update(msg)
	}
}

func typeText(a *App, s string) {
	for _, r := range s {
		if r == ' ' {
			a.handleKey(tea.KeyMsg{Type: tea.KeySpace})
			continue
		}
		a.handleKey(key(string(r)))
	}
}

func TestSupportTabRunsCommandInWorktree(t *testing.T) {
	t.Setenv("SHELL", "sh")
	dir := t.TempDir()
	a := NewApp(testConfig(), t.TempDir())
	e := newEntry("x", "/repo")
	e.Run = &artifact.Run{Worktree: dir}
	a.entries = []*Entry{e}
	a.inputFocus = false

	a.handleKey(key("5"))
	if a.tab != tabSupport {
		t.Fatalf("tab = %d", a.tab)
	}
	a.handleKey(enter())
	if !a.shellFocus {
		t.Fatal("enter must focus the command line")
	}
	typeText(a, "pwd; echo oops >&2; exit 3")
	_, cmd := a.handleKey(enter())
	if e.shellStop == nil {
		t.Fatal("command should be running")
	}
	drain(t, a, cmd)

	var got []string
	for _, l := range e.Shell {
		got = append(got, l.text)
	}
	out := strings.Join(got, "\n")
	for _, want := range []string{"pwd; echo oops >&2; exit 3", dir, "oops", "exit 3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if last := e.Shell[len(e.Shell)-1]; last.kind != shellFail || e.shellStop != nil {
		t.Fatalf("last line = %+v, running = %v", last, e.shellStop != nil)
	}
	if len(a.shellHist) != 1 {
		t.Fatalf("history = %v", a.shellHist)
	}

	// History recall, then "clear" empties the scrollback without running.
	a.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if string(a.shellInput) != "pwd; echo oops >&2; exit 3" {
		t.Fatalf("history recall = %q", string(a.shellInput))
	}
	a.handleKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	typeText(a, "clear")
	if _, cmd := a.handleKey(enter()); cmd != nil || len(e.Shell) != 0 {
		t.Fatal("clear must empty the view")
	}
	a.handleKey(esc())
	if a.shellFocus {
		t.Fatal("esc must leave the command line")
	}
}
