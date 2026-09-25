// Package ui defines the gate/progress interface the pipeline talks to. Two
// implementations exist: the Bubble Tea TUI and a plain text fallback.
package ui

import (
	"context"
	"fmt"

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
