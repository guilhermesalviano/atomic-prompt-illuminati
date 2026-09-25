package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/guibs/atomic-prompt-illuminati/internal/agent"
	"github.com/guibs/atomic-prompt-illuminati/internal/contracts"
)

// Plain is a line-oriented Gate used when stdin is not a terminal or the TUI is
// disabled. It never streams full agent output; it prints status lines only.
type Plain struct {
	in          *bufio.Reader
	out         io.Writer
	showAgent   bool
	autoApprove bool
}

// NewPlain builds a plain gate reading from stdin.
func NewPlain(autoApprove bool) *Plain {
	return &Plain{in: bufio.NewReader(os.Stdin), out: os.Stdout, autoApprove: autoApprove}
}

// NewPlainWith wires a custom reader/writer (used in tests).
func NewPlainWith(in io.Reader, out io.Writer, autoApprove bool) *Plain {
	return &Plain{in: bufio.NewReader(in), out: out, autoApprove: autoApprove}
}

func (p *Plain) Stage(k agent.Kind, msg string) {
	fmt.Fprintf(p.out, "[%s] %s\n", k, msg)
}

func (p *Plain) Line(ev agent.Event) {
	if ev.Stream == "stderr" && strings.TrimSpace(ev.Line) != "" {
		fmt.Fprintf(p.out, "  %s\n", ev.Line)
	}
}

func (p *Plain) Info(msg string) { fmt.Fprintln(p.out, msg) }

func (p *Plain) Close() {}

func (p *Plain) PlanGate(_ context.Context, plan *contracts.Plan, _ string) (Decision, error) {
	fmt.Fprintln(p.out, "\n=== PLAN ===")
	fmt.Fprintln(p.out, RenderPlan(plan))
	if p.autoApprove {
		fmt.Fprintln(p.out, "[--yes] auto-approving plan")
		return Approve, nil
	}
	return p.ask("Approve plan? [a]pprove/[r]eject: ", map[string]Decision{
		"a": Approve, "": Approve, "r": Reject,
	})
}

func (p *Plain) ReviewGate(_ context.Context, review *contracts.Review, diff string) (Decision, error) {
	fmt.Fprintln(p.out, "\n=== REVIEW ===")
	fmt.Fprintln(p.out, RenderReview(review))
	if diff != "" {
		fmt.Fprintf(p.out, "\nDiff: %d bytes (%s)\n", len(diff), "see diff.patch artifact")
	}
	if p.autoApprove {
		fmt.Fprintln(p.out, "[--yes] auto-approving review")
		return Approve, nil
	}
	opts := map[string]Decision{"a": Approve, "": Approve, "f": Fix, "r": Reject}
	if review.Pass() {
		return p.ask("Apply review? [a]pprove/[f]ix/[r]eject: ", opts)
	}
	return p.ask("Review failed. [f]ix/[r]eject: ", map[string]Decision{"f": Fix, "": Fix, "r": Reject, "a": Approve})
}

func (p *Plain) CommitGate(_ context.Context, branch, worktree string) (CommitDecision, error) {
	fmt.Fprintf(p.out, "\n=== PUBLISH ===\nChanges staged on %s (%s)\n", branch, worktree)
	if p.autoApprove {
		fmt.Fprintln(p.out, "[--yes] committing staged changes (no push)")
		return CommitOnly, nil
	}
	return p.askCommit()
}

func (p *Plain) askCommit() (CommitDecision, error) {
	opts := map[string]CommitDecision{
		"c": CommitOnly, "": CommitOnly,
		"p": CommitAndPush,
		"s": CommitStop,
	}
	for {
		fmt.Fprint(p.out, "Publish staged changes? [c]ommit/[p]ush/[s]kip: ")
		line, err := p.in.ReadString('\n')
		if err != nil && line == "" {
			if err == io.EOF {
				return CommitStop, nil
			}
			return CommitStop, err
		}
		key := strings.ToLower(strings.TrimSpace(line))
		if d, ok := opts[key]; ok {
			return d, nil
		}
		fmt.Fprintln(p.out, "unrecognized input")
	}
}

func (p *Plain) SelectAgent(_ context.Context, kind agent.Kind, failed string, options []string, preferred string, cause error) (string, error) {
	fmt.Fprintf(p.out, "\n=== AGENT FAILURE (%s) ===\n", kind)
	fmt.Fprintf(p.out, "agent %q could not run: %v\n", failed, cause)
	if len(options) == 0 {
		fmt.Fprintln(p.out, "no alternative agents available; aborting")
		return "", nil
	}
	if p.autoApprove {
		pick := options[0]
		for _, o := range options {
			if o == preferred {
				pick = o
				break
			}
		}
		fmt.Fprintf(p.out, "[--yes] falling back to %s\n", pick)
		return pick, nil
	}
	for i, o := range options {
		mark := " "
		if o == preferred {
			mark = "*"
		}
		fmt.Fprintf(p.out, "  %d%s %s\n", i+1, mark, o)
	}
	for {
		fmt.Fprintf(p.out, "Choose an agent [1-%d, name, or enter to abort]: ", len(options))
		line, err := p.in.ReadString('\n')
		if err != nil && line == "" {
			if err == io.EOF {
				return "", nil
			}
			return "", err
		}
		key := strings.ToLower(strings.TrimSpace(line))
		if key == "" {
			return "", nil
		}
		for i, o := range options {
			if key == o || key == fmt.Sprint(i+1) {
				return o, nil
			}
		}
		fmt.Fprintln(p.out, "unrecognized input")
	}
}

func (p *Plain) ask(prompt string, opts map[string]Decision) (Decision, error) {
	for {
		fmt.Fprint(p.out, prompt)
		line, err := p.in.ReadString('\n')
		if err != nil && line == "" {
			if err == io.EOF {
				return Reject, nil
			}
			return Reject, err
		}
		key := strings.ToLower(strings.TrimSpace(line))
		if d, ok := opts[key]; ok {
			return d, nil
		}
		fmt.Fprintln(p.out, "unrecognized input")
	}
}
