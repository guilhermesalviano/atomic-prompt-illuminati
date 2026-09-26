// Package pipeline drives the plan → execute → review state machine across the
// three agent CLIs, managing the run checkout and the human gates.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/guilhermesalviano/korchestrate/internal/agent"
	"github.com/guilhermesalviano/korchestrate/internal/artifact"
	"github.com/guilhermesalviano/korchestrate/internal/config"
	"github.com/guilhermesalviano/korchestrate/internal/contracts"
	"github.com/guilhermesalviano/korchestrate/internal/preflight"
	"github.com/guilhermesalviano/korchestrate/internal/ui"
	"github.com/guilhermesalviano/korchestrate/internal/worktree"
)

// Options controls a single pipeline run.
type Options struct {
	Repo   string
	Prompt string
	// Plan is a pre-defined plan to execute instead of running the planner
	// stage. When set, planning is skipped and the plan goes straight to the
	// plan gate.
	Plan *contracts.Plan
	// Name selects the branch for a new run. Empty or the current branch name
	// uses the current checkout directly. Other names create an isolated
	// worktree; existing names offer reuse or a suffixed new branch.
	// Resumed runs keep their recorded checkout.
	Name         string
	AllowDirty   bool
	KeepWorktree bool
	// Apply leaves the worktree in place and, when the remote supports it, is
	// where a PR step would run. The branch is always committed on success.
	Apply bool
}

// RunObserver is optionally implemented by a Gate that wants a snapshot of the
// run record as soon as it exists and whenever its accounting changes.
type RunObserver interface {
	RunUpdated(run artifact.Run)
}

// Pipeline is a single orchestrator run.
type Pipeline struct {
	Cfg  *config.Config
	Opts Options
	Gate ui.Gate

	// AgentFactory overrides adapter resolution (used by tests).
	AgentFactory func(name string) (agent.Agent, error)

	Run          *artifact.Run
	worktreePath string
	branch       string
	// reused records that the run adopted a pre-existing worktree/branch the
	// user chose to keep; such worktrees are never torn down on failure.
	reused bool

	// overrides remembers adapters the user selected after a stage failure.
	overrides map[agent.Kind]agentChoice
}

func (p *Pipeline) agentFor(name string) (agent.Agent, error) {
	if p.AgentFactory != nil {
		return p.AgentFactory(name)
	}
	return adapterFor(name)
}

// Execute runs the full pipeline. On failure the worktree is cleaned up unless
// KeepWorktree is set; on success the worktree and branch are retained.
func (p *Pipeline) Execute(ctx context.Context) (err error) {
	checks := preflight.Checks(p.Opts.Repo,
		p.Cfg.Models.Planner.Agent, p.Cfg.Models.Executor.Agent, p.Cfg.Models.Reviewer.Agent)
	for _, c := range checks {
		status := "ok"
		if !c.OK {
			status = "warn"
			if c.Fatal {
				status = "FAIL"
			}
		}
		p.Gate.Info(fmt.Sprintf("preflight %-4s %-14s %s", status, c.Name, c.Detail))
	}
	if err := preflight.Fatal(checks); err != nil {
		return err
	}

	clean, err := worktree.IsClean(p.Opts.Repo)
	if err != nil {
		return err
	}
	if !clean && !p.Opts.AllowDirty {
		return fmt.Errorf("repository %s has uncommitted changes; commit them or pass --allow-dirty", p.Opts.Repo)
	}

	run := p.Run
	created := false
	if run == nil {
		var nerr error
		run, nerr = artifact.New(p.Cfg.ArtifactsDir, p.Opts.Repo, p.Opts.Prompt)
		if nerr != nil {
			return nerr
		}
		created = true
	}
	p.Run = run
	if created {
		if name := strings.TrimSpace(p.Opts.Name); name != "" {
			if err := worktree.ValidBranch(p.Opts.Repo, name); err != nil {
				return err
			}
		}
	}
	p.branch = run.Branch
	p.worktreePath = run.Worktree
	if run.InPlace {
		current, err := worktree.CurrentBranch(run.Worktree)
		if err != nil {
			return err
		}
		if current != run.Branch {
			return fmt.Errorf("run uses branch %q, but its checkout is on %q; switch back before resuming", run.Branch, current)
		}
	}
	if p.branch == "" {
		current, err := worktree.CurrentBranch(p.Opts.Repo)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(p.Opts.Name)
		if name == "" || name == current {
			if current == "" || current == "HEAD" {
				return fmt.Errorf("checkout has a detached HEAD; choose a branch with --name")
			}
			p.branch, p.worktreePath, run.InPlace = current, p.Opts.Repo, true
		} else {
			var derr error
			p.branch, p.reused, derr = p.deriveBranch(ctx, name)
			if derr != nil {
				return derr
			}
		}
	}
	if p.worktreePath == "" {
		p.worktreePath = run.Path("worktree")
	}
	run.Branch = p.branch
	run.Worktree = p.worktreePath
	if err := run.Save(); err != nil {
		return err
	}
	if cfgYAML, cerr := p.Cfg.MarshalYAML(); cerr == nil {
		_ = run.Write("config.resolved.yaml", cfgYAML)
	}
	p.Gate.Info("run " + run.ID + " -> " + run.Dir)
	p.notify()

	defer func() {
		if err != nil {
			// A user rejection already marked the run aborted; keep that state
			// instead of collapsing it into a generic failure.
			if run.State == artifact.StateAborted {
				run.Error = err.Error()
				_ = run.Save()
			} else {
				_ = run.Fail(err)
			}
			p.cleanup()
		}
	}()

	if created {
		if run.InPlace {
			if err := run.SetState(artifact.StateWorktree); err != nil {
				return err
			}
			p.Gate.Info("using current checkout " + p.worktreePath + " on " + p.branch)
		} else if p.reused {
			// The user chose to keep the existing worktree/branch: adopt it
			// where it lives instead of creating a fresh one.
			if err := p.adoptWorktree(); err != nil {
				return err
			}
			run.Worktree = p.worktreePath
			_ = run.Save()
			if err := run.SetState(artifact.StateWorktree); err != nil {
				return err
			}
			p.Gate.Info("reusing worktree " + p.worktreePath + " on " + p.branch)
		} else {
			if worktree.BranchExists(p.Opts.Repo, p.branch) {
				return fmt.Errorf("branch %q already exists; choose another worktree name", p.branch)
			}
			base, err := worktree.Head(p.Opts.Repo)
			if err != nil {
				return err
			}
			if err := worktree.Add(p.Opts.Repo, p.worktreePath, p.branch, base); err != nil {
				return err
			}
			if err := run.SetState(artifact.StateWorktree); err != nil {
				return err
			}
			p.Gate.Info("worktree " + p.worktreePath + " on " + p.branch)
		}
	}
	unlock, err := worktree.LockCheckout(p.worktreePath)
	if err != nil {
		// A checkout another run is using must not be cleaned up.
		p.reused = true
		return err
	}
	defer unlock()
	if gate, ok := p.Gate.(publishControls); ok {
		gate.SetPublishHandler(func() error {
			// Once the user publishes, failure cleanup must preserve the commit.
			p.Opts.KeepWorktree = true
			err := publishRun(run)
			p.notify()
			return err
		})
	}

	// --- PLAN -------------------------------------------------------------
	var plan *contracts.Plan
	if p.Opts.Plan != nil {
		plan = p.Opts.Plan
		p.Gate.Info("using provided plan; skipping planner")
	} else {
		plan, err = retryStep(ctx, p, "planner", func() (*contracts.Plan, error) { return p.plan(ctx) })
		if err != nil {
			return err
		}
	}
	p.processControls()
	planJSON, _ := json.MarshalIndent(plan, "", "  ")
	_ = run.Write("plan.json", planJSON)
	if err := run.SetState(artifact.StateGatePlan); err != nil {
		return err
	}
	if p.Cfg.Gates.AfterPlan {
		p.Gate.Stage(agent.Planner, "awaiting plan approval")
		decision, gerr := p.Gate.PlanGate(ctx, plan, "")
		if gerr != nil {
			return gerr
		}
		if decision == ui.Reject {
			run.State = artifact.StateAborted
			_ = run.Save()
			return fmt.Errorf("plan rejected by user")
		}
	}

	// --- EXECUTE / REVIEW LOOP -------------------------------------------
	var fix string
	passed := false
	for iter := 0; iter <= p.Cfg.Loop.MaxIterations; iter++ {
		run.Iteration = iter
		var nextFix string
		passed, nextFix, err = p.cycle(ctx, plan, iter, fix)
		if err != nil {
			return err
		}
		if passed {
			break
		}
		fix = nextFix
	}
	if !passed {
		return fmt.Errorf("review did not pass within %d iteration(s)", p.Cfg.Loop.MaxIterations+1)
	}

	// --- FINALIZE ---------------------------------------------------------
	// The review passed: stage every change so the worktree is ready to
	// publish, then let the user decide whether to commit and/or push.
	if err := worktree.Stage(p.worktreePath); err != nil {
		return err
	}
	if err := run.SetState(artifact.StatePublishing); err != nil {
		return err
	}
	decision, err := p.Gate.CommitGate(ctx, p.branch, p.worktreePath)
	if err != nil {
		return err
	}
	if decision == ui.CommitStop {
		p.Gate.Info("changes staged on " + p.branch + "; not committed")
		if err := run.SetState(artifact.StateDone); err != nil {
			return err
		}
		p.Gate.Info("worktree retained at " + p.worktreePath)
		return nil
	}
	if err := run.SetState(artifact.StateCommitting); err != nil {
		return err
	}
	commit, err := retryStep(ctx, p, "commit", func() (string, error) {
		return worktree.Commit(p.worktreePath, fmt.Sprintf("%s\n\napi run %s", plan.Summary, run.ID))
	})
	if err != nil {
		return err
	}
	if commit != "" {
		run.Commit, run.Pushed = commit, false
	}
	p.Opts.KeepWorktree = true
	if err := run.Save(); err != nil {
		return err
	}
	if decision == ui.CommitAndPush {
		// A failed push must not be treated as a failed run: that would tear
		// down the worktree and discard the commit that just succeeded.
		if _, err := retryStep(ctx, p, "push", func() (bool, error) {
			return true, worktree.Push(p.worktreePath, p.branch)
		}); err != nil {
			p.Gate.Info("warning: push failed: " + err.Error())
			p.Gate.Info("commit is safe on " + p.branch + "; push it manually")
		} else {
			run.Pushed = true
			p.Gate.Info("pushed " + p.branch + " to origin")
		}
	}
	if err := run.SetState(artifact.StateDone); err != nil {
		return err
	}
	p.Gate.Info("done; branch " + p.branch + " at " + shortSHA(commit))
	p.Gate.Info("worktree retained at " + p.worktreePath)
	return nil
}

// cycle runs one executor+reviewer pass. It returns whether the review passed
// and, when it did not, the fix instructions for the next iteration.
func (p *Pipeline) cycle(ctx context.Context, plan *contracts.Plan, iter int, fix string) (bool, string, error) {
	report, err := retryStep(ctx, p, "executor", func() (*contracts.ExecReport, error) {
		report, execErr := p.execute(ctx, plan, iter, fix)
		if stageErr := worktree.Stage(p.worktreePath); stageErr != nil && execErr == nil {
			return report, stageErr
		}
		return report, execErr
	})
	if err != nil {
		return false, "", err
	}
	if report != nil {
		data, _ := json.MarshalIndent(report, "", "  ")
		_ = p.Run.Write(fmt.Sprintf("executor.report.%d.json", iter), data)
	}

	diff, err := worktree.Diff(p.worktreePath)
	if err != nil {
		return false, "", err
	}
	_ = p.Run.Write(fmt.Sprintf("diff.%d.patch", iter), []byte(diff))
	_ = p.Run.Write("diff.patch", []byte(diff))
	if strings.TrimSpace(diff) == "" {
		p.Gate.Info("executor produced no changes")
	}
	p.Gate.Info("executor changes staged on " + p.branch)
	p.processControls()

	review, err := retryStep(ctx, p, "reviewer", func() (*contracts.Review, error) { return p.review(ctx, plan, diff, iter) })
	if err != nil {
		return false, "", err
	}
	p.processControls()
	data, _ := json.MarshalIndent(review, "", "  ")
	_ = p.Run.Write(fmt.Sprintf("review.%d.json", iter), data)
	_ = p.Run.Write("review.json", data)
	if err := p.Run.SetState(artifact.StateGateReview); err != nil {
		return false, "", err
	}

	if review.Pass() {
		p.Gate.Stage(agent.Reviewer, "review passed")
		if p.Cfg.Gates.AfterReview {
			decision, err := p.Gate.ReviewGate(ctx, review, diff)
			if err != nil {
				return false, "", err
			}
			switch decision {
			case ui.Reject:
				p.Run.State = artifact.StateAborted
				_ = p.Run.Save()
				return false, "", fmt.Errorf("review rejected by user")
			case ui.Fix:
				return false, p.fixInstruction(review), nil
			}
		}
		return true, "", nil
	}

	p.Gate.Stage(agent.Reviewer, "review failed")
	if iter >= p.Cfg.Loop.MaxIterations {
		return false, "", fmt.Errorf("review failed after %d iteration(s): %s", iter+1, review.Summary)
	}
	if p.Cfg.Gates.AfterReview {
		decision, err := p.Gate.ReviewGate(ctx, review, diff)
		if err != nil {
			return false, "", err
		}
		if decision == ui.Reject {
			p.Run.State = artifact.StateAborted
			_ = p.Run.Save()
			return false, "", fmt.Errorf("review rejected by user")
		}
	}
	return false, p.fixInstruction(review), nil
}

func (p *Pipeline) fixInstruction(review *contracts.Review) string {
	if lines := review.BlockerLines(); lines != "" {
		return lines
	}
	return review.Summary
}

// deriveBranch offers reuse for an existing named branch or finds a free suffix.
func (p *Pipeline) deriveBranch(ctx context.Context, base string) (branch string, reuse bool, err error) {
	for i := 1; ; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		if !worktree.BranchExists(p.Opts.Repo, candidate) {
			return candidate, false, nil
		}
		d, gerr := p.Gate.WorktreeGate(ctx, candidate)
		if gerr != nil {
			return "", false, gerr
		}
		if d == ui.WorktreeReuse {
			return candidate, true, nil
		}
	}
}

// adoptWorktree points the run at the existing branch's worktree, creating a
// new checkout when the branch has none (or its registration is stale).
func (p *Pipeline) adoptWorktree() error {
	if wt := worktree.ForBranch(p.Opts.Repo, p.branch); wt != "" {
		if _, err := os.Stat(wt); err == nil {
			p.worktreePath = wt
			return nil
		}
		_ = worktree.Prune(p.Opts.Repo)
	}
	return worktree.Checkout(p.Opts.Repo, p.worktreePath, p.branch)
}

// cleanup removes the worktree and branch after a failed or aborted run. A
// worktree the run reused existed before the run started, so it is always
// kept, whatever KeepWorktree says.
func (p *Pipeline) cleanup() {
	if p.Run != nil && p.Run.InPlace {
		p.Gate.Info("keeping current checkout " + p.worktreePath + " (branch " + p.branch + ")")
		return
	}
	if p.worktreePath == "" || p.branch == "" {
		return
	}
	if p.reused {
		p.Gate.Info("keeping reused worktree " + p.worktreePath + " (branch " + p.branch + ")")
		return
	}
	if p.Opts.KeepWorktree {
		p.Gate.Info("keeping worktree " + p.worktreePath + " (branch " + p.branch + ")")
		return
	}
	if err := worktree.Remove(p.Opts.Repo, p.worktreePath); err == nil {
		_ = worktree.DeleteBranch(p.Opts.Repo, p.branch)
		p.Gate.Info("removed worktree and branch " + p.branch)
	}
}

// notify hands the gate a copy of the run record when it asks for one.
func (p *Pipeline) notify() {
	if o, ok := p.Gate.(RunObserver); ok && p.Run != nil {
		o.RunUpdated(*p.Run)
	}
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
