package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Models.Planner.Agent != "claude" || c.Models.Executor.Agent != "codex" || c.Models.Reviewer.Agent != "opencode" {
		t.Fatalf("unexpected default adapters: %+v", c.Models)
	}
	if c.Loop.MaxIterations != 3 {
		t.Fatalf("want 3 iterations, got %d", c.Loop.MaxIterations)
	}
	if c.Timeouts.Executor.Duration() != 45*time.Minute {
		t.Fatalf("unexpected executor timeout %s", c.Timeouts.Executor.Duration())
	}
}

func TestLoadOverridesAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kor.yaml")
	content := `
repo: /tmp/x
models:
  executor:
    agent: codex
    model: my-model
    sandbox: read-only
loop:
  max_iterations: 5
timeouts:
  planner: 3m
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Models.Executor.Model != "my-model" || c.Models.Executor.Sandbox != "read-only" {
		t.Fatalf("executor override failed: %+v", c.Models.Executor)
	}
	if c.Loop.MaxIterations != 5 {
		t.Fatalf("want 5 got %d", c.Loop.MaxIterations)
	}
	if c.Timeouts.Planner.Duration() != 3*time.Minute {
		t.Fatalf("want 3m got %s", c.Timeouts.Planner.Duration())
	}
	// Unspecified fields keep defaults.
	if c.Models.Planner.Agent != "claude" {
		t.Fatalf("planner default lost: %+v", c.Models.Planner)
	}
	// Round-trip through MarshalYAML must preserve durations.
	c.Repo = "/tmp/x"
	data, err := c.MarshalYAML()
	if err != nil {
		t.Fatal(err)
	}
	path2 := filepath.Join(dir, "roundtrip.yaml")
	if err := os.WriteFile(path2, data, 0o644); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(path2)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if c2.Timeouts.Planner.Duration() != 3*time.Minute {
		t.Fatalf("duration round-trip lost: %s", c2.Timeouts.Planner.Duration())
	}
	if c2.Models.Executor.Sandbox != "read-only" {
		t.Fatalf("sandbox round-trip lost: %+v", c2.Models.Executor)
	}
}

func TestValidate(t *testing.T) {
	c := Default()
	c.Repo = "/tmp/x"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Models.Executor.Sandbox = "bogus"
	if err := c.Validate(); err == nil {
		t.Fatal("expected invalid sandbox error")
	}
}
