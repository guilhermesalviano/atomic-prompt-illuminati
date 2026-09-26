package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/guibs/atomic-prompt-illuminati/internal/artifact"
	"github.com/guibs/atomic-prompt-illuminati/internal/worktree"
)

func TestDiscardRemovesWorktreeBranchAndArtifacts(t *testing.T) {
	repo := setupRepo(t)
	r, err := artifact.New(t.TempDir(), repo, "add a thing")
	if err != nil {
		t.Fatal(err)
	}
	r.Branch = "api/thing"
	r.Worktree = filepath.Join(t.TempDir(), "wt")
	if err := worktree.Add(repo, r.Worktree, r.Branch, ""); err != nil {
		t.Fatal(err)
	}

	warn, err := Discard(r)
	if warn != nil || err != nil {
		t.Fatalf("Discard: warn=%v err=%v", warn, err)
	}
	for _, p := range []string{r.Worktree, r.Dir} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists", p)
		}
	}
	if worktree.BranchExists(repo, r.Branch) {
		t.Errorf("branch %s still exists", r.Branch)
	}
}

func TestDiscardWarnsWhenRepoIsGone(t *testing.T) {
	r, err := artifact.New(t.TempDir(), filepath.Join(t.TempDir(), "gone"), "x")
	if err != nil {
		t.Fatal(err)
	}
	r.Worktree = t.TempDir() // exists, but git cannot remove it
	warn, err := Discard(r)
	if warn == nil || err != nil {
		t.Fatalf("want a warning only: warn=%v err=%v", warn, err)
	}
	if _, err := os.Stat(r.Dir); !os.IsNotExist(err) {
		t.Errorf("artifacts should be removed even when the worktree is stuck")
	}
}
