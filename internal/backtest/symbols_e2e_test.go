package backtest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/adapter"
	golangadapter "github.com/EzraStone/Lectio/internal/adapter/golang"
	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/store"
	"github.com/EzraStone/Lectio/internal/vcs"
)

// attributionRepo builds a module whose history contains the exact situation
// symbol attribution exists to survive: a function that stays put by name
// while moving a long way down the file, and a second function that does not
// move and is never touched.
func attributionRepo(t *testing.T) string {
	t.Helper()
	for _, bin := range []string{"go", "git"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	dir := t.TempDir()

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Newcomer", "GIT_AUTHOR_EMAIL=new@example.com",
			"GIT_COMMITTER_NAME=Newcomer", "GIT_COMMITTER_EMAIL=new@example.com",
			"GIT_AUTHOR_DATE=2026-02-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-02-01T00:00:00Z",
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
	write("go.mod", "module example.com/attr\n\ngo 1.21\n")
	write("a.go", "package attr\n\nfunc Target() int { return 1 }\n\nfunc Bystander() int { return 2 }\n")
	git("add", ".")
	git("commit", "-qm", "initial")

	// Push Target far down the file, then edit it. Its line numbers move by
	// forty; its name does not.
	var padding string
	for i := 0; i < 20; i++ {
		padding += "// filler\n"
	}
	write("a.go", "package attr\n\n"+padding+"\nfunc Target() int { return 99 }\n\nfunc Bystander() int { return 2 }\n")
	git("commit", "-qam", "fix: correct Target")

	return dir
}

func indexRepo(t *testing.T, root string) *index.View {
	t.Helper()
	ctx := context.Background()

	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	a := golangadapter.New()
	a.History = vcs.NewGit()
	if _, err := index.Build(ctx, s, a, root, adapter.DefaultOptions()); err != nil {
		t.Fatalf("index: %v", err)
	}
	v, err := index.Load(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The whole pipeline: git diff to hunks, hunks to declaration names via a
// syntax-only parse at the commit, names back to indexed symbols.
func TestAttributionEndToEnd(t *testing.T) {
	root := attributionRepo(t)
	ctx := context.Background()
	g := vcs.NewGit()

	commits, err := g.Commits(ctx, root, timeZeroForTest())
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("built %d commits, want 2", len(commits))
	}

	// Index the repository as it stands, which contains both declarations.
	v := indexRepo(t, root)
	if _, ok := v.Symbols["example.com/attr.Target"]; !ok {
		t.Fatalf("Target missing from the index: %v", keysOf(v.Symbols))
	}

	paths := NewPathTracker(map[string]bool{"a.go": true})
	got, err := AttributeSymbols(ctx, g, golangadapter.New(), v, root,
		[]core.Commit{commits[1]}, paths)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Fatalf("attributed %v, want exactly Target", got)
	}
	if got[0] != "example.com/attr.Target" {
		t.Errorf("attributed %q, want example.com/attr.Target", got[0])
	}
}

// Bystander sits below Target and never changed. If attribution followed line
// numbers rather than names it would be swept up by the forty-line shift.
func TestAttributionDoesNotSweepUpUntouchedNeighbours(t *testing.T) {
	root := attributionRepo(t)
	ctx := context.Background()
	g := vcs.NewGit()

	commits, _ := g.Commits(ctx, root, timeZeroForTest())
	v := indexRepo(t, root)

	got, _ := AttributeSymbols(ctx, g, golangadapter.New(), v, root,
		[]core.Commit{commits[1]}, NewPathTracker(map[string]bool{"a.go": true}))

	for _, id := range got {
		if id == "example.com/attr.Bystander" {
			t.Error("an untouched declaration was attributed — line numbers leaked into the match")
		}
	}
}

func keysOf(m map[core.SymbolID]core.Symbol) []core.SymbolID {
	out := make([]core.SymbolID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func timeZeroForTest() time.Time { return time.Time{} }
