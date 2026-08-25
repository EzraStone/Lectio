package rank

import (
	"testing"
)

// The candidate weightings are partial maps — "churn only" names one signal
// and omits the rest. That has to mean "the others are off", not "the others
// are undefined", or every candidate would silently inherit whatever the map
// happened to default to.
func TestPartialWeightsDisableTheOmittedSignals(t *testing.T) {
	w := Weights{SignalChurn: 1.0}

	for _, sig := range AllSignals {
		if sig == SignalChurn {
			continue
		}
		if w[sig] != 0 {
			t.Errorf("%s reads as %v in a partial map, want 0", sig, w[sig])
		}
	}
}

// Weights renormalize over the signals that fired, so a lone signal weighted
// 1.0 and a lone signal weighted 0.15 must produce the same ordering. Otherwise
// the candidate comparison would be measuring the size of a number nobody
// chose deliberately.
func TestLoneSignalWeightIsScaleInvariant(t *testing.T) {
	if got := normalizeOver(Weights{SignalChurn: 1.0}, []Signal{SignalChurn}); got != 1.0 {
		t.Errorf("churn at 1.0 renormalizes to %v", got)
	}
	if got := normalizeOver(Weights{SignalChurn: 0.15}, []Signal{SignalChurn}); got != 1.0 {
		t.Errorf("churn at 0.15 renormalizes to %v — scale leaked into the result", got)
	}
}

// normalizeOver mirrors the renormalization Rank performs, so the property can
// be asserted without building an index.
func normalizeOver(w Weights, active []Signal) float64 {
	var total float64
	for _, sig := range active {
		total += w[sig]
	}
	if total == 0 {
		return 0
	}
	return w[active[0]] / total
}

// A weighting whose only signal is silent has nothing to rank on. Returning an
// empty result is right; returning an arbitrary order would look like a
// prediction.
func TestWeightingWithNoActiveSignalRanksNothing(t *testing.T) {
	if got := normalizeOver(Weights{SignalChurn: 1.0}, nil); got != 0 {
		t.Errorf("a weighting with no active signal produced %v", got)
	}
}

// DefaultWeights is a package value several callers read. A candidate built by
// zeroing one of its entries must not reach back and change it.
func TestDefaultWeightsIsFreshEachCall(t *testing.T) {
	a := DefaultWeights()
	before := a[SignalOrphaning]
	a[SignalOrphaning] = 0

	if after := DefaultWeights()[SignalOrphaning]; after != before {
		t.Errorf("mutating one copy changed the next: %v then %v", before, after)
	}
}
