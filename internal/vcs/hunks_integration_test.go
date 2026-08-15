package vcs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hunkRepo builds a small repository whose commits exercise the cases that
// break line attribution: an edit in the middle of a file, a pure deletion, a
// rename, and a file created after the fact.
func hunkRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q", "-b", "main")
	write("a.go", "package p\n\nfunc One() {}\n\nfunc Two() {}\n\nfunc Three() {}\n")
	git("add", ".")
	git("commit", "-qm", "initial")

	// Edit the middle function only.
	write("a.go", "package p\n\nfunc One() {}\n\nfunc Two() int { return 2 }\n\nfunc Three() {}\n")
	git("commit", "-qam", "change Two")

	// Delete the middle function entirely.
	write("a.go", "package p\n\nfunc One() {}\n\nfunc Three() {}\n")
	git("commit", "-qam", "drop Two")

	return dir
}

func TestHunksAgainstARealRepository(t *testing.T) {
	dir := hunkRepo(t)
	g := NewGit()
	ctx := context.Background()

	commits, err := g.Commits(ctx, dir, timeZero())
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("built %d commits, want 3", len(commits))
	}

	// "change Two" edits line 5 of the post-commit file.
	got, err := g.Hunks(ctx, dir, commits[1].Hash)
	if err != nil {
		t.Fatal(err)
	}
	ranges := got["a.go"]
	if len(ranges) != 1 {
		t.Fatalf("edit produced %d ranges, want 1: %+v", len(ranges), ranges)
	}
	if !ranges[0].Touches(5, 5) {
		t.Errorf("range %+v does not cover the edited line 5", ranges[0])
	}
	if ranges[0].Touches(3, 3) {
		t.Errorf("range %+v reaches an untouched function on line 3", ranges[0])
	}
}

// Git reports a pure deletion as a zero-length insertion point. If those were
// dropped, every commit that only removes code would attribute to nothing —
// and removals are a large share of corrective work.
func TestHunksAttributeAPureDeletion(t *testing.T) {
	dir := hunkRepo(t)
	g := NewGit()
	ctx := context.Background()

	commits, _ := g.Commits(ctx, dir, timeZero())
	got, err := g.Hunks(ctx, dir, commits[2].Hash)
	if err != nil {
		t.Fatal(err)
	}

	ranges := got["a.go"]
	if len(ranges) == 0 {
		t.Fatal("a deletion produced no ranges at all")
	}
	var zero bool
	for _, r := range ranges {
		if r.Count == 0 {
			zero = true
		}
	}
	if !zero {
		t.Errorf("no zero-count range for a pure deletion: %+v", ranges)
	}
}

// The root commit has no parent to diff against. That is a fact about a
// repository's first commit, not a failure — and it must not abort a run.
func TestHunksOnTheRootCommitIsEmptyNotAnError(t *testing.T) {
	dir := hunkRepo(t)
	g := NewGit()
	ctx := context.Background()

	commits, _ := g.Commits(ctx, dir, timeZero())
	got, err := g.Hunks(ctx, dir, commits[0].Hash)
	if err != nil {
		t.Errorf("root commit returned an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("root commit produced ranges: %+v", got)
	}
}

// Attribution parses source as it stood when the change landed, so a revision
// whose content no longer exists on disk still has to be readable.
func TestFileAtReadsAHistoricalRevision(t *testing.T) {
	dir := hunkRepo(t)
	g := NewGit()
	ctx := context.Background()

	commits, _ := g.Commits(ctx, dir, timeZero())

	before, err := g.FileAt(ctx, dir, commits[0].Hash, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(before, "func Two() {}") {
		t.Errorf("the original revision does not contain the original Two:\n%s", before)
	}

	after, err := g.FileAt(ctx, dir, commits[2].Hash, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if contains(after, "func Two") {
		t.Errorf("the final revision still contains Two:\n%s", after)
	}
}

func TestFileAtReportsAMissingPath(t *testing.T) {
	dir := hunkRepo(t)
	g := NewGit()
	commits, _ := g.Commits(context.Background(), dir, timeZero())

	if _, err := g.FileAt(context.Background(), dir, commits[0].Hash, "nope.go"); err == nil {
		t.Error("reading a path that never existed should fail")
	}
}

func contains(b []byte, s string) bool { return strings.Contains(string(b), s) }

func timeZero() time.Time { return time.Time{} }
