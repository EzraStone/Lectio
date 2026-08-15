package corpus

import (
	"context"
	"testing"
)

// An unpinned entry is not a broken pin, and telling someone their corpus is
// broken when it merely has not been pinned yet sends them re-pinning a corpus
// that was fine — which changes every number computed over it.
func TestVerifyDistinguishesUnpinnedFromBroken(t *testing.T) {
	m := &Manifest{Repos: []Repo{{Name: "a/b", URL: "https://example.com/a/b", Rev: ""}}}
	got := Verify(context.Background(), NewCache(t.TempDir()), m)

	if len(got) != 1 {
		t.Fatalf("verified %d repos, want 1", len(got))
	}
	if got[0].Reachable {
		t.Error("an unpinned repo reported as reachable")
	}
	if got[0].Reason == "" {
		t.Error("no reason given for an unpinned repo")
	}
}

// A local clone is the only place a pin to a non-tip commit can be confirmed,
// and a missing clone must read as unconfirmed rather than as a broken pin.
func TestVerifyLocalReportsAMissingClone(t *testing.T) {
	r := Repo{Name: "a/b", URL: "https://example.com/a/b", Rev: "0123456789abcdef0123456789abcdef01234567"}
	got := VerifyLocal(context.Background(), NewCache(t.TempDir()), r)

	if got.Reachable {
		t.Error("a repo with no clone reported as verified")
	}
	if got.Reason == "" {
		t.Error("no reason given")
	}
}

func TestVerifyLocalSkipsUnpinned(t *testing.T) {
	got := VerifyLocal(context.Background(), NewCache(t.TempDir()), Repo{Name: "a/b"})
	if got.Reachable || got.Reason != "not pinned" {
		t.Errorf("got %+v", got)
	}
}

func TestFirstLine(t *testing.T) {
	for in, want := range map[string]string{
		"one\ntwo\nthree": "one",
		"only":            "only",
		"":                "",
	} {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
