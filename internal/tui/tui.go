// Package tui implements the Bubble Tea dashboard: a prompt input, a list of
// worktrees/runs on the left and an ASCII view of the plan → execute → review
// pipeline for the selected run.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/guibs/atomic-prompt-illuminati/internal/agent"
	"github.com/guibs/atomic-prompt-illuminati/internal/artifact"
	"github.com/guibs/atomic-prompt-illuminati/internal/config"
	"github.com/guibs/atomic-prompt-illuminati/internal/contracts"
	"github.com/guibs/atomic-prompt-illuminati/internal/ui"
)

// App is the dashboard model. It implements tea.Model and owns the sessions
// that pipeline runs use as their ui.Gate.
type App struct {
	// OnStart is called when the user submits a prompt. Implementations should
	// run the pipeline and call Session.Finish when done.
	OnStart func(s *Session, prompt string)

	cfg     *config.Config
	entries []*Entry
	cursor  int

	input      []rune
	inputFocus bool

	logs    map[*Entry][]string
	width   int
	height  int
	frame   int
	initial string
	autoRun bool

	mu      sync.Mutex
	lastErr error
	prog    *tea.Program
}

// NewApp builds the dashboard, loading historical runs from baseDir.
func NewApp(cfg *config.Config, baseDir string) *App {
	a := &App{cfg: cfg, width: 100, height: 30, logs: map[*Entry][]string{}}
	runs, _ := artifact.List(baseDir)
	for _, r := range runs {
		a.entries = append(a.entries, entryFromRun(r))
	}
	return a
}

// Attach connects the app to its running program.
func (a *App) Attach(p *tea.Program) { a.prog = p }

// SetInitialPrompt pre-fills the input; when auto is true the run starts
// immediately after the program starts.
func (a *App) SetInitialPrompt(prompt string, auto bool) {
	a.initial = prompt
	a.autoRun = auto
}

// LastError returns the last pipeline error observed.
func (a *App) LastError() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastErr
}

func (a *App) recordError(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	a.lastErr = err
	a.mu.Unlock()
}

func (a *App) send(m tea.Msg) {
	if a.prog != nil {
		a.prog.Send(m)
	}
}

// --- Session: the ui.Gate handed to a pipeline run --------------------------

// Session routes a single pipeline run's gate calls into the dashboard.
type Session struct {
	entry *Entry
	app   *App
}

func (s *Session) Stage(k agent.Kind, msg string) {
	s.app.send(stageEventMsg{entry: s.entry, kind: k, text: msg})
}
func (s *Session) Line(ev agent.Event) { s.app.send(lineEventMsg{entry: s.entry, ev: ev}) }
func (s *Session) Info(msg string)     { s.app.send(infoEventMsg{entry: s.entry, text: msg}) }
func (s *Session) Close()              {}

func (s *Session) PlanGate(ctx context.Context, plan *contracts.Plan, diff string) (ui.Decision, error) {
	return s.gate(ctx, &gateReq{kind: gatePlan, plan: plan, diff: diff})
}

func (s *Session) ReviewGate(ctx context.Context, review *contracts.Review, diff string) (ui.Decision, error) {
	return s.gate(ctx, &gateReq{kind: gateReview, review: review, diff: diff})
}

func (s *Session) gate(ctx context.Context, req *gateReq) (ui.Decision, error) {
	req.reply = make(chan ui.Decision, 1)
	s.app.send(gateEventMsg{entry: s.entry, req: req})
	select {
	case d := <-req.reply:
		return d, nil
	case <-ctx.Done():
		return ui.Reject, ctx.Err()
	}
}

// Finish reports the pipeline outcome and updates the entry.
func (s *Session) Finish(err error, run *artifact.Run) {
	s.app.recordError(err)
	s.app.send(doneEventMsg{entry: s.entry, err: err, run: run})
}

// --- Entry ------------------------------------------------------------------

// StageInfo is the display state of one pipeline stage.
type StageInfo struct {
	status string
	done   bool
	failed bool
}

// Entry is one run shown in the worktree list.
type Entry struct {
	ID      string
	Prompt  string
	Repo    string
	Live    bool
	Run     *artifact.Run
	State   artifact.State
	Session *Session
	Stages  map[agent.Kind]*StageInfo
	Iter    int
	Gate    *gateReq
	ErrText string
	Plan    *contracts.Plan
	Review  *contracts.Review
}

func newEntry(prompt, repo string) *Entry {
	return &Entry{
		ID:     "live-" + shortID(prompt),
		Prompt: prompt,
		Repo:   repo,
		Live:   true,
		State:  artifact.StatePreflight,
		Stages: map[agent.Kind]*StageInfo{
			agent.Planner:  {},
			agent.Executor: {},
			agent.Reviewer: {},
		},
	}
}

func entryFromRun(r *artifact.Run) *Entry {
	e := &Entry{
		ID:      r.ID,
		Prompt:  r.Prompt,
		Repo:    r.Repo,
		Run:     r,
		State:   r.State,
		Iter:    r.Iteration,
		ErrText: r.Error,
		Stages: map[agent.Kind]*StageInfo{
			agent.Planner:  {},
			agent.Executor: {},
			agent.Reviewer: {},
		},
	}
	e.hydrate()
	return e
}

// hydrate derives stage display state and loads plan/review artifacts.
func (e *Entry) hydrate() {
	done := func(k agent.Kind) { e.Stages[k].done = true }
	switch e.State {
	case artifact.StatePlanning:
		e.Stages[agent.Planner].status = "planning"
	case artifact.StateGatePlan:
		e.Stages[agent.Planner].status = "awaiting approval"
		done(agent.Planner)
	case artifact.StateExecuting:
		done(agent.Planner)
		e.Stages[agent.Executor].status = fmt.Sprintf("executing (iter %d)", e.Iter)
	case artifact.StateReviewing, artifact.StateGateReview:
		done(agent.Planner)
		done(agent.Executor)
		e.Stages[agent.Reviewer].status = fmt.Sprintf("reviewing (iter %d)", e.Iter)
	case artifact.StateCommitting, artifact.StateDone:
		done(agent.Planner)
		done(agent.Executor)
		done(agent.Reviewer)
	case artifact.StateFailed, artifact.StateAborted:
		if e.Stages[agent.Reviewer].status != "" {
			e.Stages[agent.Reviewer].failed = true
		} else if e.Stages[agent.Executor].status != "" {
			e.Stages[agent.Executor].failed = true
		} else {
			e.Stages[agent.Planner].failed = true
		}
	}
	if e.Run == nil {
		return
	}
	if data, err := e.Run.Read("plan.json"); err == nil {
		var p contracts.Plan
		if json.Unmarshal(data, &p) == nil {
			e.Plan = &p
		}
	}
	if data, err := e.Run.Read("review.json"); err == nil {
		var r contracts.Review
		if json.Unmarshal(data, &r) == nil {
			e.Review = &r
		}
	}
}

func (e *Entry) title() string {
	if e.Run != nil && e.Run.ID != "" {
		return e.Run.ID
	}
	return e.ID
}

func (e *Entry) stateLabel() (glyph, text string) {
	if e.Gate != nil {
		return "?", "needs you"
	}
	switch e.State {
	case artifact.StateDone:
		return "✓", "done"
	case artifact.StateFailed:
		return "✗", "failed"
	case artifact.StateAborted:
		return "✗", "aborted"
	default:
		if e.Live {
			return "●", string(e.State)
		}
		return "·", string(e.State)
	}
}

// --- messages ---------------------------------------------------------------

type gateKind int

const (
	gatePlan gateKind = iota
	gateReview
)

type gateReq struct {
	kind   gateKind
	plan   *contracts.Plan
	review *contracts.Review
	diff   string
	reply  chan ui.Decision
}

type (
	stageEventMsg struct {
		entry *Entry
		kind  agent.Kind
		text  string
	}
	lineEventMsg struct {
		entry *Entry
		ev    agent.Event
	}
	infoEventMsg struct {
		entry *Entry
		text  string
	}
	gateEventMsg struct {
		entry *Entry
		req   *gateReq
	}
	doneEventMsg struct {
		entry *Entry
		err   error
		run   *artifact.Run
	}
	tickMsg time.Time
)

func tick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{tick()}
	if a.initial != "" {
		a.input = []rune(a.initial)
		if a.autoRun {
			cmds = append(cmds, a.startRun(a.initial))
		} else {
			a.inputFocus = true
		}
	} else {
		a.inputFocus = true
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch t := msg.(type) {
	case tickMsg:
		a.frame++
		return a, tick()

	case tea.WindowSizeMsg:
		a.width, a.height = t.Width, t.Height
		return a, nil

	case stageEventMsg:
		a.mutate(t.entry, func(e *Entry) {
			e.State = stateFromStage(t.kind, e.State)
			applyStage(e, t.kind, t.text)
			a.pushLocked(e, t.text)
		})
		return a, nil

	case lineEventMsg:
		if t.ev.Stream == "status" {
			a.mutate(t.entry, func(e *Entry) { a.pushLocked(e, t.ev.Line) })
		} else if t.ev.Stream == "stderr" && strings.TrimSpace(t.ev.Line) != "" {
			a.mutate(t.entry, func(e *Entry) { a.pushLocked(e, "! "+t.ev.Line) })
		}
		return a, nil

	case infoEventMsg:
		a.mutate(t.entry, func(e *Entry) { a.pushLocked(e, t.text) })
		return a, nil

	case gateEventMsg:
		t.entry.Gate = t.req
		if a.current() == t.entry {
			a.inputFocus = false
		}
		return a, nil

	case doneEventMsg:
		a.mutate(t.entry, func(e *Entry) {
			e.Gate = nil
			e.Live = false
			if t.run != nil {
				e.Run = t.run
				e.State = t.run.State
				e.Iter = t.run.Iteration
			}
			if t.err != nil {
				e.ErrText = t.err.Error()
				if e.State != artifact.StateAborted {
					e.State = artifact.StateFailed
				}
			}
			e.hydrate()
			a.pushLocked(e, "run finished: "+string(e.State))
		})
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(t)
	}
	return a, nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.inputFocus {
		switch msg.Type {
		case tea.KeyEsc, tea.KeyTab:
			a.inputFocus = false
		case tea.KeyCtrlC:
			return a, tea.Quit
		case tea.KeyEnter:
			prompt := strings.TrimSpace(string(a.input))
			a.input = nil
			a.inputFocus = false
			if prompt != "" {
				return a, a.startRun(prompt)
			}
		case tea.KeyBackspace:
			if len(a.input) > 0 {
				a.input = a.input[:len(a.input)-1]
			}
		case tea.KeySpace:
			a.input = append(a.input, ' ')
		case tea.KeyRunes:
			a.input = append(a.input, msg.Runes...)
		}
		return a, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "n", "/", "i":
		a.inputFocus = true
	case "up", "k":
		a.move(-1)
	case "down", "j":
		a.move(1)
	case "a":
		a.answer(ui.Approve)
	case "f":
		a.answer(ui.Fix)
	case "r":
		a.answer(ui.Reject)
	}
	return a, nil
}

// startRun appends a live entry and asks the host to run the pipeline.
func (a *App) startRun(prompt string) tea.Cmd {
	e := newEntry(prompt, a.cfg.Repo)
	s := &Session{entry: e, app: a}
	e.Session = s
	a.entries = append([]*Entry{e}, a.entries...)
	a.cursor = 0
	return func() tea.Msg {
		if a.OnStart != nil {
			a.OnStart(s, prompt)
		}
		return nil
	}
}

func (a *App) move(delta int) {
	if len(a.entries) == 0 {
		return
	}
	a.cursor += delta
	if a.cursor < 0 {
		a.cursor = 0
	}
	if a.cursor >= len(a.entries) {
		a.cursor = len(a.entries) - 1
	}
}

func (a *App) answer(d ui.Decision) {
	e := a.current()
	if e == nil || e.Gate == nil || e.Gate.reply == nil {
		return
	}
	e.Gate.reply <- d
	e.Gate = nil
}

func (a *App) current() *Entry {
	if a.cursor < 0 || a.cursor >= len(a.entries) {
		return nil
	}
	return a.entries[a.cursor]
}

func (a *App) indexOf(e *Entry) int {
	for i, x := range a.entries {
		if x == e {
			return i
		}
	}
	return -1
}

func (a *App) mutate(e *Entry, fn func(*Entry)) {
	i := a.indexOf(e)
	if i < 0 {
		return
	}
	fn(a.entries[i])
}

func (a *App) pushLocked(e *Entry, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	logs := a.logs[e]
	logs = append(logs, line)
	if len(logs) > 400 {
		logs = logs[len(logs)-400:]
	}
	a.logs[e] = logs
}

func stateFromStage(k agent.Kind, cur artifact.State) artifact.State {
	switch k {
	case agent.Planner:
		return artifact.StatePlanning
	case agent.Executor:
		return artifact.StateExecuting
	case agent.Reviewer:
		return artifact.StateReviewing
	}
	return cur
}

func applyStage(e *Entry, k agent.Kind, text string) {
	si := e.Stages[k]
	si.status = text
	low := strings.ToLower(text)
	switch k {
	case agent.Planner:
		if strings.Contains(low, "awaiting plan") {
			e.Stages[agent.Planner].done = true
		}
	case agent.Executor:
		e.Stages[agent.Planner].done = true
		if it, ok := parseIteration(text); ok {
			e.Iter = it
		}
	case agent.Reviewer:
		e.Stages[agent.Executor].done = true
		switch {
		case strings.Contains(low, "passed"):
			e.Stages[agent.Reviewer].done = true
			e.Stages[agent.Reviewer].failed = false
		case strings.Contains(low, "failed"):
			e.Stages[agent.Reviewer].failed = true
			e.Stages[agent.Reviewer].done = false
		default:
			e.Stages[agent.Reviewer].done = false
			e.Stages[agent.Reviewer].failed = false
		}
	}
}

func parseIteration(text string) (int, bool) {
	i := strings.Index(strings.ToLower(text), "iteration ")
	if i < 0 {
		return 0, false
	}
	rest := text[i+len("iteration "):]
	n := 0
	seen := false
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
		seen = true
	}
	return n, seen
}

func shortID(prompt string) string {
	return artifact.Slug(prompt, 24) + "-" + time.Now().Format("150405")
}
