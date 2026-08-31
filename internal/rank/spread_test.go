package rank

import (
	"fmt"
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

// oneFile builds n scored items in a single file, descending by score.
func oneFile(file string, n int, top float64) []Item {
	out := make([]Item, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Item{
			Symbol: core.Symbol{
				ID:   core.SymbolID(fmt.Sprintf("%s.Sym%02d", file, i)),
				File: file,
			},
			Score: top - float64(i)*0.001,
		})
	}
	return out
}

// resultOf builds a Result whose Items are score-ordered, as Rank guarantees.
func resultOf(groups ...[]Item) *Result {
	var all []Item
	for _, g := range groups {
		all = append(all, g...)
	}
	sortByScore(all)
	return &Result{Items: all}
}

func filesOf(items []Item) map[string]int {
	out := map[string]int{}
	for _, it := range items {
		out[it.Symbol.File]++
	}
	return out
}

// The bug, in its own shape. Four of the seven signals are per-file, so every
// declaration in a heavily-worked file inherits four identical contributions
// and its symbols take the whole list on merit.
func TestSelectSpreadBreaksUpASingleFile(t *testing.T) {
	r := resultOf(
		oneFile("hot.go", 40, 0.90),
		oneFile("other.go", 20, 0.60),
		oneFile("third.go", 20, 0.50),
	)

	plain := r.Select(10)
	if got := filesOf(plain); got["hot.go"] != 10 {
		t.Fatalf("Select gave %v; the fixture is meant to be dominated by one file", got)
	}

	spread := r.SelectSpread(10)
	if len(spread) != 10 {
		t.Fatalf("SelectSpread returned %d items, want 10", len(spread))
	}
	if got := filesOf(spread)["hot.go"]; got > 4 {
		t.Errorf("hot.go took %d of 10, above the cap of 4", got)
	}
	if Files(spread) < 3 {
		t.Errorf("a path of 10 spans %d files, want at least 3", Files(spread))
	}
}

// The cap is a preference, not a limit. A repository whose interesting code
// genuinely is one file should still get a full list — truncating it would be
// the tool refusing to answer because the answer is concentrated.
func TestSelectSpreadStillFillsTheListFromOneFile(t *testing.T) {
	r := resultOf(oneFile("only.go", 30, 0.8))

	got := r.SelectSpread(10)
	if len(got) != 10 {
		t.Fatalf("returned %d items from a one-file repository, want 10", len(got))
	}
	if n := filesOf(got)["only.go"]; n != 10 {
		t.Errorf("only.go contributed %d of 10 when it is the only file", n)
	}
}

// The refusal Select documents is unchanged: a symbol no signal had anything
// to say about is not a recommendation, and padding to reach n would make it
// one.
func TestSelectSpreadNeverPadsWithZeroScores(t *testing.T) {
	items := oneFile("a.go", 3, 0.5)
	items = append(items, oneFile("b.go", 20, 0)...)
	r := resultOf(items)

	got := r.SelectSpread(10)
	if len(got) != 3 {
		t.Fatalf("returned %d items when only 3 scored anything", len(got))
	}
	for _, it := range got {
		if it.Score <= 0 {
			t.Errorf("%s scored %v and was recommended anyway", it.Symbol.ID, it.Score)
		}
	}
}

// Downstream — tiering especially — assumes a score-sorted list. The first
// pass emits items out of order by construction, so the restore is
// load-bearing.
func TestSelectSpreadReturnsScoreOrder(t *testing.T) {
	r := resultOf(
		oneFile("hot.go", 20, 0.90),
		oneFile("cool.go", 20, 0.55),
	)
	got := r.SelectSpread(10)
	for i := 1; i < len(got); i++ {
		if got[i].Score > got[i-1].Score {
			t.Fatalf("item %d scores %.4f, above item %d's %.4f", i, got[i].Score, i-1, got[i-1].Score)
		}
	}
}

func TestSelectSpreadIsDeterministic(t *testing.T) {
	r := resultOf(
		oneFile("a.go", 15, 0.9),
		oneFile("b.go", 15, 0.9), // identical scores, so ties decide everything
		oneFile("c.go", 15, 0.9),
	)
	first := r.SelectSpread(10)
	for i := 0; i < 20; i++ {
		got := r.SelectSpread(10)
		if len(got) != len(first) {
			t.Fatalf("length changed between runs: %d then %d", len(first), len(got))
		}
		for j := range got {
			if got[j].Symbol.ID != first[j].Symbol.ID {
				t.Fatalf("reordered between runs at %d: %s then %s",
					j, first[j].Symbol.ID, got[j].Symbol.ID)
			}
		}
	}
}

func TestMaxPerFileScalesWithTheList(t *testing.T) {
	for _, tc := range []struct{ n, want int }{
		{0, 0},
		// Under the floor: a third of a short list rounds to one, which would
		// forbid any two items from sharing a file.
		{1, 2}, {2, 2}, {3, 2}, {4, 2}, {5, 2}, {6, 2},
		// Past it, the share governs.
		{10, 4}, {20, 7}, {30, 10},
	} {
		if got := maxPerFile(tc.n); got != tc.want {
			t.Errorf("maxPerFile(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
	if got := maxPerFile(-1); got != 0 {
		t.Errorf("maxPerFile(-1) = %d, want 0", got)
	}
}

func TestSelectSpreadOnNothing(t *testing.T) {
	if got := (&Result{}).SelectSpread(10); len(got) != 0 {
		t.Errorf("got %d items from an empty result", len(got))
	}
	if got := resultOf(oneFile("a.go", 5, 0.5)).SelectSpread(0); got != nil {
		t.Errorf("got %d items for a list of zero", len(got))
	}
}

func TestFilesCountsDistinctFiles(t *testing.T) {
	items := append(oneFile("a.go", 3, 0.5), oneFile("b.go", 2, 0.4)...)
	if got := Files(items); got != 2 {
		t.Errorf("Files() = %d, want 2", got)
	}
	if got := Files(nil); got != 0 {
		t.Errorf("Files(nil) = %d, want 0", got)
	}
}

// Path is where the cap has to apply, because Path is the reading path. Select
// keeps its old meaning for callers that want the scores in order.
func TestPathSpreadsAndSelectDoesNot(t *testing.T) {
	r := resultOf(
		oneFile("hot.go", 40, 0.90),
		oneFile("other.go", 20, 0.60),
		oneFile("third.go", 20, 0.50),
	)

	if got := filesOf(r.Select(10)); got["hot.go"] != 10 {
		t.Errorf("Select changed behaviour: %v", got)
	}
	if got := filesOf(r.Path(10)); got["hot.go"] > 4 {
		t.Errorf("Path took %d of 10 from one file", got["hot.go"])
	}
}

// Two files and a list of ten cannot both be satisfied: the cap is four each,
// which fills eight. The remaining two come from the highest-scoring items the
// cap skipped, which degrades toward the plain ranking rather than toward an
// arbitrary redistribution — the conservative direction, and the same one the
// one-file case takes.
func TestSelectSpreadDegradesTowardTheRankingWhenTheCapCannotBeMet(t *testing.T) {
	r := resultOf(
		oneFile("hot.go", 40, 0.90),
		oneFile("other.go", 20, 0.60),
	)

	got := r.SelectSpread(10)
	if len(got) != 10 {
		t.Fatalf("returned %d items, want a full list", len(got))
	}
	files := filesOf(got)
	if files["other.go"] != 4 {
		t.Errorf("other.go took %d, want the cap of 4", files["other.go"])
	}
	if files["hot.go"] != 6 {
		t.Errorf("hot.go took %d, want the 4 it is capped at plus the 2 the fallback adds",
			files["hot.go"])
	}
	// The two extras are the highest-scoring skipped items, not arbitrary ones.
	for _, it := range got {
		if it.Symbol.File != "hot.go" {
			continue
		}
		if it.Score < 0.894 {
			t.Errorf("the fallback took %s at %.4f rather than the highest skipped",
				it.Symbol.ID, it.Score)
		}
	}
}

// The case that prompted the floor. At three items a third rounds to one, and
// "these two functions here, plus this one over there" is a perfectly good
// three-item path that a one-per-file cap would refuse.
func TestAShortPathMayKeepACoherentPair(t *testing.T) {
	r := resultOf(
		oneFile("core.go", 10, 0.95),
		oneFile("edge.go", 10, 0.60),
	)
	got := r.SelectSpread(3)
	if len(got) != 3 {
		t.Fatalf("returned %d items, want 3", len(got))
	}
	if n := filesOf(got)["core.go"]; n != 2 {
		t.Errorf("core.go contributed %d of 3, want the 2 the floor allows", n)
	}
	if Files(got) != 2 {
		t.Errorf("a three-item path spans %d files, want 2", Files(got))
	}
}

// And the floor must not undo the fix: ten items still cannot come from one
// file.
func TestTheFloorDoesNotReopenTheOriginalBug(t *testing.T) {
	r := resultOf(
		oneFile("hot.go", 40, 0.90),
		oneFile("other.go", 20, 0.60),
		oneFile("third.go", 20, 0.50),
	)
	if n := filesOf(r.SelectSpread(10))["hot.go"]; n > 4 {
		t.Errorf("hot.go took %d of 10", n)
	}
}
