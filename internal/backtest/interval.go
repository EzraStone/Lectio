package backtest

import (
	"math"
	"math/rand"
	"sort"
)

// Every matched-pair conclusion in docs/gate-a-2026-08.md is read against a
// margin of "±2 points", and that number was chosen by eye. It is doing real
// work — it decides whether churn at 55.8% is an effect and whether lectio at
// 49.5% is a coin — so it should be computed from the run rather than asserted
// beside it.
//
// This file computes it two ways, because the two disagree and the gap between
// them is itself worth reporting.

// Observation is one case's contribution to a matched-pair number.
//
// Repo is carried because cases are not independent: five cases drawn from the
// same repository see the same files, the same history and the same author
// conventions, so treating them as five independent samples understates the
// error. The interval that matters resamples repositories.
type Observation struct {
	Repo     string
	Accuracy float64
	Pairs    int
}

// Interval is a two-sided confidence interval on a matched-pair accuracy.
type Interval struct {
	Point float64 `json:"point"`
	Lo    float64 `json:"lo"`
	Hi    float64 `json:"hi"`
	// Level is the nominal coverage, e.g. 0.95.
	Level float64 `json:"level"`
	// Units is how many independent units the interval was computed over:
	// pairs for the Wilson interval, repositories for the bootstrap.
	Units int `json:"units"`
}

// Width is the full span of the interval, in accuracy points.
func (iv Interval) Width() float64 { return iv.Hi - iv.Lo }

// HalfWidth is the ± figure a table can print beside a point estimate.
func (iv Interval) HalfWidth() float64 { return iv.Width() / 2 }

// ExcludesChance reports whether the whole interval sits clear of 50%.
//
// This is the question the margin was standing in for. A strategy whose
// interval contains 0.5 has not been shown to know anything the pairing did not
// already remove, however far its point estimate sits from chance.
func (iv Interval) ExcludesChance() bool {
	return iv.Lo > MatchedChance || iv.Hi < MatchedChance
}

// DefaultLevel is the coverage every interval in a report is computed at.
const DefaultLevel = 0.95

// MinClusters is the fewest repositories a bootstrap interval may be computed
// from.
//
// A percentile bootstrap needs enough distinct resamples for its tails to mean
// something, and with k clusters there are only C(2k-1, k) of them: four
// repositories give 35, which is not a distribution. The interval it produces
// is not merely wide-with-uncertainty, it is arbitrary, and it can come out
// narrow.
//
// This is not hypothetical. A ratio sweep run at exact size matching reached
// four repositories and reported largest-files at 60.3% with an interval that
// cleared chance — from 171 pairs where a size strategy cannot, by
// construction, know anything. Eight is a floor rather than a recommendation:
// intervals from ten to fifteen repositories are still wide enough that
// nothing in this project separates two strategies at that scale.
const MinClusters = 8

// BootstrapIters is how many resamples the cluster bootstrap draws.
//
// Two thousand is enough that the 2.5th and 97.5th percentiles are stable to
// well under a tenth of a point, and cheap enough to run for every strategy on
// every report: the resampling is over a few dozen precomputed floats, not over
// the corpus.
const BootstrapIters = 2000

// zFor returns the standard-normal quantile for a two-sided interval at the
// given coverage.
func zFor(level float64) float64 {
	if level <= 0 || level >= 1 {
		return 0
	}
	return math.Sqrt2 * math.Erfinv(level)
}

// WilsonInterval is the textbook interval for a proportion, computed over
// pairs as though every pair were independent.
//
// It is the optimistic bound, and it is reported for exactly that reason:
// pairs within a case share a candidate set, and cases within a repository
// share everything, so this interval is narrower than the truth. Printing it
// beside the bootstrap shows how much of the apparent precision comes from
// counting correlated observations.
//
// Wilson rather than the normal approximation because accuracies here sit near
// 0.5 with modest n, where the plain interval can run past the ends of the
// scale and is badly calibrated besides.
func WilsonInterval(wins float64, pairs int, level float64) Interval {
	if pairs <= 0 {
		return Interval{Level: level}
	}
	n := float64(pairs)
	p := wins / n
	z := zFor(level)
	z2 := z * z
	denom := 1 + z2/n
	center := (p + z2/(2*n)) / denom
	half := z / denom * math.Sqrt(p*(1-p)/n+z2/(4*n*n))
	return Interval{
		Point: p,
		Lo:    math.Max(0, center-half),
		Hi:    math.Min(1, center+half),
		Level: level,
		Units: pairs,
	}
}

// BootstrapInterval resamples whole repositories with replacement and reports
// the percentile interval of the resulting means.
//
// Repositories, not cases and not pairs. The corpus is thirty repositories; a
// run scoring 41 cases is scoring roughly one and a half cases per repository,
// and the thing that would make a result fail to generalize is a repository
// behaving unusually, not a case within one. Resampling at the level the
// dependence lives at is the only way the interval reflects it.
//
// The estimator being bootstrapped is the same one the report prints — the
// unweighted mean of per-case accuracies — so the interval is about the number
// in the table rather than about a differently-weighted cousin of it.
//
// Seeded and deterministic. A confidence interval that moved between runs of
// the same command would be one more number nobody could reproduce.
func BootstrapInterval(obs []Observation, level float64, iters int, seed int64) Interval {
	if len(obs) == 0 || iters <= 0 {
		return Interval{Level: level}
	}
	clusters := groupByRepo(obs)
	if len(clusters) == 0 {
		return Interval{Level: level}
	}

	point := meanAccuracy(obs)
	if len(clusters) < MinClusters {
		// Too few repositories to resample. Report the point estimate with no
		// claim about the interval rather than a confident-looking one drawn
		// from a handful of distinct resamples.
		return Interval{Point: point, Lo: 0, Hi: 1, Level: level, Units: len(clusters)}
	}

	rng := rand.New(rand.NewSource(seed))
	means := make([]float64, 0, iters)
	draw := make([]Observation, 0, len(obs))
	for i := 0; i < iters; i++ {
		draw = draw[:0]
		for j := 0; j < len(clusters); j++ {
			draw = append(draw, clusters[rng.Intn(len(clusters))]...)
		}
		means = append(means, meanAccuracy(draw))
	}
	sort.Float64s(means)

	alpha := (1 - level) / 2
	return Interval{
		Point: point,
		Lo:    percentileOf(means, alpha),
		Hi:    percentileOf(means, 1-alpha),
		Level: level,
		Units: len(clusters),
	}
}

// groupByRepo buckets observations into the clusters the bootstrap draws from,
// in a fixed order so the same seed gives the same answer.
func groupByRepo(obs []Observation) [][]Observation {
	byRepo := map[string][]Observation{}
	var names []string
	for _, o := range obs {
		if _, seen := byRepo[o.Repo]; !seen {
			names = append(names, o.Repo)
		}
		byRepo[o.Repo] = append(byRepo[o.Repo], o)
	}
	sort.Strings(names)
	out := make([][]Observation, 0, len(names))
	for _, n := range names {
		out = append(out, byRepo[n])
	}
	return out
}

func meanAccuracy(obs []Observation) float64 {
	if len(obs) == 0 {
		return 0
	}
	var sum float64
	for _, o := range obs {
		sum += o.Accuracy
	}
	return sum / float64(len(obs))
}

// percentileOf reads a quantile off an already-sorted slice by nearest rank.
func percentileOf(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[len(sorted)-1]
	}
	i := int(math.Round(q*float64(len(sorted)-1)) + 0.0)
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}
