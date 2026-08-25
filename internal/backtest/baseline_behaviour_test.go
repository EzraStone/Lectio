package backtest

import (
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
)

// baselineView builds a view whose history makes each baseline's answer
// different from the others, so a test can tell them apart.
func baselineView(now time.Time) *index.View {
	day := 24 * time.Hour
	v := &index.View{
		Now: now,
		Symbols: map[core.SymbolID]core.Symbol{
			// big.go is the largest by line span.
			"p.Big":    {ID: "p.Big", File: "big.go", StartLine: 1, EndLine: 300},
			"p.Hot":    {ID: "p.Hot", File: "hot.go", StartLine: 1, EndLine: 20},
			"p.Fresh":  {ID: "p.Fresh", File: "fresh.go", StartLine: 1, EndLine: 10},
			"p.Shared": {ID: "p.Shared", File: "shared.go", StartLine: 1, EndLine: 15},
			// A test file, which no baseline should ever recommend.
			"p.TestBig": {ID: "p.TestBig", File: "big_test.go", StartLine: 1, EndLine: 500},
		},
		Commits: []core.Commit{
			// hot.go: many commits, all by one author, none recent.
			{Hash: "c1", AuthorEmail: "a@x", When: now.Add(-300 * day), Files: []core.FileChange{{Path: "hot.go"}}},
			{Hash: "c2", AuthorEmail: "a@x", When: now.Add(-280 * day), Files: []core.FileChange{{Path: "hot.go"}}},
			{Hash: "c3", AuthorEmail: "a@x", When: now.Add(-260 * day), Files: []core.FileChange{{Path: "hot.go"}}},
			{Hash: "c4", AuthorEmail: "a@x", When: now.Add(-240 * day), Files: []core.FileChange{{Path: "hot.go"}}},
			// shared.go: few commits, many distinct authors.
			{Hash: "c5", AuthorEmail: "b@x", When: now.Add(-200 * day), Files: []core.FileChange{{Path: "shared.go"}}},
			{Hash: "c6", AuthorEmail: "c@x", When: now.Add(-190 * day), Files: []core.FileChange{{Path: "shared.go"}}},
			{Hash: "c7", AuthorEmail: "d@x", When: now.Add(-180 * day), Files: []core.FileChange{{Path: "shared.go"}}},
			// fresh.go: one commit, yesterday.
			{Hash: "c8", AuthorEmail: "e@x", When: now.Add(-1 * day), Files: []core.FileChange{{Path: "fresh.go"}}},
			// big.go: one old commit.
			{Hash: "c9", AuthorEmail: "f@x", When: now.Add(-350 * day), Files: []core.FileChange{{Path: "big.go"}}},
		},
	}
	return v
}

func baselineParams(now time.Time) rank.Params {
	p := rank.DefaultParams()
	p.Now = now
	return p
}

func first(t *testing.T, got []string) string {
	t.Helper()
	if len(got) == 0 {
		t.Fatal("ranking is empty")
	}
	return got[0]
}

// The baseline that won the first run at scale. Its definition is
// load-bearing, and it had no test.
func TestLargestFilesRanksBySpan(t *testing.T) {
	now := time.Now()
	got := LargestFiles{}.RankFiles(baselineView(now), baselineParams(now))

	if f := first(t, got); f != "big.go" {
		t.Errorf("largest file is %q, want big.go", f)
	}
	for _, f := range got {
		if f == "big_test.go" {
			t.Error("a test file reached the largest-files ranking — it is the largest by span")
		}
	}
}

func TestMostChurnedCountsCommitsInTheWindow(t *testing.T) {
	now := time.Now()
	got := MostChurned{}.RankFiles(baselineView(now), baselineParams(now))

	if f := first(t, got); f != "hot.go" {
		t.Errorf("most churned is %q, want hot.go", f)
	}
}

// The window is anchored on Params.Now, which is what lets the backtest stand
// at a historical date. Reading the wall clock here is the bug that once made
// every history baseline score 0.0%.
func TestMostChurnedRespectsTheWindowAnchor(t *testing.T) {
	now := time.Now()
	v := baselineView(now)

	// Stand two years after the commits: nothing is inside a twelve-month
	// window any more.
	late := baselineParams(now.Add(2 * 365 * 24 * time.Hour))
	churned := MostChurned{}
	if got := churned.RankFiles(v, late); len(got) != 0 {
		t.Errorf("got %v from a window containing no commits", got)
	}

	// Stand at the present: the commits are visible again.
	if got := churned.RankFiles(v, baselineParams(now)); len(got) == 0 {
		t.Error("no files ranked when the window does contain commits")
	}
}

func TestMostRecentRanksByLastTouch(t *testing.T) {
	now := time.Now()
	got := MostRecent{}.RankFiles(baselineView(now), baselineParams(now))

	if f := first(t, got); f != "fresh.go" {
		t.Errorf("most recent is %q, want fresh.go", f)
	}
}

func TestMostAuthorsCountsDistinctContributors(t *testing.T) {
	now := time.Now()
	got := MostAuthors{}.RankFiles(baselineView(now), baselineParams(now))

	if f := first(t, got); f != "shared.go" {
		t.Errorf("most authors is %q, want shared.go — hot.go has more commits but one author", f)
	}
}

// A file with history but no indexed symbols cannot be recommended: nothing in
// it could be read. Every history baseline has to filter to the indexed set or
// it would rank documentation and lockfiles.
func TestBaselinesOnlyRankIndexedFiles(t *testing.T) {
	now := time.Now()
	v := baselineView(now)
	v.Commits = append(v.Commits, core.Commit{
		Hash: "c10", AuthorEmail: "g@x", When: now,
		Files: []core.FileChange{{Path: "README.md"}, {Path: "go.sum"}},
	})

	for _, b := range Baselines() {
		for _, f := range b.RankFiles(v, baselineParams(now)) {
			if f == "README.md" || f == "go.sum" {
				t.Errorf("%s ranked %q, which has no indexed symbols", b.Name(), f)
			}
		}
	}
}

// Every ordering here breaks ties deterministically, or a Gate A number moves
// between runs for no reason anyone can see.
func TestBaselineOrderingIsDeterministic(t *testing.T) {
	now := time.Now()
	v := baselineView(now)

	for _, b := range Baselines() {
		firstRun := b.RankFiles(v, baselineParams(now))
		for i := 0; i < 30; i++ {
			got := b.RankFiles(v, baselineParams(now))
			if len(got) != len(firstRun) {
				t.Fatalf("%s changed length between runs", b.Name())
			}
			for j := range got {
				if got[j] != firstRun[j] {
					t.Fatalf("%s reordered between runs: %v then %v", b.Name(), firstRun, got)
				}
			}
		}
	}
}

// The four the gate is decided against, named and distinct.
func TestBaselinesAreTheSpecsFour(t *testing.T) {
	got := Baselines()
	if len(got) != 4 {
		t.Fatalf("got %d baselines, want the spec's 4", len(got))
	}
	seen := map[string]bool{}
	for _, b := range got {
		if b.Name() == "" {
			t.Error("a baseline has no name")
		}
		if seen[b.Name()] {
			t.Errorf("duplicate baseline %q", b.Name())
		}
		seen[b.Name()] = true
	}
}
