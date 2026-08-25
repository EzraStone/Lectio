package backtest

import (
	"fmt"
	"testing"
)

// sweepFixture builds a size table where the pairing ratio decides how many
// ground-truth entries can find a twin.
//
// Sizes grow geometrically so that each touched entry's nearest candidate is
// its own designated twin. An arithmetic ladder does not work here: with sizes
// at 100, 200, 300 and twins at 140, 280, 420, the entry at 300 finds 280 —
// someone else's twin, 1.07x away — and the fixture stops testing the ratio.
func sweepFixture() (ground []string, sizes map[string]int) {
	sizes = map[string]int{}
	base := 100
	for i := 0; i < 12; i++ {
		touched := fmt.Sprintf("t%02d", i)
		twin := fmt.Sprintf("x%02d", i)
		sizes[touched] = base
		// A twin 40% larger: out of reach at 1.25x, in reach at 1.5x.
		sizes[twin] = base * 140 / 100
		ground = append(ground, touched)
		base *= 3
	}
	return ground, sizes
}

func TestScoreRatiosWalksTheLadder(t *testing.T) {
	ground, sizes := sweepFixture()
	// A ranking that always puts the touched symbol first.
	oracle := append([]string(nil), ground...)
	predicted := map[string][]string{"oracle": oracle}

	got := scoreRatios(predicted, ground, sizes, []SizeRatio{1.25, 1.5, 2.0})

	seen := map[SizeRatio]int{}
	for _, rs := range got {
		seen[rs.Ratio] = rs.Pairs
		if rs.Matched != 1 {
			t.Errorf("at %.2fx the oracle scored %.3f, want 1.0", float64(rs.Ratio), rs.Matched)
		}
	}
	if _, ok := seen[1.25]; ok {
		t.Error("1.25x produced pairs on a fixture whose twins are 1.4x away")
	}
	if seen[1.5] != 12 || seen[2.0] != 12 {
		t.Errorf("got %d pairs at 1.5x and %d at 2.0x, want 12 each", seen[1.5], seen[2.0])
	}
}

// A ratio reaching fewer than MinPairs in a case has not measured that case,
// and recording it would fill the tightest column with cells decided by one
// pair each.
func TestScoreRatiosAppliesTheMinPairsFloor(t *testing.T) {
	sizes := map[string]int{"a": 100, "twin": 105, "b": 900, "far": 5000}
	ground := []string{"a", "b"}
	got := scoreRatios(map[string][]string{"s": {"a", "b"}}, ground, sizes, []SizeRatio{1.1, 8.0})

	for _, rs := range got {
		if rs.Pairs < MinPairs {
			t.Errorf("recorded a cell with %d pairs, below the floor of %d", rs.Pairs, MinPairs)
		}
	}
	if len(got) != 0 {
		t.Errorf("got %d cells from a fixture that can never reach %d pairs", len(got), MinPairs)
	}
}

func TestScoreRatiosIsEmptyWithoutRatios(t *testing.T) {
	ground, sizes := sweepFixture()
	if got := scoreRatios(map[string][]string{"s": ground}, ground, sizes, nil); got != nil {
		t.Errorf("got %d cells with no ratios asked for", len(got))
	}
	if got := scoreRatios(nil, ground, sizes, SweepRatios); got != nil {
		t.Errorf("got %d cells with no strategies", len(got))
	}
}

// The sweep is only worth having if every strategy faces the same pairs at a
// given ratio. Two strategies scored against different pairings would produce
// a column that compares nothing.
func TestScoreRatiosGivesEveryStrategyTheSamePairs(t *testing.T) {
	ground, sizes := sweepFixture()
	forward := append([]string(nil), ground...)
	backward := make([]string, len(ground))
	for i := range ground {
		backward[i] = ground[len(ground)-1-i]
	}

	got := scoreRatios(map[string][]string{"a": forward, "b": backward}, ground, sizes, []SizeRatio{1.5, 2.0})
	byRatio := map[SizeRatio]map[string]int{}
	for _, rs := range got {
		if byRatio[rs.Ratio] == nil {
			byRatio[rs.Ratio] = map[string]int{}
		}
		byRatio[rs.Ratio][rs.Strategy] = rs.Pairs
	}
	for ratio, m := range byRatio {
		if m["a"] != m["b"] {
			t.Errorf("at %.2fx strategy a faced %d pairs and b faced %d", float64(ratio), m["a"], m["b"])
		}
	}
}

func sweepResults() []CaseResult {
	var out []CaseResult
	for i := 0; i < 8; i++ {
		r := CaseResult{Case: Case{Repo: fmt.Sprintf("repo%d", i%4)}}
		for _, ratio := range []SizeRatio{1.25, 2.0} {
			r.Ratios = append(r.Ratios,
				RatioScore{Ratio: ratio, Strategy: "lectio", Matched: 0.5 + float64(i)*0.01, Pairs: 10},
				RatioScore{Ratio: ratio, Strategy: "largest files", Matched: 0.5 - float64(i)*0.01, Pairs: 10},
			)
		}
		out = append(out, r)
	}
	return out
}

func TestSummarizeRatiosFollowsTheMainTablesOrder(t *testing.T) {
	aggs := summarizeRatios(sweepResults(), []string{"lectio", "largest files"})
	if len(aggs) != 4 {
		t.Fatalf("got %d cells, want 2 ratios x 2 strategies", len(aggs))
	}
	// Ratio ascending, then strategy in the given order.
	want := []struct {
		ratio SizeRatio
		name  string
	}{
		{1.25, "lectio"}, {1.25, "largest files"},
		{2.0, "lectio"}, {2.0, "largest files"},
	}
	for i, w := range want {
		if aggs[i].Ratio != w.ratio || aggs[i].Strategy != w.name {
			t.Errorf("cell %d is %.2fx/%s, want %.2fx/%s",
				i, float64(aggs[i].Ratio), aggs[i].Strategy, float64(w.ratio), w.name)
		}
	}
	for _, a := range aggs {
		if a.Cases != 8 || a.Pairs != 80 {
			t.Errorf("%s at %.2fx has cases=%d pairs=%d, want 8 and 80",
				a.Strategy, float64(a.Ratio), a.Cases, a.Pairs)
		}
		if a.CI.Units != 4 {
			t.Errorf("%s at %.2fx bootstrapped over %d units, want the 4 repositories",
				a.Strategy, float64(a.Ratio), a.CI.Units)
		}
	}
}

func TestSummarizeRatiosSkipsFailedCases(t *testing.T) {
	results := sweepResults()
	results[0].Err = errNothingHere
	aggs := summarizeRatios(results, []string{"lectio"})
	for _, a := range aggs {
		if a.Cases != 7 {
			t.Errorf("a failed case still contributed: cases=%d, want 7", a.Cases)
		}
	}
	if got := summarizeRatios(nil, []string{"lectio"}); got != nil {
		t.Errorf("got %d cells from no results", len(got))
	}
}

var errNothingHere = fmt.Errorf("nothing type-checkable at that revision")

func TestSweepTablePivots(t *testing.T) {
	aggs := summarizeRatios(sweepResults(), []string{"lectio", "largest files"})
	ratios, rows := SweepTable(aggs)

	if len(ratios) != 2 || ratios[0] != 1.25 || ratios[1] != 2.0 {
		t.Errorf("ratios are %v, want 1.25 then 2.0", ratios)
	}
	if len(rows) != 2 || rows[0].Strategy != "lectio" {
		t.Fatalf("rows are %+v, want lectio first", rows)
	}
	if _, ok := rows[0].At(1.25); !ok {
		t.Error("lectio has no cell at 1.25x")
	}
	if _, ok := rows[0].At(1.5); ok {
		t.Error("lectio reported a cell at a ratio the sweep never ran")
	}
}

// A blank cell says the question could not be asked at that ratio. It must not
// read as a cell at chance.
func TestSweepTableLeavesUnreachedRatiosBlank(t *testing.T) {
	aggs := []RatioAggregate{
		{Ratio: 1.0, Strategy: "reaches everywhere", Matched: 0.55, Cases: 3, Pairs: 30},
		{Ratio: 2.0, Strategy: "reaches everywhere", Matched: 0.53, Cases: 9, Pairs: 90},
		{Ratio: 2.0, Strategy: "only at the loose end", Matched: 0.51, Cases: 9, Pairs: 90},
	}
	_, rows := SweepTable(aggs)
	for _, r := range rows {
		cell, ok := r.At(1.0)
		if r.Strategy == "only at the loose end" {
			if ok {
				t.Errorf("a strategy that never reached 1.0x reported %+v there", cell)
			}
			continue
		}
		if !ok {
			t.Error("a strategy that did reach 1.0x has no cell there")
		}
	}
}
