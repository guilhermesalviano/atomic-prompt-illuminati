package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/guilhermesalviano/korchestrate/internal/artifact"
	"github.com/guilhermesalviano/korchestrate/internal/pipeline"
)

type publishRequest struct{ reply chan error }
type publishResultMsg struct {
	entry *Entry
	run   *artifact.Run
	err   error
}

func (s *Session) SetPublishHandler(fn func() error) { s.publisher = fn }

// ProcessControls runs between agent invocations on the pipeline goroutine.
func (s *Session) ProcessControls() {
	for {
		select {
		case req := <-s.publish:
			s.publishNow(req)
		default:
			return
		}
	}
}

func (s *Session) publishNow(req publishRequest) {
	if s.publisher == nil {
		req.reply <- fmt.Errorf("checkout is not ready yet; press p again once the run starts")
		return
	}
	req.reply <- s.publisher()
}

// Gates service publish requests on the pipeline goroutine, without racing
// agent writes, other Git operations, or run saves.
func awaitSession[T any](ctx context.Context, s *Session, reply <-chan T) (T, error) {
	for {
		select {
		case value := <-reply:
			return value, nil
		case req := <-s.publish:
			s.publishNow(req)
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
}

func (s *Session) RetryGate(ctx context.Context, step string, cause error) (bool, error) {
	req := &gateReq{kind: gateRetry, step: step, cause: cause, retryReply: make(chan bool, 1)}
	s.app.send(gateEventMsg{entry: s.entry, req: req})
	return awaitSession(ctx, s, (<-chan bool)(req.retryReply))
}

func (a *App) answerRetry(again bool) {
	e := a.current()
	if e == nil || e.Gate == nil || e.Gate.kind != gateRetry || e.Gate.retryReply == nil {
		return
	}
	e.Gate.retryReply <- again
	e.Gate = nil
	e.touch()
}

func (a *App) requestPublish() tea.Cmd {
	e := a.current()
	if e == nil || e.Run == nil || e.Run.Worktree == "" {
		a.notice = "No checkout ready to publish."
		return nil
	}
	if e.publishing || e.deleting {
		a.notice = "This run already has an operation in progress."
		return nil
	}
	if e.Live {
		if e.Session == nil || e.Session.publish == nil {
			a.notice = "Publish controls are not connected to this run."
			return nil
		}
		req := publishRequest{reply: make(chan error, 1)}
		select {
		case e.Session.publish <- req:
		default:
			a.notice = "Publish already queued."
			return nil
		}
		e.publishing = true
		a.notice = "Commit + push queued; runs when the active step yields."
		session := e.Session
		return func() tea.Msg {
			select {
			case err := <-req.reply:
				return publishResultMsg{entry: e, err: err}
			case <-session.done:
				select {
				case err := <-req.reply:
					return publishResultMsg{entry: e, err: err}
				default:
					return publishResultMsg{entry: e, err: fmt.Errorf("run finished before publishing; press p to publish now")}
				}
			}
		}
	}
	e.publishing = true
	run := *e.Run
	a.notice = "Committing and pushing " + run.Branch + "…"
	return func() tea.Msg {
		err := pipeline.PublishRun(&run)
		return publishResultMsg{entry: e, run: &run, err: err}
	}
}
