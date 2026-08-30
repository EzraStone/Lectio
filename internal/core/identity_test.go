package core

import (
	"testing"
	"time"
)

// Author identity is what orphaning counts. Splitting one person into two by
// casing would make a well-maintained file look abandoned by half its owners,
// and merging two people into one would do the reverse — so the folding rule
// is load-bearing in both directions and had no test.
func TestAuthorFoldsCasing(t *testing.T) {
	same := []Commit{
		{AuthorEmail: "Ada@Example.COM"},
		{AuthorEmail: "ada@example.com"},
		{AuthorEmail: "ADA@EXAMPLE.COM"},
	}
	want := same[0].Author()
	for _, c := range same[1:] {
		if got := c.Author(); got != want {
			t.Errorf("%q folded to %q, want %q", c.AuthorEmail, got, want)
		}
	}
	if want != "ada@example.com" {
		t.Errorf("folded identity is %q, want it lower-cased", want)
	}
}

// Email over display name, because display names change and emails mostly do
// not. A rename would otherwise read as a departure.
func TestAuthorPrefersEmailOverName(t *testing.T) {
	c := Commit{AuthorName: "Ada Lovelace", AuthorEmail: "ada@example.com"}
	if got := c.Author(); got != "ada@example.com" {
		t.Errorf("Author() = %q, want the email", got)
	}

	renamed := Commit{AuthorName: "A. Lovelace", AuthorEmail: "ada@example.com"}
	if renamed.Author() != c.Author() {
		t.Error("a display-name change split one identity into two")
	}
}

// Some histories carry no email at all — older repositories, and some import
// tools. Falling back to the name is better than folding every such commit
// into one empty identity.
func TestAuthorFallsBackToTheName(t *testing.T) {
	c := Commit{AuthorName: "Ada Lovelace"}
	if got := c.Author(); got != "ada lovelace" {
		t.Errorf("Author() = %q, want the lower-cased name", got)
	}
	other := Commit{AuthorName: "Grace Hopper"}
	if other.Author() == c.Author() {
		t.Error("two named authors with no email folded together")
	}
}

func TestAuthorOfAnEmptyCommitIsEmpty(t *testing.T) {
	if got := (Commit{}).Author(); got != "" {
		t.Errorf("Author() = %q on a commit with neither field", got)
	}
}

func TestPathsListsWhatTheCommitTouched(t *testing.T) {
	c := Commit{Files: []FileChange{
		{Path: "a.go", Added: 3},
		{Path: "b.go", Deleted: 1},
		{Path: "c.go", Renamed: "old.go"},
	}}
	got := c.Paths()
	if len(got) != 3 {
		t.Fatalf("got %d paths from 3 changes: %v", len(got), got)
	}
	for i, want := range []string{"a.go", "b.go", "c.go"} {
		if got[i] != want {
			t.Errorf("path %d is %q, want %q", i, got[i], want)
		}
	}
}

// A rename reports the new path. History follows the file forward; the old
// name is on the change for anyone reconstructing the move.
func TestPathsReportsTheNewNameOfARename(t *testing.T) {
	c := Commit{Files: []FileChange{{Path: "new.go", Renamed: "old.go"}}}
	if got := c.Paths(); len(got) != 1 || got[0] != "new.go" {
		t.Errorf("Paths() = %v, want the post-rename path", got)
	}
}

func TestPathsOfACommitTouchingNothing(t *testing.T) {
	if got := (Commit{}).Paths(); len(got) != 0 {
		t.Errorf("Paths() = %v on a commit with no files", got)
	}
}

func TestDistinctAuthorsCountsPeopleNotCommits(t *testing.T) {
	h := FileHistory{
		Path:    "hot.go",
		Commits: 40,
		Authors: map[string]int{"ada@x": 30, "grace@x": 9, "alan@x": 1},
	}
	if got := h.DistinctAuthors(); got != 3 {
		t.Errorf("DistinctAuthors() = %d, want 3 — 40 commits by 3 people", got)
	}
	if got := (FileHistory{Path: "new.go"}).DistinctAuthors(); got != 0 {
		t.Errorf("DistinctAuthors() = %d on a file with no history", got)
	}
}

// The threshold is not decoration. Go's coverage profile attributes a
// statement to every test binary that executed it, so a package's init path
// shows up under every test in the package — at a fraction near zero.
func TestCoverageThresholdRejectsIncidentalExecution(t *testing.T) {
	for _, tc := range []struct {
		fraction float64
		want     bool
	}{
		{0, false},
		{0.01, false}, // one line of a hundred: initialization
		{0.09, false}, // just under
		{0.10, true},  // the threshold itself counts
		{0.5, true},
		{1.0, true},
	} {
		e := CoverageEdge{Test: "p.TestFoo", Symbol: "p.Foo", Fraction: tc.fraction}
		if got := e.Covers(); got != tc.want {
			t.Errorf("Covers() = %v at fraction %.2f, want %v", got, tc.fraction, tc.want)
		}
	}
}

// Nothing in the pipeline should produce these, but a fraction outside [0,1]
// must not read as coverage by accident.
func TestCoverageRejectsANegativeFraction(t *testing.T) {
	if (CoverageEdge{Fraction: -0.5}).Covers() {
		t.Error("a negative fraction counted as coverage")
	}
}

// A file's history spans a window, and both ends are used: FirstTouch decides
// whether a file predates a contributor, LastTouch feeds recency.
func TestFileHistoryCarriesBothEndsOfItsWindow(t *testing.T) {
	now := time.Now()
	h := FileHistory{
		Path:       "a.go",
		FirstTouch: now.Add(-365 * 24 * time.Hour),
		LastTouch:  now,
	}
	if !h.FirstTouch.Before(h.LastTouch) {
		t.Error("a history whose first touch is not before its last")
	}
}
