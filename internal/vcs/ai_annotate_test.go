package vcs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

// notesRepo builds a repository with a git-ai note on exactly one of three
// commits, so annotation can be checked against a real notes ref rather than a
// stub.
func notesRepo(t *testing.T) (root string, hashes []string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=A", "GIT_AUTHOR_EMAIL=a@example.com",
			"GIT_COMMITTER_NAME=A", "GIT_COMMITTER_EMAIL=a@example.com",
			"GIT_AUTHOR_DATE=2026-03-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-03-01T00:00:00Z",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	git("init", "-q", "-b", "main")
	for i, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "-qm", "commit "+string(rune('1'+i)))
	}

	out := git("log", "--format=%H")
	for _, line := range splitLines(out) {
		if line != "" {
			hashes = append(hashes, line)
		}
	}
	// log is newest-first; make it oldest-first to match Commits().
	for i, j := 0, len(hashes)-1; i < j; i, j = i+1, j-1 {
		hashes[i], hashes[j] = hashes[j], hashes[i]
	}

	// A note on the middle commit only.
	git("notes", "--ref="+AINotesRef, "add", "-m", `{"model":"test"}`, hashes[1])
	return dir, hashes
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

func TestAINotesReadsTheNotesRef(t *testing.T) {
	dir, hashes := notesRepo(t)

	got, err := NewGit().AINotes(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("found %d notes, want 1: %v", len(got), got)
	}
	if !got[hashes[1]] {
		t.Errorf("the annotated commit %s is not in %v", hashes[1], got)
	}
}

func TestAnnotateAIMarksOnlyNotedCommits(t *testing.T) {
	dir, hashes := notesRepo(t)

	commits := make([]core.Commit, len(hashes))
	for i, h := range hashes {
		commits[i] = core.Commit{Hash: h}
	}
	if err := NewGit().AnnotateAI(context.Background(), dir, commits); err != nil {
		t.Fatal(err)
	}

	for i, c := range commits {
		want := i == 1
		if c.AIAssisted != want {
			t.Errorf("commit %d AIAssisted = %v, want %v", i, c.AIAssisted, want)
		}
	}
}

// The two sources are complementary. A trailer-derived mark set during the log
// parse must survive annotation, or a repository that uses trailers and also
// has a notes ref would lose every trailer-only commit.
func TestAnnotateAIPreservesTrailerMarks(t *testing.T) {
	dir, hashes := notesRepo(t)

	commits := []core.Commit{
		{Hash: hashes[0], AIAssisted: true}, // found via a trailer
		{Hash: hashes[1]},                   // found via the note
		{Hash: hashes[2]},
	}
	if err := NewGit().AnnotateAI(context.Background(), dir, commits); err != nil {
		t.Fatal(err)
	}

	if !commits[0].AIAssisted {
		t.Error("a trailer-derived mark was overwritten by annotation")
	}
	if !commits[1].AIAssisted {
		t.Error("the noted commit was not marked")
	}
	if commits[2].AIAssisted {
		t.Error("an unmarked commit was marked")
	}
}

// Matching is substring-based, which has a false-positive edge and a
// near-miss. Both are recorded here so a future change to the marker list is
// made knowing what it already does.
func TestAICoAuthorMatchingEdges(t *testing.T) {
	for _, tc := range []struct {
		trailers string
		want     bool
		why      string
	}{
		{"Co-authored-by: CLAUDE <NOREPLY@ANTHROPIC.COM>", true, "matching is case-insensitive"},
		{"Co-authored-by: Claudia Smith <claudia@example.com>", false,
			"claudia is not claude — the substring stops short"},
		{"Co-authored-by: Claude Monet <monet@example.com>", true,
			"a human named Claude is a false positive; the signal errs toward flagging"},
		{"Co-authored-by: Jane Dev <jane@example.com>", false, "an ordinary human"},
	} {
		if got := hasAICoAuthor(tc.trailers); got != tc.want {
			t.Errorf("hasAICoAuthor(%q) = %v, want %v — %s", tc.trailers, got, tc.want, tc.why)
		}
	}
}
