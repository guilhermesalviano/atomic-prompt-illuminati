package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/guilhermesalviano/korchestrate/internal/agent"
	"github.com/guilhermesalviano/korchestrate/internal/config"
)

// TestMain keeps every pipeline test off the real opencode CLI: drafting fails,
// so commits use the fallback message unless a test installs its own drafter.
func TestMain(m *testing.M) {
	commitDrafter = fakeAgent{"opencode", agent.Reviewer, func(context.Context, agent.Request) (*agent.Result, error) {
		return nil, errors.New("no drafter in tests")
	}}
	os.Exit(m.Run())
}

func useDrafter(t *testing.T, fn func(context.Context, agent.Request) (*agent.Result, error)) {
	t.Helper()
	prev := commitDrafter
	commitDrafter = fakeAgent{"opencode", agent.Reviewer, fn}
	t.Cleanup(func() { commitDrafter = prev })
}

func TestCommitMessageUsesDraftGroundedInRequest(t *testing.T) {
	var got agent.Request
	useDrafter(t, func(_ context.Context, r agent.Request) (*agent.Result, error) {
		got = r
		raw, _ := json.Marshal(map[string]string{"message": "fix: author commits as the user\n\nDrop the api identity."})
		return &agent.Result{Structured: raw}, nil
	})
	msg := commitMessage(context.Background(), config.ModelSpec{Model: "m"}, "/wt", "make commits mine", "+diff", "fallback", "run-1")
	if msg != "fix: author commits as the user\n\nDrop the api identity.\n\nkor run run-1" {
		t.Fatalf("message = %q", msg)
	}
	if !strings.Contains(got.Prompt, "make commits mine") || !strings.Contains(got.Prompt, "+diff") {
		t.Fatalf("prompt lacks request or diff:\n%s", got.Prompt)
	}
	if got.Agent != "plan" || got.Model != "m" || got.Dir != "/wt" {
		t.Fatalf("request = %+v", got)
	}
}

func TestCommitMessageFallsBack(t *testing.T) {
	for name, fn := range map[string]func(context.Context, agent.Request) (*agent.Result, error){
		"error":   func(context.Context, agent.Request) (*agent.Result, error) { return nil, errors.New("boom") },
		"no json": func(context.Context, agent.Request) (*agent.Result, error) { return &agent.Result{}, nil },
		"blank": func(context.Context, agent.Request) (*agent.Result, error) {
			return &agent.Result{Structured: json.RawMessage(`{"message":"  "}`)}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			useDrafter(t, fn)
			if msg := commitMessage(context.Background(), config.ModelSpec{}, "", "req", "+d", "fallback", "r"); msg != "fallback\n\nkor run r" {
				t.Fatalf("message = %q", msg)
			}
		})
	}
}

func TestCommitSpecPrefersOpencodeReviewer(t *testing.T) {
	cfg := config.Default()
	cfg.Models.Reviewer = config.ModelSpec{Agent: "opencode", Model: "custom", Variant: "high"}
	if s := commitSpec(cfg); s.Model != "custom" || s.Variant != "high" {
		t.Fatalf("spec = %+v", s)
	}
	cfg.Models.Reviewer = config.ModelSpec{Agent: "claude", Model: "opus"}
	if s := commitSpec(cfg); s.Model != config.DefaultModelFor("opencode") {
		t.Fatalf("spec = %+v", s)
	}
}
