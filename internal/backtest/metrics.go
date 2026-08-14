// Package backtest implements Gate A: retroactive onboarding prediction, run
// offline on public data.
//
// The mechanism, from the spec: find contributors whose first commit lands
// mid-history, rewind the index to the commit before it, generate the reading
// path as if orienting them, then check it against what they actually touched
// over their next ninety days.
//
// This is the real go/no-go, and it runs before a single user exists. Falling
// short means phase 1 has failed and no amount of interface saves it.
package backtest

import "sort"

// PrecisionAt is the share of the top k predictions that the contributor
// actually touched.
//
// The headline metric, because a reading path is a top-ten list and what
// matters is whether those ten were worth reading. Recall over a whole repo is
// easy to inflate by recommending more.
func PrecisionAt(predicted, actual []string, k int) float64 {
	if k <= 0 || len(predicted) == 0 {
		return 0
	}
	want := toSet(actual)
	if len(want) == 0 {
		return 0
	}

	if k > len(predicted) {
		k = len(predicted)
	}
	var hits int
	for _, p := range predicted[:k] {
		if want[p] {
			hits++
		}
	}
	return float64(hits) / float64(k)
}

// RecallAt is the share of what the contributor touched that appears in the
// top k predictions.
func RecallAt(predicted, actual []string, k int) float64 {
	want := toSet(actual)
	if len(want) == 0 || len(predicted) == 0 {
		return 0
	}
	if k <= 0 || k > len(predicted) {
		k = len(predicted)
	}

	var hits int
	for _, p := range predicted[:k] {
		if want[p] {
			hits++
		}
	}
	return float64(hits) / float64(len(want))
}

// MeanReciprocalRank rewards getting something right early.
//
// Precision@10 treats a hit at position one the same as a hit at position ten.
// For a reading path the difference is real — the first item is the one
// everybody actually reads — so this is reported alongside rather than
// instead of precision.
func MeanReciprocalRank(predicted, actual []string) float64 {
	want := toSet(actual)
	for i, p := range predicted {
		if want[p] {
			return 1 / float64(i+1)
		}
	}
	return 0
}

// F1At combines precision and recall at k.
func F1At(predicted, actual []string, k int) float64 {
	p := PrecisionAt(predicted, actual, k)
	r := RecallAt(predicted, actual, k)
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

func toSet(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		if x != "" {
			out[x] = true
		}
	}
	return out
}

// Aggregate summarizes one strategy across many cases.
type Aggregate struct {
	Strategy   string
	Cases      int
	PrecisionA float64 // mean precision@10
	RecallA    float64 // mean recall@10
	MRR        float64
	// Wins counts cases where this strategy beat a comparison strategy. Filled
	// in by the report, not by the aggregation.
	Wins int
}

// Mean folds per-case scores into an aggregate.
//
// The arithmetic mean over cases, not over predictions pooled together. Pooling
// lets one repo with a prolific contributor dominate thirty others, and the
// question Gate A asks is whether the ranking works on repos generally.
func Mean(strategy string, precisions, recalls, mrrs []float64) Aggregate {
	a := Aggregate{Strategy: strategy, Cases: len(precisions)}
	if len(precisions) == 0 {
		return a
	}
	a.PrecisionA = mean(precisions)
	a.RecallA = mean(recalls)
	a.MRR = mean(mrrs)
	return a
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// Median is reported next to the mean because a handful of easy cases can
// carry an average while the typical case is a coin flip.
func Median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
