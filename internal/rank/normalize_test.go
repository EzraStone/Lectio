package rank

import (
	"math"
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

func approx(t *testing.T, got, want float64, label string) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func TestNormalizeSpansZeroToOne(t *testing.T) {
	got := Normalize(Scores{"a": 1, "b": 2, "c": 3, "d": 4})
	approx(t, got["a"], 0, "lowest")
	approx(t, got["d"], 1, "highest")
	approx(t, got["b"], 1.0/3, "second")
	approx(t, got["c"], 2.0/3, "third")
}

// The reason percentile rank is used at all: one extreme outlier must not
// flatten everything else to nearly zero the way min-max would.
func TestNormalizeResistsHeavyTails(t *testing.T) {
	raw := Scores{"median": 2, "typical": 3, "outlier": 400}
	got := Normalize(raw)

	if got["median"] != 0 {
		t.Errorf("lowest non-zero should be 0, got %v", got["median"])
	}
	if got["typical"] <= 0.4 {
		t.Errorf("the middle symbol scored %v; an outlier has crushed the scale", got["typical"])
	}
	approx(t, got["outlier"], 1, "outlier")
}

// Sparse signals are the norm — most files have no fix commits, no AI markers.
// Ranking zeros against each other would invent a gradient from absence.
func TestNormalizeLeavesZerosAtZero(t *testing.T) {
	got := Normalize(Scores{"a": 0, "b": 0, "c": 0, "d": 5, "e": 9})

	for _, id := range []core.SymbolID{"a", "b", "c"} {
		if got[id] != 0 {
			t.Errorf("%s = %v, want 0", id, got[id])
		}
	}
	approx(t, got["d"], 0, "lowest non-zero")
	approx(t, got["e"], 1, "highest")
}

func TestNormalizeAveragesTies(t *testing.T) {
	// Four values, the middle two tied: ranks 0, 1, 2, 3 -> the tie shares 1.5.
	got := Normalize(Scores{"a": 1, "b": 5, "c": 5, "d": 9})
	approx(t, got["b"], 1.5/3, "tied b")
	approx(t, got["c"], 1.5/3, "tied c")
	if got["b"] != got["c"] {
		t.Error("tied values must score identically")
	}
}

func TestNormalizeEdgeCases(t *testing.T) {
	if got := Normalize(nil); len(got) != 0 {
		t.Errorf("nil input = %v, want empty", got)
	}
	if got := Normalize(Scores{"a": 0}); got["a"] != 0 {
		t.Errorf("all-zero input = %v, want 0", got["a"])
	}
	if got := Normalize(Scores{"only": 7}); got["only"] != 1 {
		t.Errorf("single non-zero value = %v, want 1", got["only"])
	}
	if got := Normalize(Scores{"a": math.NaN(), "b": 1}); got["a"] != 0 || got["b"] != 1 {
		t.Errorf("NaN handling = %v", got)
	}
}

func TestNormalizeIsDeterministic(t *testing.T) {
	raw := Scores{"a": 5, "b": 5, "c": 5, "d": 1, "e": 9}
	first := Normalize(raw)
	for i := 0; i < 20; i++ {
		got := Normalize(raw)
		for id, v := range first {
			if got[id] != v {
				t.Fatalf("run %d differed at %s: %v vs %v", i, id, got[id], v)
			}
		}
	}
}

func TestNormalizeBoundedPreservesMagnitude(t *testing.T) {
	// Proximity: a symbol nine hops out should read as far away, not as the
	// nearest thing available.
	got := NormalizeBounded(Scores{"near": 1, "far": 9}, 0, 4)
	approx(t, got["near"], 0.25, "near")
	approx(t, got["far"], 1, "clamped far")

	if got := NormalizeBounded(Scores{"x": 1}, 5, 5); len(got) != 0 {
		t.Errorf("degenerate range = %v, want empty", got)
	}
}

func TestPercentile(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5}
	approx(t, Percentile(vals, 0), 1, "p0")
	approx(t, Percentile(vals, 0.5), 3, "p50")
	approx(t, Percentile(vals, 1), 5, "p100")
	approx(t, Percentile(nil, 0.5), 0, "empty")
}
