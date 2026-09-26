package agent

import (
	"strings"
	"testing"
)

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
