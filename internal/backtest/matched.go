package backtest

import (
	"sort"
)

// MatchedPair is one declaration the newcomer touched, beside one of the same
// size that they did not.
type MatchedPair struct {
	// Touched is in the ground truth.
	Touched string
	// Twin is not, and is as close in size as the candidate set allows.
	Twin string
	// Size is the twin's size; Touched's is within MaxSizeRatio of it.
	Size int
}

// MaxSizeRatio bounds how unequal a matched pair may be.
//
// A pair matched at 1.25x still leaves a sliver of size information, and a
// tighter bound discards pairs in repositories where sizes are sparse. This is
// the loosest ratio at which the residual advantage is smaller than the effect
// the measure is looking for: a strategy that knew nothing but size would win
// such a pair barely above half the time, while a strategy that knew something
// real should win well clear of it.
const MaxSizeRatio = 1.25

// MinPairs is the fewest pairs a case needs before its accuracy means
// anything. Below this, one lucky pair moves the number by ten points.
const MinPairs = 5

// MatchedChance is what a strategy scores when it knows nothing the pairing
// did not already equalize. Half.
const MatchedChance = 0.5

// MatchedMargin is how far from chance a result has to sit before it is worth
// reading as an effect rather than as noise.
//
// Two points, matching the level the ablation table is read at. Below it, a
// strategy is doing what a coin does.
const MatchedMargin = 0.02

// BuildMatchedPairs pairs every ground-truth symbol with a size-matched symbol
// the contributor did not touch.
//
// This is the measure size cannot answer. Every earlier target — files,
// declarations, corrected files, corrected declarations — correlates with how
// much code there is, which is why a heuristic that simply prefers the largest
// thing available beat a seven-signal ranking at both granularities. Matching
// on size removes that axis by construction: within a pair the two candidates
// are the same size, so a strategy ranking purely by size wins half of them,
// and anything above half is information about something else.
//
// Chance is 50%. That is the whole point, and it is what makes this readable
// without a baseline to compare against — though the baselines are scored on it
// anyway, because a baseline landing well above chance would mean the pairing
// is leaking something.
//
// Twins are chosen deterministically: nearest by size, ties broken by ID, and
// each twin used at most once so a single unusual declaration cannot stand in
// for half the ground truth.
func BuildMatchedPairs(ground []string, sizes map[string]int) []MatchedPair {
	inGround := make(map[string]bool, len(ground))
	for _, id := range ground {
		inGround[id] = true
	}

	// Candidates are everything sized that the contributor did not touch.
	candidates := make([]string, 0, len(sizes))
	for id := range sizes {
		if !inGround[id] {
			candidates = append(candidates, id)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if sizes[candidates[i]] != sizes[candidates[j]] {
			return sizes[candidates[i]] < sizes[candidates[j]]
		}
		return candidates[i] < candidates[j]
	})

	// Ground truth in a fixed order too, or which symbols get the scarce
	// close-sized twins would depend on map iteration.
	targets := append([]string(nil), ground...)
	sort.Strings(targets)

	used := make(map[string]bool, len(targets))
	out := make([]MatchedPair, 0, len(targets))

	for _, id := range targets {
		size, ok := sizes[id]
		if !ok || size <= 0 {
			// Not in the candidate universe at all — nothing could have
			// recommended it, so it is not a fair thing to score against.
			continue
		}
		twin, twinSize, found := nearestUnused(candidates, sizes, size, used)
		if !found || !withinRatio(size, twinSize) {
			continue
		}
		used[twin] = true
		out = append(out, MatchedPair{Touched: id, Twin: twin, Size: twinSize})
	}
	return out
}

// withinRatio reports whether two sizes are close enough to be a fair pair.
func withinRatio(a, b int) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	return float64(hi)/float64(lo) <= MaxSizeRatio
}

// nearestUnused finds the closest-sized unused candidate.
//
// Linear rather than a binary search with a skip list: the candidate list is
// thousands of entries and this runs once per ground-truth symbol, so the
// simple version is fast enough and cannot get the tie-breaking subtly wrong.
func nearestUnused(candidates []string, sizes map[string]int, want int, used map[string]bool) (string, int, bool) {
	best, bestSize, bestDist := "", 0, 0
	found := false
	for _, id := range candidates {
		if used[id] {
			continue
		}
		size := sizes[id]
		dist := size - want
		if dist < 0 {
			dist = -dist
		}
		if !found || dist < bestDist {
			best, bestSize, bestDist, found = id, size, dist, true
			if dist == 0 {
				// Candidates are size-sorted, so nothing later can be closer.
				break
			}
		}
	}
	return best, bestSize, found
}

// ScoreMatchedPairs reports the share of pairs where the strategy ranked the
// touched symbol above its size-matched twin.
//
// Positions come from the strategy's full ranking. A symbol the strategy never
// names is treated as ranked last rather than dropped — declining to rank
// something is a prediction, and a strategy that named only its favourites
// would otherwise score on a subset of its own choosing.
//
// A tie is half a win. Two symbols the strategy cannot separate is exactly the
// null result this measure exists to detect, and scoring it as a loss would
// make an uninformative ranking look actively wrong.
func ScoreMatchedPairs(predicted []string, pairs []MatchedPair) (accuracy float64, scored int) {
	if len(pairs) == 0 {
		return 0, 0
	}
	pos := make(map[string]int, len(predicted))
	for i, id := range predicted {
		if _, seen := pos[id]; !seen {
			pos[id] = i
		}
	}
	unranked := len(predicted) + 1
	rank := func(id string) int {
		if i, ok := pos[id]; ok {
			return i
		}
		return unranked
	}

	var wins float64
	for _, p := range pairs {
		a, b := rank(p.Touched), rank(p.Twin)
		switch {
		case a < b:
			wins++
		case a == b:
			wins += 0.5
		}
	}
	return wins / float64(len(pairs)), len(pairs)
}
