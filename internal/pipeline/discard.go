package pipeline

import (
	"fmt"
	"os"

	"github.com/guibs/atomic-prompt-illuminati/internal/artifact"
	"github.com/guibs/atomic-prompt-illuminati/internal/worktree"
)

// Discard deletes everything a finished run left behind: its worktree, its
// branch and its artifact directory. A worktree git refuses to remove (for
// example because the repo itself is gone) comes back as warn and does not
// stop the rest of the cleanup; err means the run's record is still on disk.
func Discard(r *artifact.Run) (warn, err error) {
	if r.Worktree != "" {
		if _, err := os.Stat(r.Worktree); err == nil {
			if err := worktree.Remove(r.Repo, r.Worktree); err != nil {
				warn = fmt.Errorf("remove worktree: %w", err)
			}
		}
	}
	if r.Branch != "" {
		_ = worktree.DeleteBranch(r.Repo, r.Branch)
	}
	return warn, os.RemoveAll(r.Dir)
}
