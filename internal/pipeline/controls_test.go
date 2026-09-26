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

	"github.com/guilhermesalviano/korchestrate/internal/agent"
	"github.com/guilhermesalviano/korchestrate/internal/artifact"
	"github.com/guilhermesalviano/korchestrate/internal/ui"
	"github.com/guilhermesalviano/korchestrate/internal/worktree"
)

func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCurrentBranchRunsInPlace(t *testing.T) {
	for _, name := range []string{"", "main"} {
		t.Run("name="+name, func(t *testing.T) {
			repo := setupRepo(t)
			p := &Pipeline{Cfg: baseConfig(t, repo), Opts: Options{Repo: repo, Prompt: "change", Name: name}, Gate: &recordingGate{}, AgentFactory: passFactory(t)}
			if err := p.Execute(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !p.Run.InPlace || p.Run.Worktree != repo || p.Run.Branch != "main" {
				t.Fatalf("unexpected checkout: %+v", p.Run)
			}
			if got := gitOutput(t, repo, "branch", "--format=%(refname:short)"); got != "main" {
				t.Fatalf("extra branch: %s", got)
			}
			if got := gitOutput(t, repo, "show", "HEAD:feature.txt"); got != "ok" {
				t.Fatal(got)
			}
			run, err := artifact.Load(p.Run.Dir)
			if err != nil {
				t.Fatal(err)
			}
			// Switch away to ensure cleanup cannot delete the now-unchecked-out branch.
			gitRun(t, repo, "checkout", "-b", "other")
			if warn, err := Discard(run); warn != nil || err != nil {
				t.Fatalf("discard: %v / %v", warn, err)
			}
			if !worktree.BranchExists(repo, "main") {
				t.Fatal("discard deleted the user's branch")
			}
			if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCurrentLinkedCheckoutSurvivesFailureAndResume(t *testing.T) {
	repo := setupRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, repo, "worktree", "add", "-b", "linked", linked)
	factory := passFactory(t)
	p := &Pipeline{Cfg: baseConfig(t, linked), Opts: Options{Repo: linked, Prompt: "change"}, Gate: &recordingGate{}}
	p.AgentFactory = func(name string) (agent.Agent, error) {
		if name == "codex" {
			return fakeAgent{name, agent.Executor, func(context.Context, agent.Request) (*agent.Result, error) {
				if err := os.WriteFile(filepath.Join(linked, "partial.txt"), []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
				return nil, errors.New("temporary failure")
			}}, nil
		}
		return factory(name)
	}
	if err := p.Execute(context.Background()); err == nil {
		t.Fatal("wanted failure")
	}
	if got := gitOutput(t, linked, "diff", "--cached", "--name-only"); !strings.Contains(got, "partial.txt") {
		t.Fatalf("partial edits were not staged: %s", got)
	}
	run, err := artifact.Load(p.Run.Dir)
	if err != nil {
		t.Fatal(err)
	}
	resumed := &Pipeline{Cfg: p.Cfg, Run: run, Opts: p.Opts, Gate: &recordingGate{}, AgentFactory: factory}
	if err := resumed.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, linked, "show", "HEAD:partial.txt"); got != "keep" {
		t.Fatal(got)
	}
	gitRun(t, linked, "checkout", "-b", "different")
	if err := resumed.Execute(context.Background()); err == nil || !strings.Contains(err.Error(), "switch back") {
		t.Fatalf("resume on wrong branch: %v", err)
	}
}

func TestDetachedCheckoutRequiresNamedBranch(t *testing.T) {
	repo := setupRepo(t)
	gitRun(t, repo, "checkout", "--detach")
	p := &Pipeline{Cfg: baseConfig(t, repo), Opts: Options{Repo: repo, Prompt: "change"}, Gate: &recordingGate{}, AgentFactory: passFactory(t)}
	if err := p.Execute(context.Background()); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Fatalf("got %v", err)
	}
}

type retryRecordingGate struct {
	recordingGate
	steps []string
}

func (g *retryRecordingGate) RetryGate(_ context.Context, step string, _ error) (bool, error) {
	g.steps = append(g.steps, step)
	return len(g.steps) == 1, nil
}

func TestStageRetryKeepsEarlierStepsAndStagesBeforeReview(t *testing.T) {
	for _, failing := range []string{"claude", "codex", "opencode"} {
		t.Run(failing, func(t *testing.T) {
			repo := setupRepo(t)
			gate := &retryRecordingGate{}
			counts := map[string]int{}
			factory := passFactory(t)
			p := &Pipeline{Cfg: baseConfig(t, repo), Opts: Options{Repo: repo, Prompt: "change"}, Gate: gate}
			p.AgentFactory = func(name string) (agent.Agent, error) {
				base, err := factory(name)
				if err != nil {
					return nil, err
				}
				return fakeAgent{name, base.Kind(), func(ctx context.Context, req agent.Request) (*agent.Result, error) {
					counts[name]++
					if name == "opencode" {
						if got := gitOutput(t, req.Dir, "diff", "--cached", "--name-only"); !strings.Contains(got, "feature.txt") {
							t.Fatalf("review began before staging: %q", got)
						}
						if !strings.Contains(req.Prompt, "feature.txt") {
							t.Fatal("review diff lost staged changes")
						}
					}
					limit := 2 // planner/reviewer already retry invalid JSON once
					if name == "codex" {
						limit = 1
					}
					if name == failing && counts[name] <= limit {
						if name == "codex" {
							return nil, errors.New("temporary failure")
						}
						return &agent.Result{Structured: json.RawMessage(`{}`)}, nil
					}
					return base.Run(ctx, req)
				}}, nil
			}
			if err := p.Execute(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(gate.steps) != 1 {
				t.Fatalf("retry steps: %v", gate.steps)
			}
			for _, name := range []string{"claude", "codex", "opencode"} {
				if name != failing && counts[name] != 1 {
					t.Fatalf("unrelated stage %s repeated %d times", name, counts[name])
				}
			}
		})
	}
}

type checkpointPublishGate struct {
	recordingGate
	handler func() error
	pending bool
	err     error
}

func (g *checkpointPublishGate) SetPublishHandler(fn func() error) { g.handler = fn }
func (g *checkpointPublishGate) ProcessControls() {
	if g.pending {
		g.pending = false
		g.err = g.handler()
	}
}
func (g *checkpointPublishGate) CommitGate(context.Context, string, string) (ui.CommitDecision, error) {
	return ui.CommitStop, nil
}

func TestPublishAtCheckpointKeepsReviewDiff(t *testing.T) {
	repo := setupRepo(t)
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare")
	gitRun(t, repo, "remote", "add", "origin", remote)
	gate := &checkpointPublishGate{}
	factory := passFactory(t)
	p := &Pipeline{Cfg: baseConfig(t, repo), Opts: Options{Repo: repo, Prompt: "change"}, Gate: gate}
	p.AgentFactory = func(name string) (agent.Agent, error) {
		base, _ := factory(name)
		return fakeAgent{name, base.Kind(), func(ctx context.Context, req agent.Request) (*agent.Result, error) {
			if name == "opencode" {
				if !strings.Contains(req.Prompt, "+ok") {
					t.Fatal("manual publish erased reviewer diff")
				}
				if got := gitOutput(t, remote, "show", "main:feature.txt"); got != "ok" {
					t.Fatal(got)
				}
			}
			result, err := base.Run(ctx, req)
			if name == "codex" {
				gate.pending = true
			}
			return result, err
		}}, nil
	}
	if err := p.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gate.err != nil {
		t.Fatal(gate.err)
	}
	if !p.Run.Pushed || p.Run.Commit == "" {
		t.Fatalf("missing publish record: %+v", p.Run)
	}
	if data, _ := p.Run.Read("diff.patch"); !strings.Contains(string(data), "+ok") {
		t.Fatal("lost diff artifact")
	}
}

func TestPublishFailureCanRetryWithoutDuplicateCommit(t *testing.T) {
	repo := setupRepo(t)
	run, err := artifact.New(t.TempDir(), repo, "change")
	if err != nil {
		t.Fatal(err)
	}
	run.Worktree, run.Branch, run.InPlace = repo, "main", true
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PublishRun(run); err == nil {
		t.Fatal("expected missing origin failure")
	}
	commit := gitOutput(t, repo, "rev-parse", "HEAD")
	if run.Commit != commit || run.Pushed {
		t.Fatalf("commit not preserved: %+v", run)
	}
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare")
	gitRun(t, repo, "remote", "add", "origin", remote)
	if err := PublishRun(run); err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, remote, "rev-parse", "main"); got != commit || !run.Pushed {
		t.Fatalf("push retry: %s", got)
	}
}

type sameAgentGate struct{ recordingGate }

func (g *sameAgentGate) SelectAgent(context.Context, agent.Kind, string, []string, string, error) (string, error) {
	return "retry", nil
}

func TestRetrySameAgentPreservesModel(t *testing.T) {
	repo := setupRepo(t)
	cfg := baseConfig(t, repo)
	cfg.Models.Executor.Model = "chosen-model"
	calls := 0
	factory := passFactory(t)
	p := &Pipeline{Cfg: cfg, Opts: Options{Repo: repo, Prompt: "change"}, Gate: &sameAgentGate{}}
	p.AgentFactory = func(name string) (agent.Agent, error) {
		base, _ := factory(name)
		if name != "codex" {
			return base, nil
		}
		return fakeAgent{name, agent.Executor, func(ctx context.Context, req agent.Request) (*agent.Result, error) {
			calls++
			if req.Model != "chosen-model" {
				t.Fatalf("lost model: %s", req.Model)
			}
			if calls == 1 {
				return nil, errors.New("transient")
			}
			return base.Run(ctx, req)
		}}, nil
	}
	if err := p.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("executor calls: %d", calls)
	}
}

func TestUserCanRetryReviewFixesAfterIterationLimit(t *testing.T) {
	repo := setupRepo(t)
	cfg := baseConfig(t, repo)
	cfg.Loop.MaxIterations = 0
	gate := &retryRecordingGate{}
	factory := passFactory(t)
	reviews := 0
	p := &Pipeline{Cfg: cfg, Opts: Options{Repo: repo, Prompt: "change"}, Gate: gate}
	p.AgentFactory = func(name string) (agent.Agent, error) {
		base, _ := factory(name)
		if name != "opencode" {
			return base, nil
		}
		return fakeAgent{name, agent.Reviewer, func(ctx context.Context, req agent.Request) (*agent.Result, error) {
			reviews++
			if reviews == 1 {
				return &agent.Result{Structured: json.RawMessage(`{"verdict":"fail","summary":"needs a fix","issues":[{"severity":"blocker","description":"fix this"}]}`)}, nil
			}
			return base.Run(ctx, req)
		}}, nil
	}
	if err := p.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reviews != 2 || len(gate.steps) != 1 || gate.steps[0] != "review fixes" {
		t.Fatalf("reviews=%d steps=%v", reviews, gate.steps)
	}
}
