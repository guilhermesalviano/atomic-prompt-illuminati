package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLoadList(t *testing.T) {
	base := t.TempDir()
	r, err := New(base, "/repo", "Add rate limiting to the API!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.Path("run.json")); err != nil {
		t.Fatalf("run.json missing: %v", err)
	}
	if data, _ := r.Read("prompt.txt"); string(data) != "Add rate limiting to the API!\n" {
		t.Fatalf("prompt artifact = %q", data)
	}

	got, err := Load(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Repo != "/repo" || got.State != StatePreflight {
		t.Fatalf("unexpected run: %+v", got)
	}

	byID, err := LoadByID(base, r.ID[:10])
	if err != nil {
		t.Fatal(err)
	}
	if byID.ID != r.ID {
		t.Fatalf("prefix lookup got %s want %s", byID.ID, r.ID)
	}

	// No second run with same prefix ambiguity.
	list, err := List(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 run got %d", len(list))
	}
}

func TestSlug(t *testing.T) {
	if got := Slug("Hello, World! / Foo", 40); got != "hello-world-foo" {
		t.Fatalf("slug = %q", got)
	}
	long := Slug("aaaaaaaaaabbbbbbbbbbccccccccccddddddddddeeeeeeeeee", 10)
	if len(long) > 10 {
		t.Fatalf("slug too long: %q", long)
	}
	if got := Slug("!!!", 10); got != "run" {
		t.Fatalf("empty slug = %q", got)
	}
}

func TestFailAndState(t *testing.T) {
	base := t.TempDir()
	r, err := New(base, "/repo", "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetState(StatePlanning); err != nil {
		t.Fatal(err)
	}
	if err := r.Fail(os.ErrPermission); err != nil {
		t.Fatal(err)
	}
	got, _ := Load(filepath.Join(base, r.ID))
	if got.State != StateFailed || got.Error == "" {
		t.Fatalf("state=%s err=%q", got.State, got.Error)
	}
}
