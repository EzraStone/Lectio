package backtest

import (
	"fmt"
	"math"
	"testing"
)

// oneCasePerRepo gives each accuracy its own repository, so a Consistency
// counts exactly the values passed in.
func oneCasePerRepo(accuracies ...float64) []Observation {
	out := make([]Observation, 0, len(accuracies))
	for i, a := range accuracies {
		out = append(out, Observation{Repo: fmt.Sprintf("r%02d", i), Accuracy: a, Pairs: 10})
	}
	return out
}

func TestRepoConsistencyCountsDirections(t *testing.T) {
	c := RepoConsistency(oneCasePerRepo(0.6, 0.7, 0.55, 0.4, 0.5))
	if c.Above != 3 || c.Below != 1 || c.Level != 1 {
		t.Errorf("got above=%d below=%d level=%d, want 3/1/1", c.Above, c.Below, c.Level)
	}
	if c.Repos() != 5 {
		t.Errorf("Repos() = %d, want 5", c.Repos())
	}
}

// One vote per repository, cast by the mean of its cases. A repository that
// produced nine cases must not outvote eight that produced one each.
func TestRepoConsistencyGivesEachRepoOneVote(t *testing.T) {
	obs := []Observation{
		{Repo: "loud", Accuracy: 0.9, Pairs: 10},
		{Repo: "loud", Accuracy: 0.9, Pairs: 10},
		{Repo: "loud", Accuracy: 0.9, Pairs: 10},
		{Repo: "loud", Accuracy: 0.9, Pairs: 10},
		{Repo: "loud", Accuracy: 0.9, Pairs: 10},
		{Repo: "a", Accuracy: 0.45, Pairs: 10},
		{Repo: "b", Accuracy: 0.45, Pairs: 10},
		{Repo: "c", Accuracy: 0.45, Pairs: 10},
	}
	c := RepoConsistency(obs)
	if c.Above != 1 || c.Below != 3 {
		t.Errorf("got above=%d below=%d, want 1/3 — the five loud cases are one repository",
			c.Above, c.Below)
	}
	if m := meanAccuracy(obs); m <= MatchedChance {
		t.Fatalf("this fixture is meant to have a mean above chance, got %.3f", m)
	}
}

// The sign test against a table. A run of k heads in k tosses has two-sided
// p = 2 * 2^-(k-1) for k >= 1.
func TestSignTestMatchesTheBinomial(t *testing.T) {
	for _, tc := range []struct {
		above, below int
		want         float64
	}{
		{0, 0, 1},
		{1, 1, 1},      // as even as it gets
		{5, 5, 1},      // still even
		{5, 0, 0.0625}, // 2 * (1/2)^5
		{8, 0, 0.0078125},
		{0, 8, 0.0078125}, // symmetric
		{7, 1, 0.0703125},
		{12, 4, 0.0768127},
		{13, 3, 0.0212708},
		{18, 6, 0.0226558},
		{20, 4, 0.0015439},
	} {
		got := signTestP(tc.above, tc.below)
		if math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("signTestP(%d, %d) = %.7f, want %.7f", tc.above, tc.below, got, tc.want)
		}
	}
}

func TestSignTestIsNeverAboveOne(t *testing.T) {
	for n := 0; n <= 40; n++ {
		for above := 0; above <= n; above++ {
			if p := signTestP(above, n-above); p < 0 || p > 1 {
				t.Fatalf("signTestP(%d, %d) = %v, not a probability", above, n-above, p)
			}
		}
	}
}

// The tails have to sum to one, or the exact computation has an off-by-one in
// it and every p is subtly wrong.
func TestBinomPMFSumsToOne(t *testing.T) {
	for _, n := range []int{1, 5, 24, 30, 60} {
		var total float64
		for k := 0; k <= n; k++ {
			total += binomPMF(n, k)
		}
		if math.Abs(total-1) > 1e-9 {
			t.Errorf("the n=%d distribution sums to %.12f", n, total)
		}
	}
	if got := binomPMF(5, -1); got != 0 {
		t.Errorf("binomPMF(5, -1) = %v, want 0", got)
	}
	if got := binomPMF(5, 6); got != 0 {
		t.Errorf("binomPMF(5, 6) = %v, want 0", got)
	}
}

// A strategy winning a few repositories enormously and losing the rest
// narrowly has a mean above chance and no consistency behind it. Catching that
// is the reason this file exists.
func TestLopsidedSeparatesBroadFromConcentrated(t *testing.T) {
	broad := RepoConsistency(oneCasePerRepo(
		0.54, 0.56, 0.53, 0.58, 0.52, 0.55, 0.57, 0.51, 0.59, 0.53, 0.56, 0.54))
	if !broad.Lopsided() {
		t.Errorf("twelve repositories all above chance read as a coin (p=%.4f)", broad.P)
	}

	concentrated := RepoConsistency(oneCasePerRepo(
		0.95, 0.92, 0.90, 0.48, 0.47, 0.49, 0.48, 0.47, 0.49, 0.48, 0.47, 0.49))
	if concentrated.Lopsided() {
		t.Errorf("three big wins and nine small losses read as a pattern (p=%.4f)", concentrated.P)
	}
	if m := meanAccuracy(oneCasePerRepo(
		0.95, 0.92, 0.90, 0.48, 0.47, 0.49, 0.48, 0.47, 0.49, 0.48, 0.47, 0.49)); m <= MatchedChance {
		t.Fatalf("the concentrated fixture should still have a mean above chance, got %.3f", m)
	}
}

func TestConsistencyOfNothingClaimsNothing(t *testing.T) {
	c := RepoConsistency(nil)
	if c.Repos() != 0 || c.P != 1 || c.Lopsided() {
		t.Errorf("got %+v from no observations", c)
	}
}

func TestByRepoIsWorstFirst(t *testing.T) {
	rows := ByRepo([]Observation{
		{Repo: "good", Accuracy: 0.8, Pairs: 12},
		{Repo: "good", Accuracy: 0.6, Pairs: 8},
		{Repo: "bad", Accuracy: 0.3, Pairs: 5},
		{Repo: "mid", Accuracy: 0.5, Pairs: 7},
	})
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want one per repository", len(rows))
	}
	if rows[0].Repo != "bad" || rows[2].Repo != "good" {
		t.Errorf("order is %s, %s, %s — want worst first", rows[0].Repo, rows[1].Repo, rows[2].Repo)
	}
	if rows[2].Cases != 2 || rows[2].Pairs != 20 {
		t.Errorf("good has cases=%d pairs=%d, want 2 and 20", rows[2].Cases, rows[2].Pairs)
	}
	closeTo(t, rows[2].Accuracy, 0.7, 1e-12, "good's mean")
}

func TestByRepoBreaksTiesByName(t *testing.T) {
	rows := ByRepo([]Observation{
		{Repo: "zeta", Accuracy: 0.5, Pairs: 1},
		{Repo: "alpha", Accuracy: 0.5, Pairs: 1},
	})
	if rows[0].Repo != "alpha" {
		t.Errorf("tied repositories ordered %s before %s", rows[0].Repo, rows[1].Repo)
	}
}
