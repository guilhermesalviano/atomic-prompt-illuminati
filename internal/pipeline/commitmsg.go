package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/guilhermesalviano/korchestrate/internal/agent"
	"github.com/guilhermesalviano/korchestrate/internal/artifact"
	"github.com/guilhermesalviano/korchestrate/internal/config"
	"gopkg.in/yaml.v3"
)

// commitDrafter writes commit messages. Tests replace it so no real agent runs.
var commitDrafter agent.Agent = agent.OpenCode{}

const commitTimeout = 3 * time.Minute

const commitSystem = `You write git commit messages. The user's original request is the main
reference for why the change exists; the diff shows what changed. Reply with ONLY
a JSON object {"message": "..."}: a conventional-commit subject line (type: summary,
imperative, at most 72 characters), then a blank line and a short body when the
change needs explaining. Do not modify any files.`

// commitSpec picks the opencode model for drafting: the configured reviewer
// when it already runs on opencode, otherwise opencode's built-in default.
func commitSpec(cfg *config.Config) config.ModelSpec {
	if cfg != nil && strings.EqualFold(strings.TrimSpace(cfg.Models.Reviewer.Agent), "opencode") && cfg.Models.Reviewer.Model != "" {
		spec := cfg.Models.Reviewer
		return config.ModelSpec{Model: spec.Model, Variant: spec.Variant}
	}
	return config.ModelSpec{Model: config.DefaultModelFor("opencode")}
}

// runCommitSpec reads the reviewer spec a finished run was started with.
func runCommitSpec(run *artifact.Run) config.ModelSpec {
	cfg := config.Default()
	if data, err := run.Read("config.resolved.yaml"); err == nil {
		_ = yaml.Unmarshal(data, cfg)
	}
	return commitSpec(cfg)
}

// commitMessage asks opencode for a message grounded in the user's original
// request and the diff. Any failure falls back to fallback, so drafting can
// never block a commit. The run ID trailer is always appended.
func commitMessage(ctx context.Context, spec config.ModelSpec, dir, request, diff, fallback, runID string) string {
	trailer := "\n\nkor run " + runID
	if strings.TrimSpace(diff) == "" {
		return fallback + trailer
	}
	ctx, cancel := context.WithTimeout(ctx, commitTimeout)
	defer cancel()
	prompt := fmt.Sprintf("%s\n\n## Original request\n%s\n\n## Diff\n```diff\n%s\n```\n", commitSystem, request, capDiff(diff))
	res, err := commitDrafter.Run(ctx, agent.Request{
		Dir:     dir,
		Prompt:  prompt,
		Model:   spec.Model,
		Variant: spec.Variant,
		Agent:   "plan", // read-only: drafting must not touch the worktree
		Timeout: commitTimeout,
	})
	if err != nil || res == nil || len(res.Structured) == 0 {
		return fallback + trailer
	}
	var out struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(res.Structured, &out) != nil || strings.TrimSpace(out.Message) == "" {
		return fallback + trailer
	}
	return strings.TrimSpace(out.Message) + trailer
}
