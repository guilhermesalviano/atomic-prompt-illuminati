package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func setupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "init")
	return dir
}

func TestWorktreeLifecycle(t *testing.T) {
	repo := setupRepo(t)

	if clean, err := IsClean(repo); err != nil || !clean {
		t.Fatalf("expected clean repo, clean=%v err=%v", clean, err)
	}
	if !IsRepo(repo) {
		t.Fatal("expected git repo")
	}

	parent := t.TempDir()
	wt := filepath.Join(parent, "wt")
	branch := "api/test-run"
	if err := Add(repo, wt, branch, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Fatal("worktree missing checked-out files")
	}

	// New file shows up in diff and changed files.
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := Diff(wt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "new.txt") {
		t.Fatalf("diff missing new file:\n%s", diff)
	}
	files, err := ChangedFiles(wt)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "new.txt" {
		t.Fatalf("changed files = %v", files)
	}

	sha, err := Commit(wt, "test commit")
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) < 7 {
		t.Fatalf("bad sha %q", sha)
	}

	if err := Remove(repo, wt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree still present: %v", err)
	}
	if err := DeleteBranch(repo, branch); err != nil {
		t.Fatal(err)
	}
}

func TestHeadAndClean(t *testing.T) {
	repo := setupRepo(t)
	sha, err := Head(repo)
	if err != nil || len(sha) < 7 {
		t.Fatalf("head=%q err=%v", sha, err)
	}
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if clean, _ := IsClean(repo); clean {
		t.Fatal("expected dirty repo")
	}
}

func TestSnapshotIncludesUntrackedWithoutStaging(t *testing.T) {
	repo := setupRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := Snapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"+changed", "new.txt", "+fresh"} {
		if !strings.Contains(diff, want) {
			t.Errorf("snapshot missing %q:\n%s", want, diff)
		}
	}
	if staged := gitRun(t, repo, "diff", "--cached", "--name-only"); strings.TrimSpace(staged) != "" {
		t.Errorf("snapshot touched the index: %q", staged)
	}
	if st := gitRun(t, repo, "status", "--porcelain"); !strings.Contains(st, "?? new.txt") {
		t.Errorf("new.txt should still be untracked:\n%s", st)
	}
}
