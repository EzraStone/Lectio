package backtest

import (
	"fmt"
	"sort"
)

// A size-matched pair is supposed to remove size. Whether it actually does is
// an empirical question about the bound it was built at, and run 10 answered
// it badly: at 1.25x, largest-files scored 52.9% on the holdout with an
// interval clear of chance. A strategy that knows only how long a file is
// cannot beat a coin between two files of the same length, so the two
// candidates were not the same length enough.
//
// That was found by hand, by sweeping the ratio and reading a column. This
// file turns it into a check every run makes for itself.

// sizeOnlyStrategies are the strategies whose entire input is how much code
// there is. On a correctly matched pair each of them is a coin, by
// construction, and any of them landing clear of chance means the pairing is
// leaking the thing it exists to remove.
//
// The size-proportional draw is the sharpest of the three: it is literally a
// weighted coin flip and has no tiebreaker to hide behind.
var sizeOnlyStrategies = map[string]bool{
	"largest files":          true,
	"largest symbols":        true,
	"size-proportional draw": true,
}

// PairingLeak reports whether a run's size controls beat chance.
//
// Empty when they did not, which is the expected case and the one worth
// stating plainly: a leak check that never fires is doing its job.
//
// Only upward. A size control landing clear of chance *below* 50% would be
// strange, but it is not the failure this is looking for and it has its own
// explanations — a tiebreaker correlated with the pairing, most likely, which
// is the bug that produced 3.3% on a synthetic corpus and is already covered
// by the calibrations.
func PairingLeak(r Report) string {
	type hit struct {
		name    string
		matched float64
		lo      float64
	}
	var hits []hit
	for _, a := range r.Aggregates {
		if !sizeOnlyStrategies[a.Strategy] || a.MatchedCI.Units == 0 {
			continue
		}
		if a.MatchedCI.Lo > MatchedChance {
			hits = append(hits, hit{a.Strategy, a.MatchedA, a.MatchedCI.Lo})
		}
	}
	if len(hits) == 0 {
		return ""
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].matched > hits[j].matched })

	names := make([]string, 0, len(hits))
	for _, h := range hits {
		names = append(names, fmt.Sprintf("%s at %.1f%% (interval from %.1f%%)",
			h.name, h.matched*100, h.lo*100))
	}
	subject := "A strategy that knows only how much code there is"
	if len(hits) > 1 {
		subject = "Strategies that know only how much code there is"
	}
	return fmt.Sprintf(
		"%s beat chance on size-matched pairs: %s. %s cannot tell two candidates of the "+
			"same size apart, so the pairing at %.2fx is still letting size through. "+
			"Tighten it with --size-ratio, or run --sweep-ratio to see how much of the "+
			"table moves with the bound.",
		subject, joinAnd(names), subject, r.SizeRatio.Or(MaxSizeRatio))
}

// joinAnd renders a list the way a sentence needs it.
func joinAnd(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	case 2:
		return xs[0] + " and " + xs[1]
	}
	out := ""
	for i, x := range xs[:len(xs)-1] {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out + ", and " + xs[len(xs)-1]
}

// SweepLeak reports the tightest ratio in a sweep at which no size control
// beats chance, and whether one exists.
//
// This is the sweep's answer to the same question, and it is more useful than
// the single-ratio check because it names a bound that works rather than only
// condemning the one that was used.
func SweepLeak(r Report) (SizeRatio, bool) {
	if len(r.Sweep) == 0 {
		return 0, false
	}
	leaks := map[SizeRatio]bool{}
	var ratios []SizeRatio
	seen := map[SizeRatio]bool{}
	for _, cell := range r.Sweep {
		if !seen[cell.Ratio] {
			seen[cell.Ratio] = true
			ratios = append(ratios, cell.Ratio)
		}
		if sizeOnlyStrategies[cell.Strategy] && cell.CI.Units > 0 && cell.CI.Lo > MatchedChance {
			leaks[cell.Ratio] = true
		}
	}
	sort.Slice(ratios, func(i, j int) bool { return ratios[i] < ratios[j] })

	// Loosest clean bound rather than tightest, because a tighter ratio always
	// reaches fewer cases. The most useful answer is the largest bound that
	// still holds size out.
	var best SizeRatio
	var found bool
	for _, ratio := range ratios {
		if !leaks[ratio] {
			best, found = ratio, true
		}
	}
	return best, found
}
