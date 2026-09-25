// Package preflight verifies that the required agent CLIs, credentials and git
// state are present before an expensive pipeline run starts.
package preflight

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/guibs/atomic-prompt-illuminati/internal/worktree"
)

// Check is one preflight assertion.
type Check struct {
	Name   string
	OK     bool
	Fatal  bool
	Detail string
}

// Checks runs all preflight assertions for the given repo.
func Checks(repo string) []Check {
	checks := []Check{
		versionCheck("claude", "--version", true),
		versionCheck("codex", "--version", true),
		versionCheck("opencode", "--version", true),
		gitRepoCheck(repo),
		authCheck("claude", []string{
			filepath.Join(home(), ".claude", ".credentials.json"),
			filepath.Join(home(), ".claude.json"),
		}, "ANTHROPIC_API_KEY"),
		authCheck("codex", []string{
			filepath.Join(home(), ".codex", "auth.json"),
		}, "OPENAI_API_KEY"),
		authCheck("opencode", []string{
			filepath.Join(home(), ".local", "share", "opencode", "auth.json"),
		}, ""),
	}
	return checks
}

// Fatal returns the first fatal failure as an error, or nil.
func Fatal(checks []Check) error {
	var msgs []string
	for _, c := range checks {
		if c.Fatal && !c.OK {
			msgs = append(msgs, fmt.Sprintf("%s: %s", c.Name, c.Detail))
		}
	}
	if len(msgs) > 0 {
		return fmt.Errorf("preflight failed:\n  - %s", strings.Join(msgs, "\n  - "))
	}
	return nil
}

func versionCheck(bin, flag string, fatal bool) Check {
	path, err := exec.LookPath(bin)
	if err != nil {
		return Check{Name: bin, OK: false, Fatal: fatal, Detail: "not found on PATH"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, flag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Some CLIs return non-zero for --version; presence is what matters.
		line := strings.TrimSpace(string(out))
		if line == "" {
			line = err.Error()
		}
		return Check{Name: bin, OK: true, Fatal: fatal, Detail: fmt.Sprintf("%s (%s)", path, firstLine(line))}
	}
	return Check{Name: bin, OK: true, Fatal: fatal, Detail: fmt.Sprintf("%s (%s)", path, firstLine(string(out)))}
}

func gitRepoCheck(repo string) Check {
	if repo == "" {
		return Check{Name: "git", OK: false, Fatal: true, Detail: "no repo specified"}
	}
	if !worktree.IsRepo(repo) {
		return Check{Name: "git", OK: false, Fatal: true, Detail: repo + " is not a git repository"}
	}
	clean, err := worktree.IsClean(repo)
	if err != nil {
		return Check{Name: "git", OK: false, Fatal: true, Detail: err.Error()}
	}
	if !clean {
		return Check{Name: "git", OK: false, Fatal: false, Detail: "working tree is not clean (use --allow-dirty)"}
	}
	return Check{Name: "git", OK: true, Fatal: true, Detail: "clean repository"}
}

func authCheck(bin string, files []string, envVar string) Check {
	if envVar != "" {
		if os.Getenv(envVar) != "" {
			return Check{Name: bin + " auth", OK: true, Detail: "via " + envVar}
		}
	}
	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			return Check{Name: bin + " auth", OK: true, Detail: "found " + f}
		}
	}
	return Check{Name: bin + " auth", OK: false, Fatal: false, Detail: "no local credential found; agent may fail to authenticate"}
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

func firstLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
