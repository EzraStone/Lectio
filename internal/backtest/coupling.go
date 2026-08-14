package backtest

import (
	"sort"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
)

// CouplingResult is the second backtest: does hidden coupling predict surprise?
//
// The claim being tested, from the spec: if touching A and forgetting B is
// what breaks newcomers, and the signal finds those pairs first, that is what
// makes the tool feel clairvoyant. Stated as something falsifiable — a
// newcomer's corrective commits should land on hidden-coupled files more often
// than chance.
type CouplingResult struct {
	// Pairs is how many hidden-coupled pairs the signal found.
	Pairs int
	// CoupledFiles is how many distinct files appear in at least one pair.
	CoupledFiles int

	// NewcomerFixes counts corrective commits by mid-history contributors
	// inside their first ninety days.
	NewcomerFixes int
	// FixesOnCoupled is how many of those touched a hidden-coupled file.
	FixesOnCoupled int

	// BaseRate is the share of all newcomer commits touching a coupled file.
	BaseRate float64
	// FixRate is the share of newcomer *corrective* commits doing so.
	FixRate float64
	// Lift is FixRate / BaseRate. Above 1 means corrective work concentrates
	// on hidden-coupled files more than the newcomer's ordinary work does.
	Lift float64

	// Verdict is human-readable and deliberately hedged where the evidence is
	// thin.
	Verdict string
}

// Two sample-size guards, because lift is a ratio and a ratio built on small
// counts is arithmetic on noise.
//
// MinFixesForSignal bounds the denominator: three fixes, two of them on
// coupled files, produces a lift of 2.0 that would vanish on any other sample.
//
// MinHitsForSignal bounds the numerator, and it is the one that is easy to
// forget. Twenty-six newcomer fixes clears the first guard comfortably, but if
// exactly one of them touched a coupled file the lift is still decided by that
// single commit. Both counts have to be real before the number means anything.
const (
	MinFixesForSignal = 15
	MinHitsForSignal  = 5
)

// CheckCoupling runs the second backtest against an indexed repository.
//
// It compares like with like: the base rate is drawn from the same newcomers'
// ordinary commits, not from all commits by everyone. Comparing newcomer fixes
// against a repo-wide base rate would measure how new someone is rather than
// whether the pairs predict anything.
func CheckCoupling(v *index.View, p rank.Params, opts CaseOptions) CouplingResult {
	if opts.Horizon <= 0 {
		opts = DefaultCaseOptions()
	}
	var res CouplingResult

	coupled := make(map[string]bool)
	for _, pair := range (rank.HiddenCoupling{}).Pairs(v, p) {
		if !pair.Hidden {
			continue
		}
		res.Pairs++
		coupled[pair.A] = true
		coupled[pair.B] = true
	}
	res.CoupledFiles = len(coupled)

	if res.Pairs == 0 {
		res.Verdict = "no hidden-coupled pairs found — nothing to test"
		return res
	}

	newcomers := newcomerWindows(v.Commits, opts)
	if len(newcomers) == 0 {
		res.Verdict = "no mid-history contributors in this repository"
		return res
	}

	var allCommits, allOnCoupled int
	for _, c := range v.Commits {
		window, isNewcomer := newcomers[c.Author()]
		if !isNewcomer || c.When.Before(window.start) || c.When.After(window.end) {
			continue
		}

		touchesCoupled := false
		for _, f := range c.Files {
			if coupled[f.Path] {
				touchesCoupled = true
				break
			}
		}

		allCommits++
		if touchesCoupled {
			allOnCoupled++
		}
		if c.IsFix() || c.IsRevert() {
			res.NewcomerFixes++
			if touchesCoupled {
				res.FixesOnCoupled++
			}
		}
	}

	if allCommits > 0 {
		res.BaseRate = float64(allOnCoupled) / float64(allCommits)
	}
	if res.NewcomerFixes > 0 {
		res.FixRate = float64(res.FixesOnCoupled) / float64(res.NewcomerFixes)
	}
	if res.BaseRate > 0 {
		res.Lift = res.FixRate / res.BaseRate
	}

	res.Verdict = couplingVerdict(res)
	return res
}

func couplingVerdict(r CouplingResult) string {
	switch {
	case r.NewcomerFixes < MinFixesForSignal:
		return "too few newcomer fixes to say anything — this is a sample-size problem, not a result"
	case r.FixesOnCoupled < MinHitsForSignal:
		return "too few fixes landed on coupled files to tell this apart from chance"
	case r.BaseRate == 0:
		return "newcomers never touched a hidden-coupled file; the signal found pairs nobody works on"
	case r.Lift >= 1.5:
		return "newcomer fixes concentrate on hidden-coupled files well above their base rate"
	case r.Lift > 1.1:
		return "a modest concentration, in the predicted direction but not decisive"
	case r.Lift >= 0.9:
		return "no relationship — hidden coupling does not predict where newcomers go wrong here"
	default:
		return "newcomer fixes avoid hidden-coupled files, which is the opposite of the hypothesis"
	}
}

// window is a contributor's first ninety days.
type window struct{ start, end time.Time }

// newcomerWindows finds mid-history contributors and their observation window.
func newcomerWindows(commits []core.Commit, opts CaseOptions) map[string]window {
	if len(commits) <= opts.MinPriorCommits {
		return nil
	}

	// Commits arrive oldest-first, so the first sighting of an author is their
	// first commit.
	firstIndex := make(map[string]int)
	order := make([]string, 0, 16)
	for i, c := range commits {
		who := c.Author()
		if who == "" {
			continue
		}
		if _, seen := firstIndex[who]; !seen {
			firstIndex[who] = i
			order = append(order, who)
		}
	}

	out := make(map[string]window)
	for _, who := range order {
		idx := firstIndex[who]
		if idx < opts.MinPriorCommits {
			continue
		}
		start := commits[idx].When
		out[who] = window{start: start, end: start.Add(opts.Horizon)}
	}
	return out
}

// TopCoupledPairs returns the strongest hidden couplings, for a report that
// shows its working rather than only a number.
func TopCoupledPairs(v *index.View, p rank.Params, n int) []rank.Pair {
	var out []rank.Pair
	for _, pair := range (rank.HiddenCoupling{}).Pairs(v, p) {
		if pair.Hidden {
			out = append(out, pair)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Together > out[j].Together })
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
