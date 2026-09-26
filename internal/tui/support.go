package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/guilhermesalviano/korchestrate/internal/agent"
)

// The Support tab runs shell commands in the selected run's checkout and
// streams their output, so the user can inspect or fix a worktree without
// leaving the dashboard. Commands get no stdin: interactive programs won't work.

const (
	maxShellLines = 2000
	shellTimeout  = 10 * time.Minute
)

type shellKind int

const (
	shellCmd shellKind = iota
	shellOut
	shellErr
	shellOK
	shellFail
)

// shellLine is one line of Support tab output.
type shellLine struct {
	kind shellKind
	text string
}

type (
	shellOutMsg struct {
		entry *Entry
		line  shellLine
		ch    chan tea.Msg
	}
	shellDoneMsg struct {
		entry *Entry
		code  int
		err   error
		took  time.Duration
	}
)

// shellDir is where Support commands run: the run's worktree, or the repo
// before the run has a checkout.
func (e *Entry) shellDir() string {
	if e.Run != nil && e.Run.Worktree != "" {
		return e.Run.Worktree
	}
	return e.Repo
}

func (e *Entry) pushShell(l shellLine) {
	e.Shell = append(e.Shell, l)
	if len(e.Shell) > maxShellLines {
		e.Shell = e.Shell[len(e.Shell)-maxShellLines:]
	}
	e.touch()
}

// runShell starts command for e and returns the cmd that streams its output.
func (a *App) runShell(e *Entry, command string) tea.Cmd {
	if command == "clear" {
		e.Shell = nil
		e.touch()
		return nil
	}
	dir := e.shellDir()
	if _, err := os.Stat(dir); dir == "" || err != nil {
		a.notice = "This run has no checkout to run commands in."
		return nil
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.shellStop = cancel
	e.pushShell(shellLine{kind: shellCmd, text: command})
	a.follow = true

	ch := make(chan tea.Msg, 64)
	go func() {
		defer cancel()
		res := agent.Exec(ctx, agent.ProcSpec{
			Bin:     shell,
			Args:    []string{"-c", command},
			Dir:     dir,
			Timeout: shellTimeout,
			OnLine: func(stream, line string) {
				kind := shellOut
				if stream == "stderr" {
					kind = shellErr
				}
				ch <- shellOutMsg{entry: e, line: shellLine{kind: kind, text: line}, ch: ch}
			},
		})
		ch <- shellDoneMsg{entry: e, code: res.ExitCode, err: res.Err, took: res.Duration}
		close(ch)
	}()
	return waitShell(ch)
}

func waitShell(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if m, ok := <-ch; ok {
			return m
		}
		return nil
	}
}

func (a *App) finishShell(t shellDoneMsg) {
	a.mutate(t.entry, func(e *Entry) {
		e.shellStop = nil
		took := t.took.Round(10 * time.Millisecond)
		switch {
		case t.code == 0 && t.err == nil:
			e.pushShell(shellLine{kind: shellOK, text: fmt.Sprintf("exit 0 · %s", took)})
		case t.err != nil && t.code <= 0:
			e.pushShell(shellLine{kind: shellFail, text: fmt.Sprintf("%v · %s", t.err, took)})
		default:
			e.pushShell(shellLine{kind: shellFail, text: fmt.Sprintf("exit %d · %s", t.code, took)})
		}
	})
}

// stopShells cancels every running Support command, e.g. before quitting.
func (a *App) stopShells() {
	for _, e := range a.entries {
		if e.shellStop != nil {
			e.shellStop()
		}
	}
}

// handleShellKey edits and runs the Support tab's command line.
func (a *App) handleShellKey(e *Entry, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "pgdown", "shift+down":
		a.scrollBy(max(1, a.viewH/2))
		return a, nil
	case "pgup", "shift+up":
		a.scrollBy(-max(1, a.viewH/2))
		return a, nil
	case "up":
		if a.shellHistAt > 0 {
			a.shellHistAt--
			a.shellInput = []rune(a.shellHist[a.shellHistAt])
		}
		return a, nil
	case "down":
		if a.shellHistAt < len(a.shellHist)-1 {
			a.shellHistAt++
			a.shellInput = []rune(a.shellHist[a.shellHistAt])
		} else {
			a.shellHistAt, a.shellInput = len(a.shellHist), nil
		}
		return a, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		a.shellFocus = false
	case tea.KeyCtrlC:
		if e.shellStop != nil {
			e.shellStop()
			a.notice = "Stopping command…"
		} else {
			a.shellFocus = false
		}
	case tea.KeyEnter:
		command := strings.TrimSpace(string(a.shellInput))
		if command == "" {
			return a, nil
		}
		if e.shellStop != nil {
			a.notice = "A command is still running; ctrl+c stops it."
			return a, nil
		}
		if n := len(a.shellHist); n == 0 || a.shellHist[n-1] != command {
			a.shellHist = append(a.shellHist, command)
		}
		a.shellHistAt, a.shellInput = len(a.shellHist), nil
		return a, a.runShell(e, command)
	case tea.KeyBackspace:
		if len(a.shellInput) > 0 {
			a.shellInput = a.shellInput[:len(a.shellInput)-1]
		}
	case tea.KeyCtrlU:
		a.shellInput = nil
	case tea.KeyCtrlW:
		a.shellInput = []rune(deleteLastWord(string(a.shellInput)))
	case tea.KeySpace:
		a.shellInput = append(a.shellInput, ' ')
	case tea.KeyRunes:
		a.shellInput = append(a.shellInput, msg.Runes...)
	}
	return a, nil
}

func deleteLastWord(s string) string {
	s = strings.TrimRight(s, " ")
	if i := strings.LastIndexByte(s, ' '); i >= 0 {
		return s[:i+1]
	}
	return ""
}

// renderShell draws the Support tab's scrollback.
func renderShell(e *Entry, w int) []string {
	if len(e.Shell) == 0 {
		return append(emptyNote("Run shell commands in "+e.shellDir()+". Press enter or ! to type one; output streams here. Interactive programs (editors, pagers) aren't supported.", w),
			"", mutedStyle.Render("ctrl+c stops a command · ↑↓ history · \"clear\" empties this view"))
	}
	var lines []string
	for _, l := range e.Shell {
		text := cleanShell(l.text)
		switch l.kind {
		case shellCmd:
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, goldStyle.Bold(true).Render("$ ")+boldStyle.Render(truncate(text, w-2)))
		case shellOK:
			lines = append(lines, greenStyle.Render("✔ "+text))
		case shellFail:
			lines = append(lines, redStyle.Render("✘ "+text))
		case shellErr:
			lines = append(lines, styleAll(wrap(text, w), amberStyle)...)
		default:
			lines = append(lines, styleAll(wrap(text, w), textStyle)...)
		}
	}
	return lines
}

// cleanShell strips escapes and control characters but, unlike sanitize,
// keeps indentation so command output stays readable.
func cleanShell(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\t", "    ")
	return strings.TrimRight(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s), " ")
}

// shellPrompt is the Support tab's command line.
func (a *App) shellPrompt(e *Entry, w int) string {
	placeholder := "enter or ! to type a command"
	switch {
	case e.shellStop != nil:
		placeholder = "running… ctrl+c stops it"
	case a.shellFocus:
		placeholder = "command to run in " + e.shellDir()
	}
	return inputLine("$", string(a.shellInput), a.shellFocus, placeholder, w)
}
