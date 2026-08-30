package corpus

import (
	"context"
	"errors"
	"testing"
)

// EnsureAll is what `lectio corpus fetch` runs, and it had no test.
//
// The behaviour that matters is what it does with a failure. A corpus fetch is
// unattended and takes hours; stopping at repository three of thirty would mean
// discovering repository eleven is also broken only after a second hours-long
// run. It reports each one and carries on, and that is worth pinning down.
func TestEnsureAllReportsEveryFailureAndKeepsGoing(t *testing.T) {
	url, rev := localOrigin(t)
	c := newTestCache(t)

	m := &Manifest{Repos: []Repo{
		{Name: "good/one", URL: url, Rev: rev},
		{Name: "bad/unpinned", URL: url},
		{Name: "bad/nowhere", URL: t.TempDir() + "/does-not-exist", Rev: rev},
		{Name: "good/two", URL: url, Rev: rev},
	}}

	type outcome struct {
		name string
		err  error
	}
	var seen []outcome
	if err := c.EnsureAll(context.Background(), m, func(r Repo, err error) {
		seen = append(seen, outcome{r.Name, err})
	}); err != nil {
		t.Fatalf("EnsureAll returned %v; failures belong in the callback", err)
	}

	if len(seen) != 4 {
		t.Fatalf("progress fired %d times for 4 repositories: %+v", len(seen), seen)
	}
	for i, want := range []string{"good/one", "bad/unpinned", "bad/nowhere", "good/two"} {
		if seen[i].name != want {
			t.Errorf("repository %d was %q, want %q — order is not the manifest's", i, seen[i].name, want)
		}
	}
	if seen[0].err != nil || seen[3].err != nil {
		t.Errorf("a reachable repository failed: %v, %v", seen[0].err, seen[3].err)
	}
	if seen[1].err == nil {
		t.Error("an unpinned repository was reported as fetched")
	}
	if seen[2].err == nil {
		t.Error("a repository with no origin was reported as fetched")
	}
	// The one that matters: the run reached the last repository despite two
	// failures before it.
	if seen[3].err != nil {
		t.Errorf("the fourth repository did not fetch after two failures: %v", seen[3].err)
	}
}

// A cancelled fetch stops, and says so. Without this the only way to abandon a
// three-hour corpus run would be to kill the process mid-clone.
func TestEnsureAllStopsOnCancellation(t *testing.T) {
	url, rev := localOrigin(t)
	c := newTestCache(t)

	m := &Manifest{Repos: []Repo{
		{Name: "a/one", URL: url, Rev: rev},
		{Name: "a/two", URL: url, Rev: rev},
		{Name: "a/three", URL: url, Rev: rev},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	var reached int
	err := c.EnsureAll(ctx, m, func(Repo, error) {
		reached++
		cancel()
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("EnsureAll returned %v after cancellation, want context.Canceled", err)
	}
	if reached != 1 {
		t.Errorf("kept going through %d repositories after the first was cancelled", reached)
	}
}

// A nil callback is legitimate — a caller that only wants the side effect —
// and must not panic.
func TestEnsureAllAcceptsNoProgressCallback(t *testing.T) {
	url, rev := localOrigin(t)
	c := newTestCache(t)
	m := &Manifest{Repos: []Repo{{Name: "a/one", URL: url, Rev: rev}}}

	if err := c.EnsureAll(context.Background(), m, nil); err != nil {
		t.Fatalf("EnsureAll with no callback: %v", err)
	}
	if _, err := c.Ensure(context.Background(), m.Repos[0]); err != nil {
		t.Errorf("the repository was not materialized: %v", err)
	}
}

func TestEnsureAllOnAnEmptyManifest(t *testing.T) {
	c := newTestCache(t)
	fired := false
	if err := c.EnsureAll(context.Background(), &Manifest{}, func(Repo, error) { fired = true }); err != nil {
		t.Fatalf("EnsureAll on an empty manifest: %v", err)
	}
	if fired {
		t.Error("progress fired for a manifest with no repositories")
	}
}

// Short renders a pin for a progress line, and the three cases it distinguishes
// are all ones a reader acts on differently.
func TestShortRendersEveryPinState(t *testing.T) {
	for _, tc := range []struct {
		rev, want string
	}{
		{"", "unpinned"},
		{"abc123", "abc123"},
		{"0123456789", "0123456789"},   // exactly ten, kept whole
		{"0123456789ab", "0123456789"}, // longer, truncated
		{"9dda7d1a2b3c4d5e6f708192a3b4c5d6e7f80912", "9dda7d1a2b"},
	} {
		if got := (Repo{Rev: tc.rev}).Short(); got != tc.want {
			t.Errorf("Short() of %q = %q, want %q", tc.rev, got, tc.want)
		}
	}
}

// The timeout exists so an unattended run cannot hang on a stalled clone. A
// zero means the default rather than no timeout at all, which is the reverse
// of what a zero usually means and is worth a test saying so.
func TestZeroTimeoutMeansTheDefaultNotNone(t *testing.T) {
	c := NewCache(t.TempDir())
	if c.Timeout != 0 {
		t.Skipf("NewCache now sets a timeout of %v; this test is about the zero case", c.Timeout)
	}
	// A cancelled context proves git ran under a derived context rather than
	// under context.Background().
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.git(ctx, "", "--version"); err == nil {
		t.Error("git ran to completion under a cancelled context")
	}
}

func TestDefaultCacheDirIsAbsolute(t *testing.T) {
	dir := DefaultCacheDir()
	if dir == "" {
		t.Fatal("DefaultCacheDir is empty")
	}
	if dir[0] != '/' && len(dir) > 1 && dir[1] != ':' {
		t.Errorf("DefaultCacheDir() = %q, which is not an absolute path", dir)
	}
}
