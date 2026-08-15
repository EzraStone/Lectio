package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The index lives inside the repository it describes, which is what makes it
// disposable and what makes `rm -rf .lectio` a complete uninstall.
func TestOpenRepoPlacesTheIndexInsideTheRepository(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	s, err := OpenRepo(ctx, root)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	defer s.Close()

	want := DefaultPath(root)
	if got := s.Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("no database at %s: %v", want, err)
	}
	if rel, err := filepath.Rel(root, want); err != nil || filepath.IsAbs(rel) || rel == ".." {
		t.Errorf("the index landed outside the repository: %s", want)
	}
}

// The root is recorded so later commands can find the repository the index
// describes without being told again.
func TestOpenRepoRecordsItsRoot(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	s, err := OpenRepo(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.SetMeta(ctx, "root", root); err != nil {
		t.Fatal(err)
	}
	got, err := s.Meta(ctx, "root")
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
}

// Opening the same repository twice must reuse the database rather than
// truncating it, or a second command in the same session would wipe the index
// the first one built.
func TestOpenRepoIsIdempotent(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	first, err := OpenRepo(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetMeta(ctx, "marker", "kept"); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second, err := OpenRepo(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	got, err := second.Meta(ctx, "marker")
	if err != nil {
		t.Fatal(err)
	}
	if got != "kept" {
		t.Errorf("marker = %q after reopening, want kept — the database was replaced", got)
	}
}

func TestOpenRepoFailsOnAnUnwritablePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission checks do not apply")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Skip("cannot make the directory read-only")
	}
	t.Cleanup(func() { os.Chmod(root, 0o700) })

	if s, err := OpenRepo(context.Background(), root); err == nil {
		s.Close()
		t.Error("opening an index in a read-only directory should fail")
	}
}
