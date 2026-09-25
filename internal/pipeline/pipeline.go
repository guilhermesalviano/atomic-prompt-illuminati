// Package pipeline drives the plan → execute → review state machine across the
// three agent CLIs, enforcing worktree isolation and the human gates.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/guibs/atomic-prompt-illuminati/internal/agent"
	"github.com/guibs/atomic-prompt-illuminati/internal/artifact"
	"github.com/guibs/atomic-prompt-illuminati/internal/config"
	"github.com/guibs/atomic-prompt-illuminati/internal/contracts"
	"github.com/guibs/atomic-prompt-illuminati/internal/preflight"
	"github.com/guibs/atomic-prompt-illuminati/internal/ui"
	"github.com/guibs/atomic-prompt-illuminati/internal/worktree"
)

// Options controls a single pipeline run.
type Options struct {
	Repo         string
	Prompt       string
	AllowDirty   bool
	KeepWorktree bool
	// Apply leaves the worktree in place and, when the remote supports it, is
	// where a PR step would run. The branch is always committed on success.
	Apply bool
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
	checks := preflight.Checks(p.Opts.Repo)
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
	p.branch = run.Branch
	p.worktreePath = run.Worktree
	if p.branch == "" {
		p.branch = "api/" + run.ID
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

	defer func() {
		if err != nil {
			_ = run.Fail(err)
			p.cleanup()
		}
	}()

	if created {
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

	// --- PLAN -------------------------------------------------------------
	plan, err := p.plan(ctx)
	if err != nil {
		return err
	}
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
	if err := run.SetState(artifact.StateCommitting); err != nil {
		return err
	}
	commit, err := worktree.Commit(p.worktreePath, fmt.Sprintf("%s\n\napi run %s", plan.Summary, run.ID))
	if err != nil {
		return err
	}
	run.Commit = commit
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
	report, err := p.execute(ctx, plan, iter, fix)
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

	review, err := p.review(ctx, plan, diff, iter)
	if err != nil {
		return false, "", err
	}
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

// cleanup removes the worktree and branch after a failed or aborted run.
func (p *Pipeline) cleanup() {
	if p.worktreePath == "" || p.branch == "" {
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

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
