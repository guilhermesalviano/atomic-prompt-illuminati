// Package ui defines the gate/progress interface the pipeline talks to. Two
// implementations exist: the Bubble Tea TUI and a plain text fallback.
package ui

import (
	"context"
	"fmt"

	"github.com/guilhermesalviano/korchestrate/internal/agent"
	"github.com/guilhermesalviano/korchestrate/internal/contracts"
)

// Decision is the user's answer at a gate.
type Decision int

const (
	Approve Decision = iota
	Reject
	Fix
)

func (d Decision) String() string {
	switch d {
	case Approve:
		return "approve"
	case Reject:
		return "reject"
	case Fix:
		return "fix"
	default:
		return "unknown"
	}
}

// WorktreeDecision is the user's answer when a new run's default branch name
// already exists, usually because a previous run created it.
type WorktreeDecision int

const (
	// WorktreeReuse adopts the existing worktree and branch for the new run.
	WorktreeReuse WorktreeDecision = iota
	// WorktreeCreate leaves the existing worktree alone and gives the run a
	// freshly created branch and worktree.
	WorktreeCreate
)

func (d WorktreeDecision) String() string {
	switch d {
	case WorktreeReuse:
		return "reuse"
	case WorktreeCreate:
		return "create"
	default:
		return "unknown"
	}
}

// CommitDecision is the user's answer at the publish gate that follows a passed
// review, once every change has been staged.
type CommitDecision int

const (
	// CommitStop leaves the changes staged and does not commit.
	CommitStop CommitDecision = iota
	// CommitOnly commits the staged changes on the run branch.
	CommitOnly
	// CommitAndPush commits and pushes the branch to origin.
	CommitAndPush
)

func (d CommitDecision) String() string {
	switch d {
	case CommitStop:
		return "stop"
	case CommitOnly:
		return "commit"
	case CommitAndPush:
		return "commit+push"
	default:
		return "unknown"
	}
}

// Gate presents progress and decision points to the user.
type Gate interface {
	// Stage announces the start of a stage or a status change.
	Stage(kind agent.Kind, msg string)
	// Line forwards a streamed agent output line.
	Line(ev agent.Event)
	// Info prints a general informative message.
	Info(msg string)
	// PlanGate asks the user to approve or reject the plan.
	PlanGate(ctx context.Context, plan *contracts.Plan, diff string) (Decision, error)
	// ReviewGate asks the user to approve, reject or request a fix.
	ReviewGate(ctx context.Context, review *contracts.Review, diff string) (Decision, error)
	// CommitGate presents the staged changes and asks whether to commit,
	// commit and push, or stop with the changes staged.
	CommitGate(ctx context.Context, branch, worktree string) (CommitDecision, error)
	// WorktreeGate asks whether a new run should reuse an existing
	// worktree/branch or create a new one. branch is the already-taken name.
	WorktreeGate(ctx context.Context, branch string) (WorktreeDecision, error)
	// SelectAgent asks the user to pick a replacement adapter after `failed`
	// could not run a stage. options lists the adapters still untried and
	// preferred hints at the configured fallback. It returns "" to give up.
	SelectAgent(ctx context.Context, kind agent.Kind, failed string, options []string, preferred string, cause error) (string, error)
	// Close releases terminal resources.
	Close()
}

// AutoApprove wraps a Gate and answers every gate with Approve, printing what
// would have been asked. Used by --yes.
type AutoApprove struct{ Inner Gate }

func (a AutoApprove) Stage(k agent.Kind, msg string) { a.Inner.Stage(k, msg) }
func (a AutoApprove) Line(ev agent.Event)            { a.Inner.Line(ev) }
func (a AutoApprove) Info(msg string)                { a.Inner.Info(msg) }
func (a AutoApprove) Close()                         { a.Inner.Close() }

func (a AutoApprove) PlanGate(_ context.Context, plan *contracts.Plan, diff string) (Decision, error) {
	a.Inner.Info("[--yes] auto-approving plan")
	a.Inner.Info(RenderPlan(plan))
	return Approve, nil
}

func (a AutoApprove) ReviewGate(_ context.Context, review *contracts.Review, diff string) (Decision, error) {
	a.Inner.Info("[--yes] auto-approving review")
	a.Inner.Info(RenderReview(review))
	return Approve, nil
}

func (a AutoApprove) CommitGate(_ context.Context, branch, _ string) (CommitDecision, error) {
	a.Inner.Info("[--yes] committing staged changes on " + branch + " (no push)")
	return CommitOnly, nil
}

func (a AutoApprove) WorktreeGate(_ context.Context, branch string) (WorktreeDecision, error) {
	a.Inner.Info("[--yes] branch " + branch + " already exists; creating a new worktree")
	return WorktreeCreate, nil
}

func (a AutoApprove) SelectAgent(_ context.Context, kind agent.Kind, failed string, options []string, preferred string, cause error) (string, error) {
	a.Inner.Info(fmt.Sprintf("[--yes] %s agent %q failed: %v", kind, failed, cause))
	pick := ""
	for _, o := range options {
		if o == preferred {
			pick = o
			break
		}
	}
	if pick == "" && len(options) > 0 {
		pick = options[0]
	}
	if pick == "" {
		a.Inner.Info("[--yes] no fallback agent available; aborting")
		return "", nil
	}
	a.Inner.Info("[--yes] falling back to " + pick)
	return pick, nil
}
