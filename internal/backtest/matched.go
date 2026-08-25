package backtest

import (
	"hash/fnv"
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

// SizeRatio is the ratio a particular run pairs at, so the choice can be
// varied rather than assumed.
//
// The constant above is a judgement call sitting underneath every matched-pair
// number in this project, and it trades two things off against each other in
// opposite directions. Loosening it admits more pairs, which narrows every
// interval, and leaves more residual size information inside a pair, which is
// exactly what the measure exists to remove. Tightening it does the reverse
// and can starve a corpus of pairs entirely.
//
// A finding that only appears at 1.25 is a finding about 1.25. Making the
// ratio a run parameter is what lets that be checked instead of hoped.
type SizeRatio float64

// Valid reports whether a ratio can pair anything. At or below 1.0 only exact
// size matches qualify, which is a legitimate — and very strict — choice; below
// 1.0 nothing can ever pair.
func (r SizeRatio) Valid() bool { return r >= 1 }

// Or returns the ratio, falling back to the default when unset.
func (r SizeRatio) Or(def SizeRatio) SizeRatio {
	if !r.Valid() {
		return def
	}
	return r
}

// MinPairs is the fewest pairs a case needs before its accuracy means
// anything. Below this, one lucky pair moves the number by ten points.
//
// How much of the corpus clears it depends sharply on granularity, and the
// difference is a real limit on what the file-level number can say:
//
//	symbols   67 of 75 cases   2,891 pairs
//	files     33 of 77 cases     426 pairs
//
// A repository has tens of files where it has thousands of declarations, so
// exact size matches are far rarer at file level and most cases cannot be
// paired at all. Any file-granularity result here rests on under half the
// corpus, and that belongs beside the number rather than in a footnote.
const MinPairs = 5

// MatchedChance is what a strategy scores when it knows nothing the pairing
// did not already equalize. Half.
const MatchedChance = 0.5

// MatchedMargin is the tolerance the calibrations are checked against: a
// ranking that must score chance has to land within two points of it.
//
// It was once the reading rule for real runs too, and it should not have been.
// Two points was chosen by eye and is roughly right at 2,891 pairs and badly
// wrong at 426 — the file-level runs it was applied to have a 95% half-width
// near five points even before the dependence between cases in a repository is
// accounted for. Real runs are now read against a bootstrap interval computed
// from the run itself; see interval.go. This constant governs only the
// synthetic corpus, where the answer is known and the sample is whatever the
// test constructs.
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
	return BuildMatchedPairsAt(ground, sizes, MaxSizeRatio)
}

// BuildMatchedPairsAt is BuildMatchedPairs at a chosen size ratio.
func BuildMatchedPairsAt(ground []string, sizes map[string]int, ratio SizeRatio) []MatchedPair {
	ratio = ratio.Or(MaxSizeRatio)
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
		twin, twinSize, found := nearestUnused(candidates, sizes, size, used, id)
		if !found || !withinRatioAt(size, twinSize, ratio) {
			continue
		}
		used[twin] = true
		out = append(out, MatchedPair{Touched: id, Twin: twin, Size: twinSize})
	}
	return out
}

// withinRatio reports whether two sizes are close enough to be a fair pair at
// the default ratio.
func withinRatio(a, b int) bool { return withinRatioAt(a, b, MaxSizeRatio) }

func withinRatioAt(a, b int, ratio SizeRatio) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	return float64(hi)/float64(lo) <= float64(ratio)
}

// nearestUnused finds a closest-sized unused candidate, chosen so the choice
// carries no information about ID order.
//
// The tie-breaking here is the whole measure. Taking the first candidate at the
// minimum distance looks neutral and is not: candidates are sorted by (size,
// ID), so twins get consumed in ascending ID order while ground-truth symbols
// are processed in ascending ID order too. The twin ends up with a lower ID
// than its partner almost every time.
//
// That matters because every strategy in this harness breaks ties by ID — it
// is how they stay reproducible. So a pairing correlated with ID order makes
// every strategy score far below chance for a reason that has nothing to do
// with ranking quality. Measured on a synthetic corpus, a pure size ranking
// scored 3.3% where it must score 50.
//
// Choosing among the tied candidates by a hash of the touched symbol's ID
// keeps the selection deterministic and decorrelates it from the ordering
// every strategy shares.
func nearestUnused(candidates []string, sizes map[string]int, want int, used map[string]bool, seed string) (string, int, bool) {
	bestDist := -1
	var tied []string
	for _, id := range candidates {
		if used[id] {
			continue
		}
		dist := sizes[id] - want
		if dist < 0 {
			dist = -dist
		}
		switch {
		case bestDist < 0 || dist < bestDist:
			bestDist, tied = dist, []string{id}
		case dist == bestDist:
			tied = append(tied, id)
		}
	}
	if len(tied) == 0 {
		return "", 0, false
	}
	chosen := pickTied(tied, seed)
	return chosen, sizes[chosen], true
}

// pickTied selects one candidate deterministically but independently of ID
// order, using the seed rather than the position in the list.
func pickTied(tied []string, seed string) string {
	if len(tied) == 1 {
		return tied[0]
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	return tied[int(h.Sum64()%uint64(len(tied)))]
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
