package backtest

import "sort"

// A matched-pair number is only as trustworthy as the ratio it was paired at,
// and 1.25x was a judgement call. Checking whether a finding depends on it
// means running the corpus at several ratios — which sounds like several runs
// and is not.
//
// Indexing a rewound revision is minutes; building pairs from an already-built
// size table and scoring an already-computed ranking against them is
// microseconds. The expensive half is shared across every ratio, so a sweep
// costs one run plus rounding.

// SweepRatios is the ladder a sweep walks by default.
//
// 1.0 is the strictest possible pairing — only exactly equal sizes — and is the
// one number in the sweep that carries no residual size information at all. It
// will reach the fewest cases, and that trade is the point: if a finding holds
// at 1.0 it is not an artifact of the bound, and if it appears only at 2.0 it
// probably is.
var SweepRatios = []SizeRatio{1.0, 1.1, MaxSizeRatio, 1.5, 2.0}

// RatioScore is one strategy's matched accuracy at one ratio, on one case.
type RatioScore struct {
	Ratio    SizeRatio
	Strategy string
	Matched  float64
	Pairs    int
}

// RatioAggregate is one strategy's matched accuracy at one ratio, over a run.
type RatioAggregate struct {
	Ratio    SizeRatio `json:"ratio"`
	Strategy string    `json:"strategy"`
	Matched  float64   `json:"matched"`
	// Cases and Pairs are how much the cell rests on. They fall as the ratio
	// tightens, and reading a cell without them is how a sweep gets
	// misinterpreted: the strictest column is also the thinnest.
	Cases int      `json:"cases"`
	Pairs int      `json:"pairs"`
	CI    Interval `json:"ci,omitzero"`
}

// scoreRatios scores one case's rankings against pairs built at each ratio.
//
// predicted is passed in already computed. That is the whole economy of the
// sweep: the ranking does not depend on the ratio, only the pairs do, so
// nothing here re-ranks anything.
func scoreRatios(predicted map[string][]string, ground []string, sizes map[string]int, ratios []SizeRatio) []RatioScore {
	if len(ratios) == 0 || len(predicted) == 0 {
		return nil
	}
	names := make([]string, 0, len(predicted))
	for name := range predicted {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]RatioScore, 0, len(ratios)*len(names))
	for _, ratio := range ratios {
		pairs := BuildMatchedPairsAt(ground, sizes, ratio)
		if len(pairs) < MinPairs {
			// The same floor the main measure applies. A ratio that reaches
			// only two pairs in a case has not measured that case, and
			// recording it would let the tightest column fill with numbers
			// decided by one pair each.
			continue
		}
		for _, name := range names {
			matched, n := ScoreMatchedPairs(predicted[name], pairs)
			out = append(out, RatioScore{Ratio: ratio, Strategy: name, Matched: matched, Pairs: n})
		}
	}
	return out
}

// summarizeRatios folds per-case sweep scores into one row per (ratio,
// strategy), ordered by ratio and then by the strategy order of the main
// table.
func summarizeRatios(results []CaseResult, order []string) []RatioAggregate {
	type key struct {
		ratio    SizeRatio
		strategy string
	}
	acc := map[key][]Observation{}
	pairs := map[key]int{}
	var ratios []SizeRatio
	seenRatio := map[SizeRatio]bool{}

	for _, r := range results {
		if r.Err != nil {
			continue
		}
		for _, rs := range r.Ratios {
			if rs.Pairs == 0 {
				continue
			}
			k := key{rs.Ratio, rs.Strategy}
			acc[k] = append(acc[k], Observation{Repo: r.Case.Repo, Accuracy: rs.Matched, Pairs: rs.Pairs})
			pairs[k] += rs.Pairs
			if !seenRatio[rs.Ratio] {
				seenRatio[rs.Ratio] = true
				ratios = append(ratios, rs.Ratio)
			}
		}
	}
	if len(acc) == 0 {
		return nil
	}
	sort.Slice(ratios, func(i, j int) bool { return ratios[i] < ratios[j] })

	out := make([]RatioAggregate, 0, len(acc))
	for _, ratio := range ratios {
		for _, name := range order {
			k := key{ratio, name}
			obs := acc[k]
			if len(obs) == 0 {
				continue
			}
			out = append(out, RatioAggregate{
				Ratio:    ratio,
				Strategy: name,
				Matched:  meanAccuracy(obs),
				Cases:    len(obs),
				Pairs:    pairs[k],
				CI:       BootstrapInterval(obs, DefaultLevel, BootstrapIters, bootstrapSeed),
			})
		}
	}
	return out
}

// SweepRow is one strategy's whole ladder, for rendering.
type SweepRow struct {
	Strategy string
	Cells    []RatioAggregate
}

// SweepTable pivots a sweep into one row per strategy, in the run's strategy
// order.
func SweepTable(aggs []RatioAggregate) ([]SizeRatio, []SweepRow) {
	var ratios []SizeRatio
	seen := map[SizeRatio]bool{}
	var order []string
	byStrategyName := map[string][]RatioAggregate{}

	for _, a := range aggs {
		if !seen[a.Ratio] {
			seen[a.Ratio] = true
			ratios = append(ratios, a.Ratio)
		}
		if _, ok := byStrategyName[a.Strategy]; !ok {
			order = append(order, a.Strategy)
		}
		byStrategyName[a.Strategy] = append(byStrategyName[a.Strategy], a)
	}
	sort.Slice(ratios, func(i, j int) bool { return ratios[i] < ratios[j] })

	rows := make([]SweepRow, 0, len(order))
	for _, name := range order {
		rows = append(rows, SweepRow{Strategy: name, Cells: byStrategyName[name]})
	}
	return ratios, rows
}

// At returns the cell for a ratio, and whether the strategy reached it.
//
// A strategy can be missing at the tight end of the ladder: exact size matches
// are rare, and a corpus can fail the MinPairs floor everywhere at 1.0 while
// clearing it comfortably at 2.0. A blank cell is information — it says the
// question could not be asked — and is not the same as a cell at chance.
func (r SweepRow) At(ratio SizeRatio) (RatioAggregate, bool) {
	for _, c := range r.Cells {
		if c.Ratio == ratio {
			return c, true
		}
	}
	return RatioAggregate{}, false
}
