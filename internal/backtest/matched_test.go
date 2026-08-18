package backtest

import (
	"fmt"
	"testing"
)

func sizes(pairs map[string]int) map[string]int { return pairs }

// The property the whole measure rests on: within a pair the two candidates
// are the same size, so size alone cannot pick a winner.
func TestMatchedPairsAreCloseInSize(t *testing.T) {
	s := sizes(map[string]int{
		"touched.a": 100, "touched.b": 40,
		"other.1": 98, "other.2": 41, "other.3": 500, "other.4": 3,
	})
	got := BuildMatchedPairs([]string{"touched.a", "touched.b"}, s)

	if len(got) != 2 {
		t.Fatalf("built %d pairs, want 2: %+v", len(got), got)
	}
	for _, p := range got {
		if !withinRatio(s[p.Touched], s[p.Twin]) {
			t.Errorf("pair %s/%s is %d vs %d — outside %.2fx",
				p.Touched, p.Twin, s[p.Touched], s[p.Twin], MaxSizeRatio)
		}
	}
}

// A twin the contributor also touched is not a twin — it is a second positive,
// and pairing two positives makes the comparison meaningless.
func TestTwinsAreNeverInTheGroundTruth(t *testing.T) {
	s := sizes(map[string]int{"a": 50, "b": 51, "c": 52, "far": 5000})
	ground := []string{"a", "b", "c"}

	inGround := map[string]bool{"a": true, "b": true, "c": true}
	for _, p := range BuildMatchedPairs(ground, s) {
		if inGround[p.Twin] {
			t.Errorf("pair %s/%s uses a touched symbol as its twin", p.Touched, p.Twin)
		}
	}
}

// One unusual declaration must not stand in for half the ground truth, or the
// measure becomes a repeated comparison against a single symbol.
func TestEachTwinIsUsedOnce(t *testing.T) {
	s := map[string]int{"t1": 100, "t2": 100, "t3": 100, "u1": 100, "u2": 100, "u3": 100}
	got := BuildMatchedPairs([]string{"t1", "t2", "t3"}, s)

	seen := map[string]bool{}
	for _, p := range got {
		if seen[p.Twin] {
			t.Errorf("twin %s reused", p.Twin)
		}
		seen[p.Twin] = true
	}
	if len(got) != 3 {
		t.Errorf("built %d pairs from three exact matches, want 3", len(got))
	}
}

// When nothing close enough exists, the pair is dropped rather than stretched.
// A loose pair leaks the size information the measure removes.
func TestUnmatchablesAreDroppedNotStretched(t *testing.T) {
	s := map[string]int{"huge": 1000, "tiny.1": 5, "tiny.2": 6}
	got := BuildMatchedPairs([]string{"huge"}, s)
	if len(got) != 0 {
		t.Errorf("built %+v — a 1000-line symbol was paired with a 5-line one", got)
	}
}

// A symbol outside the candidate universe could not have been recommended, so
// scoring against it measures nothing about the ranking.
func TestUnsizedGroundTruthIsSkipped(t *testing.T) {
	s := map[string]int{"known": 50, "other": 51}
	got := BuildMatchedPairs([]string{"known", "vanished"}, s)
	if len(got) != 1 || got[0].Touched != "known" {
		t.Errorf("got %+v, want only the sized symbol paired", got)
	}
}

// Which symbols get the scarce close-sized twins must not depend on map
// iteration order.
func TestMatchedPairsAreDeterministic(t *testing.T) {
	s := map[string]int{}
	var ground []string
	for i := 0; i < 20; i++ {
		s[fmt.Sprintf("t%02d", i)] = 100 + i
		ground = append(ground, fmt.Sprintf("t%02d", i))
		s[fmt.Sprintf("u%02d", i)] = 100 + i
	}

	first := BuildMatchedPairs(ground, s)
	for i := 0; i < 50; i++ {
		got := BuildMatchedPairs(ground, s)
		if len(got) != len(first) {
			t.Fatalf("pair count moved: %d then %d", len(first), len(got))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("pair %d moved between runs: %+v then %+v", j, first[j], got[j])
			}
		}
	}
}

func TestScoreMatchedPairsCountsWins(t *testing.T) {
	pairs := []MatchedPair{
		{Touched: "a", Twin: "x"},
		{Touched: "b", Twin: "y"},
		{Touched: "c", Twin: "z"},
		{Touched: "d", Twin: "w"},
	}
	// Ranks a and b above their twins, c and d below.
	predicted := []string{"a", "x", "b", "y", "z", "c", "w", "d"}

	got, n := ScoreMatchedPairs(predicted, pairs)
	if n != 4 {
		t.Fatalf("scored %d pairs, want 4", n)
	}
	if got != 0.5 {
		t.Errorf("accuracy = %v, want 0.5", got)
	}
}

// Two symbols the ranking cannot separate is the null result this measure
// exists to detect. Scoring it as a loss would make an uninformative ranking
// look actively wrong.
func TestUnrankedPairsScoreAsChance(t *testing.T) {
	pairs := []MatchedPair{{Touched: "a", Twin: "x"}, {Touched: "b", Twin: "y"}}
	// Names neither side of either pair.
	got, _ := ScoreMatchedPairs([]string{"unrelated"}, pairs)
	if got != 0.5 {
		t.Errorf("accuracy = %v on a ranking that names nothing, want 0.5", got)
	}
}

// Declining to rank something is a prediction. A strategy naming only its
// favourites must not be scored on a subset of its own choosing.
func TestUnnamedSymbolsRankLastRatherThanBeingDropped(t *testing.T) {
	pairs := []MatchedPair{{Touched: "a", Twin: "x"}}

	// Names the twin and not the touched symbol: a loss, not a skip.
	if got, n := ScoreMatchedPairs([]string{"x"}, pairs); got != 0 || n != 1 {
		t.Errorf("accuracy = %v over %d pairs, want 0 over 1", got, n)
	}
	// Names the touched symbol and not the twin: a win.
	if got, _ := ScoreMatchedPairs([]string{"a"}, pairs); got != 1 {
		t.Errorf("accuracy = %v, want 1", got)
	}
}

func TestScoreMatchedPairsOnNoPairs(t *testing.T) {
	if got, n := ScoreMatchedPairs([]string{"a"}, nil); got != 0 || n != 0 {
		t.Errorf("got %v over %d pairs, want 0 over 0", got, n)
	}
}

func TestWithinRatio(t *testing.T) {
	for _, tc := range []struct {
		a, b int
		want bool
	}{
		{100, 100, true},
		{100, 125, true},
		{125, 100, true},
		{100, 126, false},
		{0, 100, false},
		{100, 0, false},
		{-5, 5, false},
	} {
		if got := withinRatio(tc.a, tc.b); got != tc.want {
			t.Errorf("withinRatio(%d, %d) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// A case the pairing could not reach has not been measured. Averaging its
// absence as zero would report every strategy far below chance in proportion
// to how often that happened, which is a fact about the corpus rather than
// about any ranking.
func TestUnpairedCasesDoNotDragTheAverageDown(t *testing.T) {
	results := []CaseResult{
		{
			Case:   Case{Repo: "r", RewindTo: "a", Contributor: "x"},
			Scores: []Score{{Strategy: "lectio", Precision: 0.2, Matched: 0.6, Pairs: 10}},
		},
		{
			// Same case shape, but the pairing found nothing usable.
			Case:   Case{Repo: "r", RewindTo: "b", Contributor: "y"},
			Scores: []Score{{Strategy: "lectio", Precision: 0.2, Matched: 0, Pairs: 0}},
		},
	}
	rep := Summarize(results, 10)

	var got float64
	for _, a := range rep.Aggregates {
		if a.Strategy == "lectio" {
			got = a.MatchedA
		}
	}
	if got != 0.6 {
		t.Errorf("MatchedA = %v, want 0.6 — the unpaired case was averaged in as zero", got)
	}
	if rep.MatchedCases != 1 {
		t.Errorf("MatchedCases = %d, want 1", rep.MatchedCases)
	}
	if rep.MatchedPairs != 10 {
		t.Errorf("MatchedPairs = %d, want 10", rep.MatchedPairs)
	}
}

// Pairs and cases are different numbers and the caption reports both. Calling
// two cases 'two pairs' understates the evidence by an order of magnitude.
func TestMatchedPairsCountsPairsNotCases(t *testing.T) {
	var results []CaseResult
	for i := 0; i < 3; i++ {
		results = append(results, CaseResult{
			Case:   Case{Repo: "r", RewindTo: string(rune('a' + i)), Contributor: "x"},
			Scores: []Score{{Strategy: "lectio", Matched: 0.5, Pairs: 7}},
		})
	}
	rep := Summarize(results, 10)

	if rep.MatchedPairs != 21 {
		t.Errorf("MatchedPairs = %d, want 21 (3 cases x 7 pairs)", rep.MatchedPairs)
	}
	if rep.MatchedCases != 3 {
		t.Errorf("MatchedCases = %d, want 3", rep.MatchedCases)
	}
}

// A file-target run produces no pairs, and the report must not imply a control
// it never applied.
func TestFileTargetsProduceNoMatchedColumn(t *testing.T) {
	rep := Summarize([]CaseResult{{
		Case:   Case{Repo: "r", RewindTo: "a", Contributor: "x"},
		Target: TargetTouched,
		Scores: []Score{{Strategy: "lectio", Precision: 0.4}},
	}}, 10)

	if rep.MatchedPairs != 0 || rep.MatchedCases != 0 {
		t.Errorf("a file target reported %d pairs over %d cases", rep.MatchedPairs, rep.MatchedCases)
	}
}
