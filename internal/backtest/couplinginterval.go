package backtest

import (
	"math"
	"math/rand"
	"sort"
)

// Lift 1.02 across 2,311 newcomer corrective commits is the most-quoted number
// in this project and the only one that never carried an error bar. It is also
// the number the product's case for existing rests on: the spec's argument is
// that co-change minus static dependency surfaces what a senior engineer knows
// and cannot articulate, and lift is how that was made falsifiable.
//
// "1.02, no relationship" happens to be the right reading. It was not a
// defensible one while the alternative — 1.02 with an interval from 0.7 to 1.5,
// which would say the same thing about the evidence and something very
// different about the corpus — could not be told apart from it.

// CouplingInterval is a confidence interval on pooled lift.
type CouplingInterval struct {
	Point float64 `json:"point"`
	Lo    float64 `json:"lo"`
	Hi    float64 `json:"hi"`
	Level float64 `json:"level"`
	// Repos is how many repositories the resampling drew from.
	Repos int `json:"repos"`
}

// ExcludesNoRelationship reports whether the interval sits clear of 1.0.
//
// One, not zero: lift is a ratio of two rates, so no relationship is a lift of
// exactly one. An interval containing 1.0 has not shown that corrective work
// concentrates on hidden-coupled files, nor that it avoids them.
//
// A refused interval — too few repositories to resample — has Lo and Hi both
// zero and makes no claim. Lift has no upper bound, so there is no "whole
// scale" to fall back to the way a proportion has [0, 1]; the empty range is
// the sentinel, and it has to be checked for or it reads as clear of one from
// below.
func (iv CouplingInterval) ExcludesNoRelationship() bool {
	return iv.Repos > 0 && iv.Hi > iv.Lo && (iv.Lo > 1 || iv.Hi < 1)
}

// BootstrapCoupling resamples repositories and returns a percentile interval
// on the pooled lift.
//
// Repositories are the unit for the same reason they are everywhere else here:
// the commits inside one share a codebase, a team and a set of conventions, so
// treating 2,311 commits as 2,311 independent observations would produce an
// interval far narrower than the evidence supports. What varies between draws
// is which repositories are in the corpus, which is the question — would this
// answer hold on thirty different Go repositories?
//
// PooledCoupling is the estimator, unchanged, so the interval is about the
// number the report prints rather than about a differently-weighted cousin.
// That matters here more than usual: pooling on counts and averaging ratios
// give visibly different answers on this data, and the pooled one is the one
// that was published.
func BootstrapCoupling(rs []RepoCoupling, level float64, iters int, seed int64) CouplingInterval {
	usable := make([]RepoCoupling, 0, len(rs))
	for _, r := range rs {
		// A repository the guards declined contributed no lift, and resampling
		// it would put mass on a value PooledCoupling never used.
		if r.NewcomerFixes >= MinFixesForSignal && r.FixesOnCoupled >= MinHitsForSignal {
			usable = append(usable, r)
		}
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].Repo < usable[j].Repo })

	point := PooledCoupling(rs).Lift
	if len(usable) < MinClusters || iters <= 0 {
		// Same floor as every other interval here. Below it a percentile
		// bootstrap draws from too few distinct resamples for its tails to
		// mean anything, and it can come out narrow.
		return CouplingInterval{Point: point, Level: level, Repos: len(usable)}
	}

	rng := rand.New(rand.NewSource(seed))
	lifts := make([]float64, 0, iters)
	draw := make([]RepoCoupling, len(usable))
	for i := 0; i < iters; i++ {
		for j := range draw {
			draw[j] = usable[rng.Intn(len(usable))]
		}
		if lift := PooledCoupling(draw).Lift; lift > 0 {
			lifts = append(lifts, lift)
		}
	}
	if len(lifts) == 0 {
		return CouplingInterval{Point: point, Level: level, Repos: len(usable)}
	}
	sort.Float64s(lifts)

	alpha := (1 - level) / 2
	return CouplingInterval{
		Point: point,
		Lo:    percentileOf(lifts, alpha),
		Hi:    percentileOf(lifts, 1-alpha),
		Level: level,
		Repos: len(usable),
	}
}

// CouplingPower reports the smallest lift this corpus could have distinguished
// from no relationship, given how the interval came out.
//
// This is the question a null result has to answer and almost never does. "No
// relationship" and "not enough evidence to see one" produce the same number,
// and the difference between them is whether the interval was tight enough to
// have excluded anything worth having. An interval of 0.9 to 1.2 around 1.02
// rules out the strong signal the spec claims; one of 0.5 to 2.0 rules out
// nothing at all and should not be quoted as a refutation.
//
// Returned as the distance from 1.0 to the nearer end of the interval: the
// smallest departure from no-relationship that would have landed outside it.
func CouplingPower(iv CouplingInterval) float64 {
	if iv.Repos == 0 || iv.Hi <= iv.Lo {
		return 0
	}
	above, below := iv.Hi-1, 1-iv.Lo
	return math.Max(above, below)
}
