package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guibs/atomic-prompt-illuminati/internal/agent"
	"github.com/guibs/atomic-prompt-illuminati/internal/artifact"
	"github.com/guibs/atomic-prompt-illuminati/internal/config"
	"github.com/guibs/atomic-prompt-illuminati/internal/contracts"
	"github.com/guibs/atomic-prompt-illuminati/internal/ui"
)

type fakeAgent struct {
	name string
	kind agent.Kind
	fn   func(ctx context.Context, r agent.Request) (*agent.Result, error)
}

func (f fakeAgent) Name() string     { return f.name }
func (f fakeAgent) Kind() agent.Kind { return f.kind }
func (f fakeAgent) Run(ctx context.Context, r agent.Request) (*agent.Result, error) {
	return f.fn(ctx, r)
}

type recordingGate struct {
	planGates, reviewGates int
	infos                  []string
}

func (g *recordingGate) Stage(agent.Kind, string) {}
func (g *recordingGate) Line(agent.Event)         {}
func (g *recordingGate) Info(m string)            { g.infos = append(g.infos, m) }
func (g *recordingGate) Close()                   {}
func (g *recordingGate) PlanGate(context.Context, *contracts.Plan, string) (ui.Decision, error) {
	g.planGates++
	return ui.Approve, nil
}
func (g *recordingGate) ReviewGate(context.Context, *contracts.Review, string) (ui.Decision, error) {
	g.reviewGates++
	return ui.Approve, nil
}
func (g *recordingGate) CommitGate(context.Context, string, string) (ui.CommitDecision, error) {
	return ui.CommitOnly, nil
}
func (g *recordingGate) SelectAgent(context.Context, agent.Kind, string, []string, string, error) (string, error) {
	return "", nil
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func setupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "init")
	return dir
}

func baseConfig(t *testing.T, repo string) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Repo = repo
	cfg.ArtifactsDir = t.TempDir()
	cfg.Loop.MaxIterations = 1
	cfg.Timeouts.Planner = config.Duration(0)
	cfg.Timeouts.Executor = config.Duration(0)
	cfg.Timeouts.Reviewer = config.Duration(0)
	return cfg
}

func planJSON(t *testing.T) json.RawMessage {
	t.Helper()
	return json.RawMessage(`{"summary":"add feature","steps":[{"id":"1","description":"write feature.txt"}],"acceptance_criteria":["feature.txt exists"]}`)
}

func TestExecuteHappyPath(t *testing.T) {
	repo := setupRepo(t)
	cfg := baseConfig(t, repo)
	gate := &recordingGate{}

	execRuns := 0
	factory := func(name string) (agent.Agent, error) {
		switch name {
		case "claude":
			return fakeAgent{"claude", agent.Planner, func(_ context.Context, _ agent.Request) (*agent.Result, error) {
				return &agent.Result{Structured: planJSON(t)}, nil
			}}, nil
		case "codex":
			return fakeAgent{"codex", agent.Executor, func(_ context.Context, r agent.Request) (*agent.Result, error) {
				execRuns++
				if err := os.WriteFile(filepath.Join(r.Dir, "feature.txt"), []byte("ok\n"), 0o644); err != nil {
					return nil, err
				}
				return &agent.Result{Structured: json.RawMessage(`{"status":"done","summary":"wrote feature"}`)}, nil
			}}, nil
		case "opencode":
			return fakeAgent{"opencode", agent.Reviewer, func(_ context.Context, r agent.Request) (*agent.Result, error) {
				if !strings.Contains(r.Prompt, "feature.txt") {
					t.Error("review prompt missing diff context")
				}
				return &agent.Result{Structured: json.RawMessage(`{"verdict":"pass","summary":"looks good"}`)}, nil
			}}, nil
		}
		return nil, nil
	}

	p := &Pipeline{Cfg: cfg, Opts: Options{Repo: repo, Prompt: "add feature", Name: "test-run"}, Gate: gate, AgentFactory: factory}
	if err := p.Execute(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if p.Run.State != artifact.StateDone {
		t.Fatalf("state = %s, want done", p.Run.State)
	}
	if p.Run.Branch != "test-run" {
		t.Fatalf("branch = %q, want the worktree name", p.Run.Branch)
	}
	if p.Run.Commit == "" {
		t.Fatal("expected a commit")
	}
	if gate.planGates != 1 || gate.reviewGates != 1 {
		t.Fatalf("gates plan=%d review=%d", gate.planGates, gate.reviewGates)
	}
	if execRuns != 1 {
		t.Fatalf("executor ran %d times", execRuns)
	}
	for _, f := range []string{"plan.json", "review.json", "diff.patch", "run.json"} {
		if _, err := p.Run.Read(f); err != nil {
			t.Errorf("missing artifact %s: %v", f, err)
		}
	}
	if data, _ := p.Run.Read("diff.patch"); !strings.Contains(string(data), "feature.txt") {
		t.Errorf("diff.patch missing feature.txt:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(p.Run.Worktree, "feature.txt")); err != nil {
		t.Errorf("worktree missing file: %v", err)
	}
}

func TestExecuteFixLoopThenPass(t *testing.T) {
	repo := setupRepo(t)
	cfg := baseConfig(t, repo)
	cfg.Loop.MaxIterations = 2
	gate := &recordingGate{}

	execRuns, reviewRuns := 0, 0
	factory := func(name string) (agent.Agent, error) {
		switch name {
		case "claude":
			return fakeAgent{"claude", agent.Planner, func(context.Context, agent.Request) (*agent.Result, error) {
				return &agent.Result{Structured: planJSON(t)}, nil
			}}, nil
		case "codex":
			return fakeAgent{"codex", agent.Executor, func(_ context.Context, r agent.Request) (*agent.Result, error) {
				execRuns++
				if execRuns > 1 && !strings.Contains(r.Prompt, "Fix required") {
					t.Error("second executor run missing fix instructions")
				}
				_ = os.WriteFile(filepath.Join(r.Dir, "feature.txt"), []byte("ok\n"), 0o644)
				return &agent.Result{Structured: json.RawMessage(`{"status":"done","summary":"x"}`)}, nil
			}}, nil
		case "opencode":
			return fakeAgent{"opencode", agent.Reviewer, func(context.Context, agent.Request) (*agent.Result, error) {
				reviewRuns++
				if reviewRuns == 1 {
					return &agent.Result{Structured: json.RawMessage(`{"verdict":"fail","summary":"needs work","issues":[{"severity":"blocker","description":"missing tests"}]}`)}, nil
				}
				return &agent.Result{Structured: json.RawMessage(`{"verdict":"pass","summary":"fixed"}`)}, nil
			}}, nil
		}
		return nil, nil
	}

	p := &Pipeline{Cfg: cfg, Opts: Options{Repo: repo, Prompt: "add feature", Name: "test-run"}, Gate: gate, AgentFactory: factory}
	if err := p.Execute(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if execRuns != 2 || reviewRuns != 2 {
		t.Fatalf("exec=%d review=%d, want 2/2", execRuns, reviewRuns)
	}
	if p.Run.Iteration != 1 {
		t.Fatalf("iteration = %d, want 1", p.Run.Iteration)
	}
}

type fallbackGate struct {
	recordingGate
	choose string
}

func (g *fallbackGate) SelectAgent(context.Context, agent.Kind, string, []string, string, error) (string, error) {
	return g.choose, nil
}

func TestExecutorFallsBackOnFailure(t *testing.T) {
	repo := setupRepo(t)
	cfg := baseConfig(t, repo)
	gate := &fallbackGate{choose: "opencode"}

	codexRuns, opencodeExecRuns := 0, 0
	factory := func(name string) (agent.Agent, error) {
		switch name {
		case "claude":
			return fakeAgent{"claude", agent.Planner, func(context.Context, agent.Request) (*agent.Result, error) {
				return &agent.Result{Structured: planJSON(t)}, nil
			}}, nil
		case "codex":
			return fakeAgent{"codex", agent.Executor, func(context.Context, agent.Request) (*agent.Result, error) {
				codexRuns++
				return nil, errors.New("codex not working")
			}}, nil
		case "opencode":
			return fakeAgent{"opencode", agent.Executor, func(_ context.Context, r agent.Request) (*agent.Result, error) {
				if r.OutFile == "" {
					return &agent.Result{Structured: json.RawMessage(`{"verdict":"pass","summary":"ok"}`)}, nil
				}
				opencodeExecRuns++
				_ = os.WriteFile(filepath.Join(r.Dir, "feature.txt"), []byte("ok\n"), 0o644)
				return &agent.Result{Structured: json.RawMessage(`{"status":"done","summary":"wrote"}`)}, nil
			}}, nil
		}
		return nil, nil
	}

	p := &Pipeline{Cfg: cfg, Opts: Options{Repo: repo, Prompt: "add feature", Name: "test-run"}, Gate: gate, AgentFactory: factory}
	if err := p.Execute(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if codexRuns != 1 || opencodeExecRuns != 1 {
		t.Fatalf("codex=%d opencode=%d, want 1/1", codexRuns, opencodeExecRuns)
	}
	if p.Run.State != artifact.StateDone {
		t.Fatalf("state = %s, want done", p.Run.State)
	}
}

func TestDefaultBranchFromCurrentBranch(t *testing.T) {
	repo := setupRepo(t)
	cfg := baseConfig(t, repo)
	gate := &recordingGate{}
	factory := func(name string) (agent.Agent, error) {
		switch name {
		case "claude":
			return fakeAgent{name, agent.Planner, func(context.Context, agent.Request) (*agent.Result, error) {
				return &agent.Result{Structured: planJSON(t)}, nil
			}}, nil
		case "codex":
			return fakeAgent{name, agent.Executor, func(_ context.Context, r agent.Request) (*agent.Result, error) {
				_ = os.WriteFile(filepath.Join(r.Dir, "feature.txt"), []byte("ok\n"), 0o644)
				return &agent.Result{Structured: json.RawMessage(`{"status":"done","summary":"x"}`)}, nil
			}}, nil
		case "opencode":
			return fakeAgent{name, agent.Reviewer, func(context.Context, agent.Request) (*agent.Result, error) {
				return &agent.Result{Structured: json.RawMessage(`{"verdict":"pass","summary":"ok"}`)}, nil
			}}, nil
		}
		return nil, nil
	}

	// First run without a name lands on api/main (setupRepo inits with -b main).
	p := &Pipeline{Cfg: cfg, Opts: Options{Repo: repo, Prompt: "add feature"}, Gate: gate, AgentFactory: factory}
	if err := p.Execute(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if p.Run.Branch != "api/main" {
		t.Fatalf("branch = %q, want api/main", p.Run.Branch)
	}

	// A second nameless run must not collide: api/main is kept on success.
	p2 := &Pipeline{Cfg: cfg, Opts: Options{Repo: repo, Prompt: "add feature again"}, Gate: gate, AgentFactory: factory}
	if err := p2.Execute(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if p2.Run.Branch != "api/main-2" {
		t.Fatalf("branch = %q, want api/main-2", p2.Run.Branch)
	}
}

type rejectPlanGate struct{ recordingGate }

func (g *rejectPlanGate) PlanGate(context.Context, *contracts.Plan, string) (ui.Decision, error) {
	return ui.Reject, nil
}

func TestRejectedPlanStaysAborted(t *testing.T) {
	repo := setupRepo(t)
	cfg := baseConfig(t, repo)
	factory := func(name string) (agent.Agent, error) {
		return fakeAgent{name, agent.Planner, func(context.Context, agent.Request) (*agent.Result, error) {
			return &agent.Result{Structured: planJSON(t)}, nil
		}}, nil
	}
	p := &Pipeline{Cfg: cfg, Opts: Options{Repo: repo, Prompt: "add feature", Name: "test-run"}, Gate: &rejectPlanGate{}, AgentFactory: factory}
	if err := p.Execute(context.Background()); err == nil {
		t.Fatal("expected rejection error")
	}
	saved, err := artifact.Load(p.Run.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != artifact.StateAborted || saved.Error == "" {
		t.Fatalf("persisted state = %s (error %q), want aborted with error", saved.State, saved.Error)
	}
}
