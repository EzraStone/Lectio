package vcs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestParseRenamePath(t *testing.T) {
	cases := []struct {
		in       string
		wantNew  string
		wantOld  string
		describe string
	}{
		{"internal/rank/score.go", "internal/rank/score.go", "", "plain path"},
		{"old.go => new.go", "new.go", "old.go", "whole-path rename"},
		{"internal/{rank => score}/file.go", "internal/score/file.go", "internal/rank/file.go", "braced middle"},
		{"{old => new}/file.go", "new/file.go", "old/file.go", "braced prefix"},
		{"internal/{ => rank}/file.go", "internal/rank/file.go", "internal/file.go", "moved into a directory"},
		{"internal/{rank => }/file.go", "internal/file.go", "internal/rank/file.go", "moved out of a directory"},
	}
	for _, c := range cases {
		gotNew, gotOld := parseRenamePath(c.in)
		if gotNew != c.wantNew || gotOld != c.wantOld {
			t.Errorf("%s: parseRenamePath(%q) = (%q, %q), want (%q, %q)",
				c.describe, c.in, gotNew, gotOld, c.wantNew, c.wantOld)
		}
	}
}

func TestParseNumstat(t *testing.T) {
	fc, ok := parseNumstat("12\t3\tinternal/rank/score.go")
	if !ok || fc.Added != 12 || fc.Deleted != 3 || fc.Path != "internal/rank/score.go" {
		t.Errorf("parseNumstat = %+v, ok=%v", fc, ok)
	}

	// Binary files report "-" for both counts. That a binary changed is still
	// signal even though the line counts are not.
	fc, ok = parseNumstat("-\t-\tlogo.png")
	if !ok || fc.Added != 0 || fc.Path != "logo.png" {
		t.Errorf("binary numstat = %+v, ok=%v", fc, ok)
	}

	if _, ok := parseNumstat("not a numstat line"); ok {
		t.Error("non-numstat line was accepted")
	}
}

func TestHasAICoAuthor(t *testing.T) {
	for _, s := range []string{
		"Claude <noreply@anthropic.com>",
		"Copilot <copilot@github.com>",
		"a human <h@example.com>\x1eCursor Agent <agent@cursor.com>",
	} {
		if !hasAICoAuthor(s) {
			t.Errorf("hasAICoAuthor(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "Real Person <real@example.com>"} {
		if hasAICoAuthor(s) {
			t.Errorf("hasAICoAuthor(%q) = true, want false", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration: parsing git's output is exactly where assumptions rot, so the
// round trip runs against a real repository built by the real binary.

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "-q", "-b", "main")
	run("config", "user.name", "First Dev")
	run("config", "user.email", "first@example.com")

	write("a.go", "package a\n\nfunc A() {}\n")
	run("add", ".")
	run("commit", "-q", "-m", "add a")

	run("config", "user.name", "Second Dev")
	run("config", "user.email", "second@example.com")
	write("a.go", "package a\n\nfunc A() { println(1) }\n")
	run("commit", "-qam", "fix: correct A")

	// A rename, so --find-renames parsing is exercised end to end.
	run("mv", "a.go", "b.go")
	run("commit", "-qam", "move a to b")

	// A commit with an AI co-author trailer.
	write("c.go", "package a\n\nfunc C() {}\n")
	run("add", ".")
	run("commit", "-q", "-m", "add C\n\nCo-authored-by: Claude <noreply@anthropic.com>")

	return dir
}

func TestCommitsAgainstRealGit(t *testing.T) {
	dir := gitRepo(t)
	g := NewGit()
	ctx := context.Background()

	commits, err := g.Commits(ctx, dir, time.Time{})
	if err != nil {
		t.Fatalf("Commits: %v", err)
	}
	if len(commits) != 4 {
		t.Fatalf("commits = %d, want 4", len(commits))
	}

	// Chronological order, oldest first — git log gives the reverse.
	if commits[0].Subject != "add a" {
		t.Errorf("first commit = %q, want \"add a\"", commits[0].Subject)
	}
	if commits[3].Subject != "add C" {
		t.Errorf("last commit = %q, want \"add C\"", commits[3].Subject)
	}

	if !commits[1].IsFix() {
		t.Error("\"fix: correct A\" should classify as a fix")
	}
	if commits[1].Author() != "second@example.com" {
		t.Errorf("author = %q", commits[1].Author())
	}

	rename := commits[2]
	if len(rename.Files) != 1 {
		t.Fatalf("rename commit files = %+v", rename.Files)
	}
	if rename.Files[0].Path != "b.go" || rename.Files[0].Renamed != "a.go" {
		t.Errorf("rename = %+v, want b.go from a.go", rename.Files[0])
	}

	if !commits[3].AIAssisted {
		t.Error("Co-authored-by: Claude should mark the commit AI-assisted")
	}
	if commits[0].AIAssisted {
		t.Error("a commit with no trailer must not be marked AI-assisted")
	}

	if len(commits[0].Files) != 1 || commits[0].Files[0].Added != 3 {
		t.Errorf("first commit files = %+v, want 3 added lines in a.go", commits[0].Files)
	}
}

func TestAuthorActivityAgainstRealGit(t *testing.T) {
	dir := gitRepo(t)
	activity, err := NewGit().AuthorActivity(context.Background(), dir)
	if err != nil {
		t.Fatalf("AuthorActivity: %v", err)
	}
	if len(activity) != 2 {
		t.Fatalf("authors = %v, want 2", activity)
	}
	if _, ok := activity["first@example.com"]; !ok {
		t.Error("first author missing")
	}
	if activity["second@example.com"].IsZero() {
		t.Error("second author has no recorded activity")
	}
}

func TestHeadCommitAndIsRepo(t *testing.T) {
	dir := gitRepo(t)
	g := NewGit()
	ctx := context.Background()

	if !g.IsRepo(ctx, dir) {
		t.Error("IsRepo = false on a real repo")
	}
	head, err := g.HeadCommit(ctx, dir)
	if err != nil || len(head) != 40 {
		t.Errorf("HeadCommit = %q, err %v", head, err)
	}
	if NewGit().IsRepo(ctx, t.TempDir()) {
		t.Error("IsRepo = true on a plain directory")
	}
}

func TestCommitsRespectsSince(t *testing.T) {
	dir := gitRepo(t)
	// The fixture commits are dated 2026-01-01; a later cutoff excludes them.
	got, err := NewGit().Commits(context.Background(), dir, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Commits: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("commits after cutoff = %d, want 0", len(got))
	}
}

func TestAINotesAbsentIsNotAnError(t *testing.T) {
	dir := gitRepo(t)
	notes, err := NewGit().AINotes(context.Background(), dir)
	if err != nil {
		t.Fatalf("a repo without the notes ref should not error: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("notes = %v, want empty", notes)
	}
}

func TestOrphaned(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !Orphaned(time.Time{}, now) {
		t.Error("an author with no recorded activity counts as gone")
	}
	if Orphaned(now.AddDate(0, 0, -30), now) {
		t.Error("30 days silent is not orphaned")
	}
	if !Orphaned(now.AddDate(0, 0, -120), now) {
		t.Error("120 days silent is orphaned")
	}
}
