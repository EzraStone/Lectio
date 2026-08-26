package backtest

import (
	"math"
	"testing"
)

func adjustedEquals(t *testing.T, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d adjusted values, want %d", len(got), len(want))
	}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Errorf("adjusted[%d] = %.9f, want %.9f (all: %v)", i, got[i], want[i], got)
		}
	}
}

// The textbook procedure, on values chosen so every step is checkable by hand:
// the smallest is multiplied by 4, the next by 3, then 2, then 1.
func TestHolmStepsDown(t *testing.T) {
	adjustedEquals(t,
		Holm([]float64{0.01, 0.02, 0.03, 0.04}),
		[]float64{0.04, 0.06, 0.06, 0.06},
	)
}

// Adjusted values must never decrease as raw values increase, or a table
// ordered by one contradicts a table ordered by the other. The running maximum
// is what enforces it: 0.03 alone would adjust to 0.06 and 0.04 to 0.04.
func TestHolmIsMonotone(t *testing.T) {
	got := Holm([]float64{0.001, 0.03, 0.04, 0.9})
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Errorf("adjusted values decrease at %d: %v", i, got)
		}
	}
}

func TestHolmPreservesInputOrder(t *testing.T) {
	// Deliberately unsorted: the smallest raw p is last. Values chosen so no
	// step hits the cap at 1, which would flatten the answer and hide whether
	// the result came back in the input's order.
	got := Holm([]float64{0.5, 0.2, 0.01})
	adjustedEquals(t, got, []float64{0.5, 0.4, 0.03})
	if !(got[2] < got[1] && got[1] < got[0]) {
		t.Errorf("adjusted values do not follow the raw ones: %v", got)
	}
}

// The step-down and the running maximum interact at the top of the list: once
// a value is capped at 1, every larger raw p adjusts to 1 as well.
func TestHolmClampsAndStaysClamped(t *testing.T) {
	adjustedEquals(t, Holm([]float64{0.9, 0.5, 0.01}), []float64{1.0, 1.0, 0.03})
}

func TestHolmNeverExceedsOne(t *testing.T) {
	for _, adjusted := range Holm([]float64{0.4, 0.5, 0.6, 0.9, 1.0}) {
		if adjusted > 1 {
			t.Errorf("adjusted p = %v, not a probability", adjusted)
		}
	}
}

func TestHolmNeverShrinksAP(t *testing.T) {
	raw := []float64{0.001, 0.01, 0.02, 0.08, 0.3, 0.9}
	for i, adjusted := range Holm(raw) {
		if adjusted < raw[i] {
			t.Errorf("raw %.4f adjusted down to %.4f", raw[i], adjusted)
		}
	}
}

func TestHolmOnOneTestChangesNothing(t *testing.T) {
	adjustedEquals(t, Holm([]float64{0.035}), []float64{0.035})
	if got := Holm(nil); got != nil {
		t.Errorf("got %v from no tests", got)
	}
}

// The run that motivated all of this: churn-only at 0.035 in a table of ten.
func TestChurnOnlyDoesNotSurviveATableOfTen(t *testing.T) {
	// The ten sign-test p-values from the holdout replay, churn-only first.
	raw := []float64{0.035, 0.077, 0.092, 0.118, 0.118, 0.180, 0.302, 0.332, 0.424, 0.454}
	got := Holm(raw)

	if got[0] < ConsistencyLevel {
		t.Errorf("churn-only adjusted to %.4f, which still clears %.2f", got[0], ConsistencyLevel)
	}
	if got[0] <= raw[0] {
		t.Errorf("adjustment did not raise 0.035: %.4f", got[0])
	}
	// 0.035 x 10 = 0.35, which is where it lands and is nowhere near the line.
	adjustedEquals(t, got[:1], []float64{0.35})
}

func holmAggs(ps ...float64) []Aggregate {
	out := make([]Aggregate, 0, len(ps))
	for i, p := range ps {
		out = append(out, Aggregate{
			Strategy:     string(rune('a' + i)),
			MatchedRepos: Consistency{Above: 12, Below: 3, P: p},
		})
	}
	return out
}

func TestApplyHolmFillsInTheFamily(t *testing.T) {
	aggs := holmAggs(0.035, 0.077, 0.092, 0.118)
	applyHolm(aggs)

	for _, a := range aggs {
		if a.MatchedRepos.Family != 4 {
			t.Errorf("%s reports a family of %d, want 4", a.Strategy, a.MatchedRepos.Family)
		}
		if a.MatchedRepos.PAdjusted < a.MatchedRepos.P {
			t.Errorf("%s adjusted %.4f down to %.4f", a.Strategy, a.MatchedRepos.P, a.MatchedRepos.PAdjusted)
		}
	}
}

// A strategy with no sign test is not part of the family, and counting it
// would penalize every other row for a test that was never run.
func TestApplyHolmSkipsStrategiesWithoutASignTest(t *testing.T) {
	aggs := holmAggs(0.01, 0.02, 0.03)
	aggs = append(aggs, Aggregate{Strategy: "no pairs"})
	applyHolm(aggs)

	if got := aggs[0].MatchedRepos.Family; got != 3 {
		t.Errorf("family is %d, want 3 — a strategy with no test was counted", got)
	}
	if aggs[3].MatchedRepos.PAdjusted != 0 {
		t.Errorf("a strategy with no test was adjusted to %v", aggs[3].MatchedRepos.PAdjusted)
	}
}

// One test is not a family, and adjusting it would only invite the reader to
// wonder what it was adjusted against.
func TestApplyHolmLeavesASingleTestAlone(t *testing.T) {
	aggs := holmAggs(0.035)
	applyHolm(aggs)
	if aggs[0].MatchedRepos.PAdjusted != 0 || aggs[0].MatchedRepos.Family != 0 {
		t.Errorf("a lone test was corrected: %+v", aggs[0].MatchedRepos)
	}
	if !aggs[0].MatchedRepos.Lopsided() {
		t.Error("an uncorrected 0.035 does not read as lopsided")
	}
}

func TestLopsidedReadsTheAdjustedP(t *testing.T) {
	nominal := Consistency{Above: 12, Below: 3, P: 0.035, PAdjusted: 0.35, Family: 10}
	if nominal.Lopsided() {
		t.Error("a row that only clears its raw p reads as a pattern")
	}
	if !nominal.NominalOnly() {
		t.Error("a row clearing raw and not adjusted is not marked as nominal-only")
	}
	closeTo(t, nominal.Effective(), 0.35, 1e-12, "effective p")

	real := Consistency{Above: 20, Below: 1, P: 0.0001, PAdjusted: 0.001, Family: 10}
	if !real.Lopsided() {
		t.Error("a row clearing the corrected threshold does not read as a pattern")
	}
	if real.NominalOnly() {
		t.Error("a row clearing both thresholds was marked nominal-only")
	}
}

// A report that predates the correction has no adjusted p, and must still be
// readable against its raw one rather than silently reading as significant.
func TestUncorrectedReportsFallBackToTheRawP(t *testing.T) {
	old := Consistency{Above: 12, Below: 3, P: 0.035}
	closeTo(t, old.Effective(), 0.035, 1e-12, "effective p with no correction")
	if !old.Lopsided() {
		t.Error("an uncorrected 0.035 does not read as lopsided")
	}
	if old.NominalOnly() {
		t.Error("an uncorrected row was marked nominal-only")
	}
}
