package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/guilhermesalviano/korchestrate/internal/artifact"
)

// finishedApp returns an app listing three finished runs with the middle one
// selected, and records which runs discard was asked to delete.
func finishedApp(t *testing.T) (*App, *[]string) {
	t.Helper()
	a := NewApp(testConfig(), t.TempDir())
	for _, id := range []string{"a", "b", "c"} {
		e := entryFromRun(&artifact.Run{ID: id, Branch: id, State: artifact.StateDone, Dir: t.TempDir()})
		a.entries = append(a.entries, e)
	}
	a.cursor = 1
	var gone []string
	a.discard = func(r *artifact.Run) (error, error) {
		gone = append(gone, r.ID)
		return nil, nil
	}
	return a, &gone
}

func TestDeleteAsksThenRemovesSelectedRun(t *testing.T) {
	a, gone := finishedApp(t)

	a.handleKey(key("x"))
	if a.confirmDel == nil || !strings.Contains(a.View(), "Delete b?") {
		t.Fatalf("x should ask for confirmation:\n%s", a.View())
	}
	_, cmd := a.handleKey(key("y"))
	if cmd == nil || !a.entries[1].deleting {
		t.Fatal("y should start the delete")
	}
	a.Update(cmd())

	if len(*gone) != 1 || (*gone)[0] != "b" {
		t.Fatalf("discarded %v, want [b]", *gone)
	}
	if len(a.entries) != 2 || a.current().Run.ID != "c" {
		t.Fatalf("want b gone and c selected, got cursor %d of %d", a.cursor, len(a.entries))
	}
}

func TestDeleteCancelKeepsRun(t *testing.T) {
	a, gone := finishedApp(t)
	a.handleKey(key("x"))
	if _, cmd := a.handleKey(esc()); cmd != nil || a.confirmDel != nil {
		t.Fatal("esc should cancel without deleting")
	}
	if len(*gone) != 0 || len(a.entries) != 3 {
		t.Fatalf("nothing should be deleted: %v", *gone)
	}
}

func TestDeleteLastRunMovesCursorUp(t *testing.T) {
	a, _ := finishedApp(t)
	a.cursor = 2
	a.handleKey(key("x"))
	_, cmd := a.handleKey(key("y"))
	a.Update(cmd())
	if a.cursor != 1 || a.current().Run.ID != "b" {
		t.Fatalf("cursor %d, want 1 on b", a.cursor)
	}
}

func TestDeleteRefusesLiveRun(t *testing.T) {
	a, _ := finishedApp(t)
	a.entries[1].Live = true
	a.handleKey(key("x"))
	if a.confirmDel != nil {
		t.Fatal("a live run must not be deletable")
	}
	if !strings.Contains(a.View(), "can't delete a running worktree") {
		t.Fatalf("expected a notice:\n%s", a.View())
	}
}

func TestDeleteFailureKeepsRunAndShowsError(t *testing.T) {
	a, _ := finishedApp(t)
	a.discard = func(*artifact.Run) (error, error) { return nil, errors.New("permission denied") }
	a.handleKey(key("x"))
	_, cmd := a.handleKey(key("y"))
	a.Update(cmd())
	e := a.entries[1]
	if len(a.entries) != 3 || e.deleting || !strings.Contains(e.ErrText, "permission denied") {
		t.Fatalf("failed delete should keep the run with an error: %+v", e)
	}
}

func TestDeleteWarnsAboutUnpushedCommit(t *testing.T) {
	a, _ := finishedApp(t)
	a.entries[1].Run.Commit = "0123456789abcdef"
	a.handleKey(key("x"))
	if !strings.Contains(a.View(), "never pushed") {
		t.Fatalf("expected an unpushed-commit warning:\n%s", a.View())
	}
}

func TestSidebarShowsKoalaWhenItFits(t *testing.T) {
	a, _ := finishedApp(t)
	a.width, a.height = 120, 40
	if !strings.Contains(a.View(), "▄█▄") {
		t.Fatalf("a tall sidebar should show the koala:\n%s", a.View())
	}
	a.height = 16
	if strings.Contains(a.View(), "▄█▄") {
		t.Fatalf("a short sidebar should give its rows to the list:\n%s", a.View())
	}
}
