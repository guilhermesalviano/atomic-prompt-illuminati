// Package worktree manages the isolated git worktree each pipeline run uses, so
// the target repository's working tree is never touched.
package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo(dir string) bool {
	out, err := git(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// Head returns the current commit SHA of repo.
func Head(repo string) (string, error) {
	out, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// IsClean reports whether the repo has no staged, unstaged or untracked changes.
func IsClean(repo string) (bool, error) {
	out, err := git(repo, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

// Add creates a new worktree at path on a fresh branch.
func Add(repo, path, branch, base string) error {
	if base == "" {
		base = "HEAD"
	}
	_, err := git(repo, "worktree", "add", "-b", branch, path, base)
	return err
}

// ValidBranch reports whether name can be used as a new branch name. An empty
// name is rejected because the worktree name is required to start a run.
func ValidBranch(repo, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a worktree name is required")
	}
	if _, err := git(repo, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("invalid worktree name %q", name)
	}
	return nil
}

// BranchExists reports whether a local branch already exists.
func BranchExists(repo, branch string) bool {
	_, err := git(repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// Remove deletes a worktree, force-removing it if needed.
func Remove(repo, path string) error {
	if _, err := git(repo, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	_, _ = git(repo, "worktree", "prune")
	return nil
}

// DeleteBranch removes a run branch after its worktree is gone.
func DeleteBranch(repo, branch string) error {
	_, err := git(repo, "branch", "-D", branch)
	return err
}

// ChangedFiles lists files reported as modified/added/deleted in the worktree.
func ChangedFiles(worktree string) ([]string, error) {
	out, err := git(worktree, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if i := strings.LastIndex(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		files = append(files, strings.Trim(path, `"`))
	}
	return files, nil
}

// Diff returns the full diff of the worktree including untracked files.
func Diff(worktree string) (string, error) {
	if _, err := git(worktree, "add", "-A", "-N"); err != nil {
		return "", err
	}
	out, err := git(worktree, "diff", "--no-color", "--no-ext-diff")
	if err != nil {
		return "", err
	}
	return out, nil
}

// Snapshot returns the worktree's changes against HEAD, including untracked
// files, without touching the index. It is safe to call while an agent is
// working in the worktree, unlike Diff which marks files intent-to-add.
func Snapshot(worktree string) (string, error) {
	out, err := git(worktree, "diff", "HEAD", "--no-color", "--no-ext-diff")
	if err != nil {
		return "", err
	}
	others, err := git(worktree, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return out, err
	}
	var b strings.Builder
	b.WriteString(out)
	for _, f := range strings.Split(others, "\x00") {
		if f == "" {
			continue
		}
		// --no-index exits 1 when the files differ, which is always the case
		// here, so the output is kept regardless of the error.
		d, _ := git(worktree, "diff", "--no-color", "--no-ext-diff", "--no-index", "--", "/dev/null", f)
		b.WriteString(d)
	}
	return b.String(), nil
}

// Stage adds every change in the worktree to the index without committing.
func Stage(worktree string) error {
	_, err := git(worktree, "add", "-A")
	return err
}

// Push publishes branch to origin and sets its upstream.
func Push(worktree, branch string) error {
	_, err := git(worktree, "push", "-u", "origin", branch)
	return err
}

// Commit stages everything and creates a commit, returning the new SHA. It is a
// no-op when there is nothing to commit.
func Commit(worktree, message string) (string, error) {
	status, err := git(worktree, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) == "" {
		return "", nil
	}
	if _, err := git(worktree, "add", "-A"); err != nil {
		return "", err
	}
	_, err = git(worktree, "-c", "user.name=api", "-c", "user.email=api@localhost",
		"commit", "-m", message)
	if err != nil {
		return "", err
	}
	out, err := git(worktree, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
