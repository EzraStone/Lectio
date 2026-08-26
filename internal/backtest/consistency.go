package backtest

import (
	"math"
	"sort"
)

// An interval says how precise a number is. It does not say whether the number
// is one repository's result wearing a corpus average as a disguise.
//
// A strategy scoring 59% across twenty-four repositories could be 59% in most
// of them or 50% in twenty and 95% in four. Those are different claims — the
// first generalizes and the second is a fact about four repositories — and the
// mean cannot tell them apart. This file counts repositories instead.

// Consistency is a strategy's record across repositories: how many it beat
// chance in, how many it lost in, and how surprising that split would be if it
// were flipping coins.
type Consistency struct {
	// Above is repositories whose mean accuracy exceeded chance.
	Above int `json:"above"`
	// Below is repositories whose mean fell short of it.
	Below int `json:"below"`
	// Level is repositories that landed exactly on chance. Dropped from the
	// test, per the sign test's usual convention, but reported because a
	// strategy that is level everywhere is a finding of its own.
	Level int `json:"level"`
	// P is the two-sided sign-test probability of a split at least this
	// lopsided under the null that each repository is a coin.
	P float64 `json:"p"`
	// PAdjusted is P after Holm–Bonferroni correction across every strategy
	// the report scored. Zero means no correction was applied — either the
	// report predates it, or there was only one test, and an exact binomial
	// p is never zero so the two cannot be confused.
	PAdjusted float64 `json:"p_adjusted,omitzero"`
	// Family is how many tests the correction was over. Reported because an
	// adjusted p is meaningless without it.
	Family int `json:"family,omitzero"`
}

// Repos is how many repositories contributed.
func (c Consistency) Repos() int { return c.Above + c.Below + c.Level }

// ConsistencyLevel is the p below which a split is worth remarking on.
//
// The conventional 0.05, and it is doing less work here than the number
// usually does: this is one of several readings placed beside each other, not a
// gate, and nothing in this project passes or fails on it.
const ConsistencyLevel = 0.05

// Lopsided reports whether the split is unlikely enough to be worth reading as
// a pattern rather than as a coin.
//
// Against the adjusted p where one exists. A report showing ten strategies
// produces about one nominal 0.05 by construction, so reading each row against
// the raw threshold means finding a pattern in most reports that contain none.
func (c Consistency) Lopsided() bool {
	if c.Above+c.Below == 0 {
		return false
	}
	return c.Effective() < ConsistencyLevel
}

// Effective is the p a reading should be made against: the adjusted one when
// the report computed it, the raw one otherwise.
func (c Consistency) Effective() float64 {
	if c.PAdjusted > 0 {
		return c.PAdjusted
	}
	return c.P
}

// NominalOnly marks a split that clears the threshold on its own p and not
// after correction. It is the most interesting row in a table precisely
// because it is the one a reader would otherwise quote.
func (c Consistency) NominalOnly() bool {
	return c.PAdjusted > 0 && c.P < ConsistencyLevel && c.PAdjusted >= ConsistencyLevel
}

// RepoConsistency counts how many repositories a strategy beat chance in.
//
// Repositories, not cases: cases from one repository share a file set and a
// history, so counting cases would let a repository that happened to produce
// nine cases outvote eight repositories that produced one each. Each repository
// gets one vote, cast by the mean of its own cases.
//
// The sign test that follows is deliberately blunt. It throws away how far
// above chance each repository landed and keeps only the direction, which makes
// it insensitive to one repository with an enormous effect — exactly the
// failure mode a mean cannot see. A strategy that is genuinely picking up
// something general should win most repositories by a little; one that is not
// will win about half of them.
func RepoConsistency(obs []Observation) Consistency {
	var c Consistency
	for _, group := range groupByRepo(obs) {
		switch m := meanAccuracy(group); {
		case m > MatchedChance:
			c.Above++
		case m < MatchedChance:
			c.Below++
		default:
			c.Level++
		}
	}
	c.P = signTestP(c.Above, c.Below)
	return c
}

// signTestP is the two-sided probability of a split at least this lopsided
// under the null that each repository is an independent coin.
//
// Computed exactly rather than by normal approximation: a corpus is thirty
// repositories and a run reaches twenty-odd of them, which is small enough that
// the approximation's error is comparable to the quantity being reported.
func signTestP(above, below int) float64 {
	n := above + below
	if n == 0 {
		return 1
	}
	extreme := above
	if below > extreme {
		extreme = below
	}
	// P(X >= extreme) + P(X <= n-extreme), which for a symmetric null is twice
	// the upper tail. Capped at 1, since the two tails overlap when the split
	// is even.
	var tail float64
	for k := extreme; k <= n; k++ {
		tail += binomPMF(n, k)
	}
	return math.Min(1, 2*tail)
}

// binomPMF is the probability of exactly k successes in n fair trials.
//
// Through log-gamma rather than factorials, because n choose k overflows
// float64 well before n is large enough to stop mattering.
func binomPMF(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	lnChoose := lgamma(n+1) - lgamma(k+1) - lgamma(n-k+1)
	return math.Exp(lnChoose - float64(n)*math.Ln2)
}

func lgamma(n int) float64 {
	v, _ := math.Lgamma(float64(n))
	return v
}

// RepoAccuracy is one repository's matched-pair result for one strategy.
type RepoAccuracy struct {
	Repo     string  `json:"repo"`
	Cases    int     `json:"cases"`
	Pairs    int     `json:"pairs"`
	Accuracy float64 `json:"accuracy"`
}

// ByRepo breaks a strategy's matched accuracy down per repository, worst
// first, so the shape behind the mean is visible.
//
// Worst first because the interesting question is not which repository the
// strategy did best in — a top-heavy list reads as success no matter what is
// underneath it — but whether the bottom of the list is near chance or well
// below it.
func ByRepo(obs []Observation) []RepoAccuracy {
	out := make([]RepoAccuracy, 0, len(obs))
	for _, group := range groupByRepo(obs) {
		row := RepoAccuracy{Repo: group[0].Repo, Cases: len(group), Accuracy: meanAccuracy(group)}
		for _, o := range group {
			row.Pairs += o.Pairs
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Accuracy != out[j].Accuracy {
			return out[i].Accuracy < out[j].Accuracy
		}
		return out[i].Repo < out[j].Repo
	})
	return out
}
