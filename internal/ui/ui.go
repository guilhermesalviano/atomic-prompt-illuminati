// Package ui defines the gate/progress interface the pipeline talks to. Two
// implementations exist: the Bubble Tea TUI and a plain text fallback.
package ui

import (
	"context"

	"github.com/guibs/atomic-prompt-illuminati/internal/agent"
	"github.com/guibs/atomic-prompt-illuminati/internal/contracts"
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
