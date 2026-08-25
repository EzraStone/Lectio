package backtest

import (
	"testing"

	"github.com/EzraStone/Lectio/internal/rank"
)

// A confirmation only means something if the hypothesis predates the evidence.
// These assert the candidate set is what candidates.go says it is, so a later
// edit that quietly adds a winner after seeing the holdout is visible in the
// diff as a test change too.
func TestCandidatesAreTheNamedFive(t *testing.T) {
	got := Candidates()
	want := []string{
		DefaultVariant,
		"churn only",
		"lectio −orphaning",
		"churn + centrality",
		"history only",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d candidates, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("candidate %d is %q, want %q", i, got[i].Name, name)
		}
	}
}

// The full ranking has to be in the set. A comparison that dropped it would
// report which candidate is best without saying whether any of them beats what
// is already shipped.
func TestCandidatesIncludeTheShippedWeighting(t *testing.T) {
	for _, c := range Candidates() {
		if c.Name != DefaultVariant {
			continue
		}
		def := rank.DefaultWeights()
		for sig, w := range def {
			if c.Weights[sig] != w {
				t.Errorf("the default candidate has %s at %v, shipped is %v", sig, c.Weights[sig], w)
			}
		}
		return
	}
	t.Fatal("the shipped weighting is not among the candidates")
}

// Each candidate has to predict something different, or the comparison cannot
// distinguish between them whatever the numbers say.
func TestCandidatesAreDistinct(t *testing.T) {
	seen := map[rank.Signal]map[float64]bool{}
	_ = seen

	sigs := func(w rank.Weights) map[rank.Signal]float64 {
		out := map[rank.Signal]float64{}
		for sig, v := range w {
			if v > 0 {
				out[sig] = v
			}
		}
		return out
	}

	cands := Candidates()
	for i := range cands {
		for j := i + 1; j < len(cands); j++ {
			a, b := sigs(cands[i].Weights), sigs(cands[j].Weights)
			if len(a) != len(b) {
				continue
			}
			same := true
			for sig, v := range a {
				if b[sig] != v {
					same = false
					break
				}
			}
			if same {
				t.Errorf("%q and %q are the same weighting", cands[i].Name, cands[j].Name)
			}
		}
	}
}

func TestChurnOnlyIsChurnOnly(t *testing.T) {
	for _, c := range Candidates() {
		if c.Name != "churn only" {
			continue
		}
		for sig, w := range c.Weights {
			if sig != rank.SignalChurn && w != 0 {
				t.Errorf("churn only carries %s at %v", sig, w)
			}
		}
		if c.Weights[rank.SignalChurn] <= 0 {
			t.Error("churn only does not weight churn")
		}
		return
	}
	t.Fatal("no churn-only candidate")
}

// withoutSignals must not mutate the shipped weighting, which is a package
// value every other caller reads.
func TestWithoutSignalsDoesNotMutateTheDefault(t *testing.T) {
	before := rank.DefaultWeights()[rank.SignalOrphaning]
	_ = withoutSignals(rank.SignalOrphaning)
	if after := rank.DefaultWeights()[rank.SignalOrphaning]; after != before {
		t.Errorf("DefaultWeights orphaning went from %v to %v", before, after)
	}
}
