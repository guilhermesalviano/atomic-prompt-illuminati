package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	res, err := (Codex{}).Run(context.Background(), Request{Model: "test", Prompt: "test"})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("expected executable lookup failure, got %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatal("a process that never started must not report success")
	}
}

func TestCodexReportsJSONErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event string
		exit  string
	}{
		{"error", `{"type":"error","message":"Invalid schema: missing changed_files"}`, "1"},
		{"failed turn", `{"type":"turn.failed","error":{"message":"Invalid schema: missing changed_files"}}`, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := "#!/bin/sh\nprintf '%s\\n' '" + tc.event + "'\nexit " + tc.exit + "\n"
			if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			res, err := (Codex{}).Run(context.Background(), Request{Model: "test", Prompt: "test"})
			if err == nil || !strings.Contains(err.Error(), "Invalid schema: missing changed_files") {
				t.Fatalf("expected JSON error detail, got %v", err)
			}
			if len(res.Events) != 1 {
				t.Fatalf("error event was not retained: %+v", res)
			}
		})
	}
}

func TestCodexBuildArgs(t *testing.T) {
	args := buildArgs(Request{
		Dir:          "/wt",
		Model:        "gpt-6-sol",
		Variant:      "high",
		Sandbox:      "workspace-write",
		ApproveForMe: true,
		SchemaFile:   "schema.json",
		OutFile:      "out.txt",
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"exec --json -m gpt-6-sol",
		"-C /wt",
		"--approve-for-me",
		"-c model_reasoning_effort=high",
		"--output-schema schema.json",
		"-o out.txt",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
	if strings.Contains(joined, " -s ") {
		t.Errorf("sandbox flag must not combine with --approve-for-me: %v", args)
	}
}

func TestCodexBuildArgsNoEffort(t *testing.T) {
	args := buildArgs(Request{Model: "gpt-6-sol", Sandbox: "read-only"})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "model_reasoning_effort") {
		t.Errorf("effort must be omitted when unset: %v", args)
	}
	if !strings.Contains(joined, "-s read-only") {
		t.Errorf("sandbox flag missing: %v", args)
	}
}
