package index

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/adapter"
	golangadapter "github.com/EzraStone/Lectio/internal/adapter/golang"
	"github.com/EzraStone/Lectio/internal/store"
)

// A rewound repository read with a window anchored at the wall clock contains
// no commits at all: every history-derived signal goes silent and the ranking
// degrades to structure while still reporting a number. This is the bug that
// made the first real Gate A run report 0.0% for all four history baselines.
func TestHistoryWindowIsAnchoredAtAsOf(t *testing.T) {
	for _, bin := range []string{"go", "git"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}

	src, err := filepath.Abs(filepath.Join("..", "adapter", "golang", "testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if out, err := exec.Command("cp", "-r", src+"/.", dst).CombinedOutput(); err != nil {
		t.Fatalf("copy fixture: %v\n%s", err, out)
	}

	// History dated four years ago, far outside any window anchored at today.
	const ancient = "2022-03-01T00:00:00Z"
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "Long Ago"},
		{"config", "user.email", "past@example.com"},
		{"add", "."},
		{"commit", "-q", "-m", "the distant past"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dst
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE="+ancient, "GIT_COMMITTER_DATE="+ancient,
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	ctx := context.Background()
	build := func(asOf time.Time) int {
		t.Helper()
		s, err := store.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()

		opts := adapter.DefaultOptions()
		opts.HistoryWindow = 365 * 24 * time.Hour
		opts.AsOf = asOf

		if _, err := Build(ctx, s, golangadapter.New(), dst, opts); err != nil {
			t.Fatalf("Build: %v", err)
		}
		v, err := Load(ctx, s)
		if err != nil {
			t.Fatal(err)
		}
		return len(v.Commits)
	}

	// Anchored at the wall clock, four-year-old history falls outside.
	if got := build(time.Time{}); got != 0 {
		t.Errorf("with a wall-clock anchor, commits = %d, want 0", got)
	}

	// Anchored a month after the commit, it is inside.
	asOf, err := time.Parse(time.RFC3339, ancient)
	if err != nil {
		t.Fatal(err)
	}
	if got := build(asOf.AddDate(0, 1, 0)); got == 0 {
		t.Error("with the window anchored at the rewind date, history was still empty")
	}
}
