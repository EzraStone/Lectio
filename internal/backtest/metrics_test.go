package backtest

import (
	"math"
	"testing"
)

func near(t *testing.T, got, want float64, label string) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func TestPrecisionAt(t *testing.T) {
	predicted := []string{"a.go", "b.go", "c.go", "d.go"}
	actual := []string{"a.go", "c.go", "z.go"}

	near(t, PrecisionAt(predicted, actual, 2), 0.5, "p@2")
	near(t, PrecisionAt(predicted, actual, 4), 0.5, "p@4")
	near(t, PrecisionAt(predicted, actual, 1), 1, "p@1")

	// k beyond the prediction list uses what is there rather than padding with
	// misses, or a short honest list scores worse than a padded one.
	near(t, PrecisionAt(predicted, actual, 100), 0.5, "p@100")
}

func TestRecallAt(t *testing.T) {
	predicted := []string{"a.go", "b.go", "c.go"}
	actual := []string{"a.go", "c.go", "z.go"}

	near(t, RecallAt(predicted, actual, 3), 2.0/3, "r@3")
	near(t, RecallAt(predicted, actual, 1), 1.0/3, "r@1")
}

func TestMetricsEdgeCases(t *testing.T) {
	near(t, PrecisionAt(nil, []string{"a"}, 10), 0, "empty predictions")
	near(t, PrecisionAt([]string{"a"}, nil, 10), 0, "empty truth")
	near(t, PrecisionAt([]string{"a"}, []string{"a"}, 0), 0, "k=0")
	near(t, RecallAt(nil, nil, 5), 0, "both empty")
	near(t, F1At([]string{"x"}, []string{"y"}, 1), 0, "no overlap")
}

// Precision@10 treats a hit at position one the same as a hit at position ten.
// For a reading path the difference is real.
func TestMeanReciprocalRank(t *testing.T) {
	actual := []string{"target.go"}
	near(t, MeanReciprocalRank([]string{"target.go", "b", "c"}, actual), 1, "first")
	near(t, MeanReciprocalRank([]string{"a", "target.go", "c"}, actual), 0.5, "second")
	near(t, MeanReciprocalRank([]string{"a", "b", "c"}, actual), 0, "absent")
}

func TestF1At(t *testing.T) {
	predicted := []string{"a", "b"}
	actual := []string{"a", "b"}
	near(t, F1At(predicted, actual, 2), 1, "perfect")
}

// Pooling predictions lets one repo with a prolific contributor dominate
// thirty others, and Gate A asks whether the ranking works on repos generally.
func TestMeanIsPerCase(t *testing.T) {
	got := Mean("lectio", []float64{1, 0, 0.5}, []float64{0.2, 0.2, 0.2}, []float64{1, 0, 0.5})
	near(t, got.PrecisionA, 0.5, "mean precision")
	near(t, got.RecallA, 0.2, "mean recall")
	if got.Cases != 3 {
		t.Errorf("cases = %d, want 3", got.Cases)
	}
	if got.Strategy != "lectio" {
		t.Errorf("strategy = %q", got.Strategy)
	}
}

// A handful of easy cases can carry an average while the typical case is a
// coin flip, so the median is reported alongside.
func TestMedian(t *testing.T) {
	near(t, Median([]float64{1, 2, 3}), 2, "odd")
	near(t, Median([]float64{1, 2, 3, 4}), 2.5, "even")
	near(t, Median(nil), 0, "empty")

	skewed := []float64{0, 0, 0, 0, 1.0}
	if Median(skewed) >= mean(skewed) {
		t.Error("the median should expose a mean carried by outliers")
	}
}

func TestMeanOfNothing(t *testing.T) {
	got := Mean("none", nil, nil, nil)
	if got.Cases != 0 || got.PrecisionA != 0 {
		t.Errorf("empty aggregate = %+v", got)
	}
}
