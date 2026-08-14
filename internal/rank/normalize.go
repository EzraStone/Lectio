package rank

import (
	"math"
	"sort"

	"github.com/EzraStone/Lectio/internal/core"
)

// Normalize maps raw signal values into [0,1] by percentile rank within the
// non-zero population.
//
// Min-max normalization is the obvious choice and it is wrong for this data.
// Every distribution here is heavy-tailed: in a real service one utility
// function has four hundred dependents while the median has two, and one
// config file absorbs a tenth of all commits. Min-max would map that median
// symbol to 0.005 and turn a seven-signal ranking into a one-hot indicator for
// whichever symbol happens to top each distribution. Percentile rank keeps the
// ordering, spreads the mass evenly, and makes weights mean what they look
// like they mean — a signal weighted 0.2 contributes at most 0.2.
//
// Zeros stay at zero. Several signals are sparse by nature — most files have
// no fix commits and no AI markers — and ranking the zeros against each other
// would manufacture a gradient out of an absence of evidence.
//
// Ties share the average of the ranks they span, so twenty files with
// identical churn do not get an arbitrary ordering baked into the score.
func Normalize(raw Scores) Scores {
	if len(raw) == 0 {
		return Scores{}
	}

	type entry struct {
		id  core.SymbolID
		val float64
	}
	nonzero := make([]entry, 0, len(raw))
	for id, v := range raw {
		if v > 0 && !math.IsNaN(v) {
			nonzero = append(nonzero, entry{id, v})
		}
	}

	out := make(Scores, len(raw))
	if len(nonzero) == 0 {
		return out
	}
	if len(nonzero) == 1 {
		out[nonzero[0].id] = 1
		return out
	}

	sort.Slice(nonzero, func(i, j int) bool {
		if nonzero[i].val != nonzero[j].val {
			return nonzero[i].val < nonzero[j].val
		}
		return nonzero[i].id < nonzero[j].id // deterministic ties
	})

	n := len(nonzero)
	for i := 0; i < n; {
		j := i
		for j+1 < n && nonzero[j+1].val == nonzero[i].val {
			j++
		}
		// Average rank across the tied run, scaled so the top rank is 1.
		avgRank := float64(i+j) / 2
		score := avgRank / float64(n-1)
		for k := i; k <= j; k++ {
			out[nonzero[k].id] = score
		}
		i = j + 1
	}
	return out
}

// NormalizeBounded rescales values already known to lie in a bounded range,
// preserving their relative magnitudes.
//
// Used where the raw value is meaningful on its own rather than only relative
// to the population — proximity, where "two hops away" means the same thing in
// every repo, and percentile rank would report the nearest available symbol as
// maximally near even when it is nine hops out.
func NormalizeBounded(raw Scores, lo, hi float64) Scores {
	out := make(Scores, len(raw))
	if hi <= lo {
		return out
	}
	for id, v := range raw {
		switch {
		case v <= lo:
			out[id] = 0
		case v >= hi:
			out[id] = 1
		default:
			out[id] = (v - lo) / (hi - lo)
		}
	}
	return out
}

// Percentile returns the value at the given quantile of a sorted-in-place
// copy, for diagnostics and tests.
func Percentile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := q * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}
