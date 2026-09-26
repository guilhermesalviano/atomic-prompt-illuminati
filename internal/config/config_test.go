package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

func TestLoadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{
		"models": {
			"executor": {
				"agent": "opencode", "model": "provider/custom-model",
				"approve_for_me": false, "extra_args": ["--verbose"]
			}
		},
		"loop": {"max_iterations": 5},
		"gates": {"after_plan": false},
		"timeouts": {"planner": "3m"}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Models.Executor.Agent != "opencode" || c.Models.Executor.Model != "provider/custom-model" {
		t.Fatalf("executor override failed: %+v", c.Models.Executor)
	}
	if c.Models.Executor.ApproveForMe || c.Gates.AfterPlan || c.Loop.MaxIterations != 5 {
		t.Fatalf("JSON overrides not applied: %+v", c)
	}
	if !reflect.DeepEqual(c.Models.Executor.ExtraArgs, []string{"--verbose"}) || c.Timeouts.Planner.Duration() != 3*time.Minute {
		t.Fatalf("JSON arguments or duration not decoded: %+v", c)
	}
	d := Default()
	if !reflect.DeepEqual(c.Models.Planner, d.Models.Planner) || c.Models.Executor.Sandbox != d.Models.Executor.Sandbox {
		t.Fatalf("unspecified defaults lost: %+v", c.Models)
	}
}

func TestDefaultConfigJSON(t *testing.T) {
	path := filepath.Join("..", "..", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatal("default config.json must be valid JSON")
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Models, Default().Models) {
		t.Fatalf("config.json differs from built-in model defaults: %+v", c.Models)
	}
}

func TestDiscover(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files []string
		want  string
	}{
		{name: "none"},
		{name: "repo JSON", files: []string{"repo/config.json"}, want: "repo/config.json"},
		{name: "user JSON", files: []string{"user/kor/config.json"}, want: "user/kor/config.json"},
		{name: "user YAML", files: []string{"user/kor/config.yaml"}, want: "user/kor/config.yaml"},
		{name: "repo before user", files: []string{"repo/config.json", "user/kor/config.yaml"}, want: "repo/config.json"},
		{name: "repo YAML before JSON", files: []string{"repo/config.json", "repo/kor.yaml"}, want: "repo/kor.yaml"},
		{name: "user YAML before JSON", files: []string{"user/kor/config.json", "user/kor/config.yaml"}, want: "user/kor/config.yaml"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, useXDG := range []bool{true, false} {
				dir := t.TempDir()
				t.Setenv("HOME", dir)
				userDir := filepath.Join(dir, ".config")
				if useXDG {
					userDir = filepath.Join(dir, "xdg")
					t.Setenv("XDG_CONFIG_HOME", userDir)
				} else {
					t.Setenv("XDG_CONFIG_HOME", "")
				}
				resolve := func(path string) string {
					if filepath.Dir(path) == "user/kor" {
						return filepath.Join(userDir, "kor", filepath.Base(path))
					}
					return filepath.Join(dir, path)
				}
				for _, file := range tt.files {
					path := resolve(file)
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				want := ""
				if tt.want != "" {
					want = resolve(tt.want)
				}
				if got := Discover(filepath.Join(dir, "repo")); got != want {
					t.Fatalf("XDG=%t: Discover() = %q, want %q", useXDG, got, want)
				}
			}
		})
	}
}
