package corpus

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// localOrigin builds a real git repository to clone from, so Ensure is tested
// against git rather than against a mock of it.
func localOrigin(t *testing.T) (url, rev string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	run("init", "-q", "-b", "main")
	run("config", "user.name", "Fixture")
	run("config", "user.email", "fixture@example.com")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "first")
	first := run("rev-parse", "HEAD")

	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package a\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "second")

	return dir, first
}

// newCache bypasses Validate's https rule, which exists for manifests rather
// than for the fetching machinery.
func newTestCache(t *testing.T) *Cache {
	t.Helper()
	c := NewCache(t.TempDir())
	c.Timeout = 2 * time.Minute
	return c
}

func TestEnsureClonesAndChecksOutThePin(t *testing.T) {
	origin, first := localOrigin(t)
	c := newTestCache(t)
	r := Repo{Name: "fixture/repo", URL: origin, Rev: first}

	dir, err := c.Ensure(context.Background(), r)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	head, err := c.git(context.Background(), dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(head) != first {
		t.Errorf("checked out %s, want the pinned %s", strings.TrimSpace(head), first)
	}

	// The pin is an older commit, so the newer file must not be present.
	if _, err := os.Stat(filepath.Join(dir, "b.go")); err == nil {
		t.Error("working tree is at HEAD rather than at the pinned revision")
	}
}

// The backtest rewinds to a commit deep in the past; a shallow clone makes
// most of the corpus unusable in a way that looks like "no contributors"
// rather than an error.
func TestEnsureKeepsFullHistory(t *testing.T) {
	origin, first := localOrigin(t)
	c := newTestCache(t)

	dir, err := c.Ensure(context.Background(), Repo{Name: "fixture/repo", URL: origin, Rev: first})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "shallow")); err == nil {
		t.Error("clone is shallow")
	}
	out, err := c.git(context.Background(), dir, "rev-list", "--count", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "2" {
		t.Errorf("history has %s commits, want 2", strings.TrimSpace(out))
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	origin, first := localOrigin(t)
	c := newTestCache(t)
	r := Repo{Name: "fixture/repo", URL: origin, Rev: first}

	for i := 0; i < 3; i++ {
		if _, err := c.Ensure(context.Background(), r); err != nil {
			t.Fatalf("Ensure run %d: %v", i, err)
		}
	}
}

func TestEnsureRefusesUnpinned(t *testing.T) {
	c := newTestCache(t)
	_, err := c.Ensure(context.Background(), Repo{Name: "x/y", URL: "https://example.com/x.git"})
	if err == nil || !strings.Contains(err.Error(), "corpus pin") {
		t.Errorf("error = %v, want a pointer to the pin command", err)
	}
}

func TestPinResolvesWithoutCloning(t *testing.T) {
	origin, _ := localOrigin(t)
	c := newTestCache(t)
	m := &Manifest{Version: 1, Repos: []Repo{{Name: "fixture/repo", URL: origin}}}

	changed, err := c.Pin(context.Background(), m)
	if err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if changed != 1 {
		t.Errorf("changed = %d, want 1", changed)
	}
	if !revRE.MatchString(m.Repos[0].Rev) {
		t.Errorf("rev = %q, want a full sha", m.Repos[0].Rev)
	}
	// Nothing should have been cloned.
	if entries, _ := os.ReadDir(c.Dir); len(entries) != 0 {
		t.Errorf("Pin created %d cache entries; it should clone nothing", len(entries))
	}

	// Re-pinning an unchanged remote reports no change, so the manifest diff
	// stays empty.
	if changed, err := c.Pin(context.Background(), m); err != nil || changed != 0 {
		t.Errorf("second Pin: changed=%d err=%v, want 0 and nil", changed, err)
	}
}

func TestStatusReportsReadiness(t *testing.T) {
	origin, first := localOrigin(t)
	c := newTestCache(t)
	m := &Manifest{Version: 1, Repos: []Repo{
		{Name: "fixture/repo", URL: origin, Rev: first},
		{Name: "absent/repo", URL: origin, Rev: first},
	}}

	before := c.Status(context.Background(), m)
	if before[0].Present || before[0].Ready {
		t.Errorf("uncached repo reported present: %+v", before[0])
	}

	if _, err := c.Ensure(context.Background(), m.Repos[0]); err != nil {
		t.Fatal(err)
	}
	after := c.Status(context.Background(), m)
	if !after[0].Present || !after[0].Ready {
		t.Errorf("cached repo not reported ready: %+v", after[0])
	}
	if after[0].At != first {
		t.Errorf("At = %q, want %q", after[0].At, first)
	}
	if after[1].Present {
		t.Errorf("second repo should still be absent: %+v", after[1])
	}
}

func TestDefaultCacheDirIsOutsideTheRepo(t *testing.T) {
	t.Setenv("LECTIO_CORPUS_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-test")
	if got := DefaultCacheDir(); got != "/tmp/xdg-test/lectio/corpus" {
		t.Errorf("DefaultCacheDir() = %q", got)
	}

	t.Setenv("LECTIO_CORPUS_DIR", "/explicit/override")
	if got := DefaultCacheDir(); got != "/explicit/override" {
		t.Errorf("override ignored: %q", got)
	}
}
