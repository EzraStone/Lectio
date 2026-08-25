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
	Strategy   string  `json:"strategy"`
	Cases      int     `json:"cases"`
	PrecisionA float64 `json:"precision"` // mean precision@10
	RecallA    float64 `json:"recall"`    // mean recall@10
	MRR        float64 `json:"mrr"`
	// MatchedA is mean accuracy over size-matched pairs. 0.5 is chance, and
	// unlike precision it is readable without a baseline beside it.
	MatchedA float64 `json:"matched"`
	// MatchedCI is the cluster-bootstrap interval around MatchedA, resampling
	// repositories. This is what decides whether a row is an effect: a row
	// whose interval contains 0.5 has not been shown to know anything the
	// pairing did not already remove, however far its point estimate sits from
	// chance.
	MatchedCI Interval `json:"matched_ci,omitzero"`
	// MatchedWilson is the same accuracy's interval computed over pairs as
	// though each were independent. It is always narrower and always wrong in
	// the same direction; it is here so the difference is visible rather than
	// assumed.
	MatchedWilson Interval `json:"matched_wilson,omitzero"`
	// MatchedRepos records how many repositories the strategy beat chance in,
	// which separates a broad small effect from a narrow large one. The
	// interval cannot make that distinction and the mean actively hides it.
	MatchedRepos Consistency `json:"matched_repos,omitzero"`
	// MatchedByRepo is the per-repository breakdown behind MatchedRepos, worst
	// first. Carried in JSON only; the table would be thirty rows per strategy.
	MatchedByRepo []RepoAccuracy `json:"matched_by_repo,omitempty"`
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

// MeanMatched folds matched-pair accuracies into an aggregate.
//
// Averaged over cases that produced pairs, not over all cases. A case with no
// usable pairs has not been measured, and counting it as zero would report
// every strategy as far below chance in proportion to how many cases the
// pairing could not reach.
func MeanMatched(xs []float64) float64 { return mean(xs) }

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
