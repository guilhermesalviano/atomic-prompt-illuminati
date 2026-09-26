package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/guilhermesalviano/korchestrate/internal/config"
	"github.com/guilhermesalviano/korchestrate/internal/models"
)

func TestApplyChoices(t *testing.T) {
	cfg := config.Default()
	cfg.Models.Executor.Sandbox = "workspace-write"
	cfg.Models.Reviewer.SubAgent = "plan"
	cfg.Loop.MaxIterations = 3

	out := applyChoices(cfg, models.Choices{
		Planner:  models.Choice{Agent: "codex", Model: "gpt-6-astra", Variant: "high"},
		Executor: models.Choice{Agent: "codex", Model: "gpt-6-luna", Variant: "max"},
		Reviewer: models.Choice{Agent: "zai", Model: "zai/glm-5.2", Variant: "highspeed"},
	})

	if out == cfg {
		t.Fatal("applyChoices must return a copy")
	}
	if out.Models.Planner.Agent != "codex" || out.Models.Planner.Model != "gpt-6-astra" || out.Models.Planner.Variant != "high" {
		t.Fatalf("planner not applied: %+v", out.Models.Planner)
	}
	if out.Models.Executor.Model != "gpt-6-luna" || out.Models.Executor.Variant != "max" {
		t.Fatalf("executor not applied: %+v", out.Models.Executor)
	}
	if out.Models.Executor.Sandbox != "workspace-write" || !out.Models.Executor.ApproveForMe {
		t.Fatalf("executor knobs the picker does not own must be kept: %+v", out.Models.Executor)
	}
	if out.Models.Reviewer.SubAgent != "plan" {
		t.Fatalf("reviewer subagent must be kept: %+v", out.Models.Reviewer)
	}
	if out.Loop.MaxIterations != 3 || out.Repo != cfg.Repo {
		t.Fatal("unrelated config must be carried over")
	}
	// The source config is untouched.
	if cfg.Models.Executor.Model == "gpt-6-luna" {
		t.Fatal("applyChoices must not mutate the source config")
	}
}

func TestApplyChoicesEmptyKeepsConfig(t *testing.T) {
	cfg := config.Default()
	out := applyChoices(cfg, models.Choices{})
	if out.Models.Planner.Model != cfg.Models.Planner.Model ||
		out.Models.Executor.Model != cfg.Models.Executor.Model ||
		out.Models.Reviewer.Model != cfg.Models.Reviewer.Model {
		t.Fatalf("empty choices must keep the config models: %+v", out.Models)
	}
}

func TestPlanFiles(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(valid, []byte(`{"summary":"s","steps":[{"id":"1","description":"d"}],"acceptance_criteria":["c"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, prompt, err := planFiles("implement the plan", []string{valid})
	if err != nil {
		t.Fatalf("load valid plan: %v", err)
	}
	if plan.Summary != "s" || len(plan.Steps) != 1 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if prompt != "implement the plan" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}

	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte(`{"summary":"s"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := planFiles("implement the plan", []string{invalid}); err == nil {
		t.Fatal("expected invalid plan error (missing steps/criteria)")
	}
	if _, _, err := planFiles("implement the plan", []string{filepath.Join(dir, "missing.json")}); err == nil {
		t.Fatal("expected missing file error")
	}
}
