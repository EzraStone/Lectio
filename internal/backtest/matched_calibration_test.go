package backtest

import (
	"fmt"
	"sort"
	"testing"
)

// syntheticCorpus builds a heavy-tailed size distribution resembling a real
// repository, with ground truth chosen independently of size.
func syntheticCorpus() (sizes map[string]int, ground []string, all []string) {
	sizes = map[string]int{}
	n := 0
	for _, s := range []int{1, 2, 3, 4, 5, 6, 8, 10, 12, 15, 20, 30, 50, 80, 120, 200} {
		for k := 0; k < 40; k++ {
			sizes[fmt.Sprintf("s%04d", n)] = s
			n++
		}
	}
	for id := range sizes {
		all = append(all, id)
	}
	sort.Strings(all)
	for i, id := range all {
		if i%7 == 0 {
			ground = append(ground, id)
		}
	}
	return sizes, ground, all
}

// sizeRanking is what a strategy that knows nothing but size produces.
func sizeRanking(sizes map[string]int, all []string) []string {
	out := append([]string(nil), all...)
	sort.Slice(out, func(i, j int) bool {
		if sizes[out[i]] != sizes[out[j]] {
			return sizes[out[i]] > sizes[out[j]]
		}
		return out[i] < out[j] // the same ID tie-break every strategy uses
	})
	return out
}

// The calibration the whole measure depends on: a strategy that knows nothing
// but size must score chance, because the pairs are size-matched.
//
// The first version of the pairing failed this at 3.3%. Candidates are sorted
// by (size, ID) and ground truth is processed in ID order, so twins were
// consumed in ascending ID order and almost always had a lower ID than their
// partner. Every strategy here breaks ties by ID — it is how they stay
// reproducible — so all of them inherited that ordering and scored far below
// chance for a reason having nothing to do with ranking quality.
//
// A whole corpus run was produced under that flaw, with every strategy between
// 28.8% and 46.8%. Without this test it would have read as a finding.
func TestSizeOnlyRankingScoresChance(t *testing.T) {
	sizes, ground, all := syntheticCorpus()
	pairs := BuildMatchedPairs(ground, sizes)
	if len(pairs) < 50 {
		t.Fatalf("only %d pairs; the fixture is too thin to calibrate against", len(pairs))
	}

	got, _ := ScoreMatchedPairs(sizeRanking(sizes, all), pairs)
	if got < MatchedChance-MatchedMargin || got > MatchedChance+MatchedMargin {
		t.Errorf("a size-only ranking scored %.1f%% on size-matched pairs, want %.0f%% ± %.0f — "+
			"the pairing is leaking something that correlates with how strategies order",
			got*100, MatchedChance*100, MatchedMargin*100)
	}
}

// The complementary calibration: reversing the same ranking must also score
// chance. A measure that only fails one direction would hide a bias that
// happens to favour whichever ordering was tested.
func TestReversedSizeRankingAlsoScoresChance(t *testing.T) {
	sizes, ground, all := syntheticCorpus()
	pairs := BuildMatchedPairs(ground, sizes)

	ranked := sizeRanking(sizes, all)
	for i, j := 0, len(ranked)-1; i < j; i, j = i+1, j-1 {
		ranked[i], ranked[j] = ranked[j], ranked[i]
	}

	got, _ := ScoreMatchedPairs(ranked, pairs)
	if got < MatchedChance-MatchedMargin || got > MatchedChance+MatchedMargin {
		t.Errorf("a reversed size ranking scored %.1f%%, want chance", got*100)
	}
}

// Plain ID order is the other ordering every strategy shares, and the pairing
// must not correlate with it either.
func TestIDOrderScoresChance(t *testing.T) {
	sizes, ground, all := syntheticCorpus()
	pairs := BuildMatchedPairs(ground, sizes)

	got, _ := ScoreMatchedPairs(all, pairs)
	if got < MatchedChance-MatchedMargin || got > MatchedChance+MatchedMargin {
		t.Errorf("ranking by ID scored %.1f%%, want chance — twins are still being "+
			"selected in ID order", got*100)
	}
}

// A ranking that genuinely knows the answer must score near the top, or the
// measure has no power to detect anything.
func TestAnOracleScoresWell(t *testing.T) {
	sizes, ground, all := syntheticCorpus()
	pairs := BuildMatchedPairs(ground, sizes)

	inGround := map[string]bool{}
	for _, id := range ground {
		inGround[id] = true
	}
	oracle := append([]string(nil), all...)
	sort.SliceStable(oracle, func(i, j int) bool {
		return inGround[oracle[i]] && !inGround[oracle[j]]
	})

	got, _ := ScoreMatchedPairs(oracle, pairs)
	if got < 0.99 {
		t.Errorf("an oracle scored %.1f%%, want ~100%% — the measure cannot detect "+
			"a ranking that knows the answer", got*100)
	}
}
