package backtest

import (
	"strings"
	"testing"
)

func TestSizeRatioValidity(t *testing.T) {
	for _, tc := range []struct {
		r     SizeRatio
		valid bool
	}{
		{0, false},
		{0.9, false},
		{-1, false},
		{1, true},
		{1.25, true},
		{4, true},
	} {
		if got := tc.r.Valid(); got != tc.valid {
			t.Errorf("SizeRatio(%v).Valid() = %v, want %v", float64(tc.r), got, tc.valid)
		}
	}
}

func TestSizeRatioOrFallsBack(t *testing.T) {
	if got := SizeRatio(0).Or(MaxSizeRatio); got != MaxSizeRatio {
		t.Errorf("unset ratio resolved to %v, want the default", float64(got))
	}
	if got := SizeRatio(1.5).Or(MaxSizeRatio); got != 1.5 {
		t.Errorf("a set ratio resolved to %v, want 1.5", float64(got))
	}
	if got := SizeRatio(0.5).Or(MaxSizeRatio); got != MaxSizeRatio {
		t.Errorf("an impossible ratio resolved to %v, want the default", float64(got))
	}
}

// A tighter ratio must not admit a pair a looser one rejected, and the sizes
// in every pair it does admit must actually be within the ratio.
func TestBuildMatchedPairsHonorsTheRatio(t *testing.T) {
	sizes := map[string]int{
		"touched.A":  100,
		"touched.B":  200,
		"twin.exact": 100,
		"twin.near":  210, // 1.05x of B
		"twin.far":   400, // 2.00x of B
	}
	ground := []string{"touched.A", "touched.B"}

	strict := BuildMatchedPairsAt(ground, sizes, 1.0)
	if len(strict) != 1 || strict[0].Touched != "touched.A" {
		t.Fatalf("at 1.0x got %+v, want only the exact match", strict)
	}

	loose := BuildMatchedPairsAt(ground, sizes, 1.25)
	if len(loose) != 2 {
		t.Fatalf("at 1.25x got %d pairs, want both", len(loose))
	}
	for _, p := range loose {
		if !withinRatioAt(sizes[p.Touched], p.Size, 1.25) {
			t.Errorf("pair %s/%s is %d vs %d, outside 1.25x",
				p.Touched, p.Twin, sizes[p.Touched], p.Size)
		}
	}
}

// Loosening the ratio can only add pairs, never remove them. If it ever
// removed one, the twin assignment would depend on the ratio in a way that
// makes two runs incomparable for a second reason.
func TestLooseningTheRatioOnlyAddsPairs(t *testing.T) {
	sizes := map[string]int{}
	var ground []string
	for i := 1; i <= 30; i++ {
		id := "sym" + string(rune('a'+i%26)) + string(rune('0'+i/10))
		sizes[id] = 10 * i
		if i%3 == 0 {
			ground = append(ground, id)
		}
	}

	prev := 0
	for _, ratio := range []SizeRatio{1.0, 1.1, 1.25, 1.5, 2.0, 4.0} {
		got := len(BuildMatchedPairsAt(ground, sizes, ratio))
		if got < prev {
			t.Errorf("at %.2fx got %d pairs, fewer than the %d at the tighter ratio", float64(ratio), got, prev)
		}
		prev = got
	}
	if prev == 0 {
		t.Fatal("no ratio produced any pairs — the fixture is not exercising anything")
	}
}

// The unparameterised entry point has to keep meaning what it meant, since
// every number in the write-up came through it.
func TestBuildMatchedPairsDefaultsToMaxSizeRatio(t *testing.T) {
	sizes := map[string]int{"a": 100, "b": 120, "c": 400}
	ground := []string{"a"}

	def := BuildMatchedPairs(ground, sizes)
	at := BuildMatchedPairsAt(ground, sizes, MaxSizeRatio)
	if len(def) != len(at) {
		t.Fatalf("default gave %d pairs, explicit %.2fx gave %d", len(def), float64(MaxSizeRatio), len(at))
	}
	for i := range def {
		if def[i] != at[i] {
			t.Errorf("pair %d differs: %+v vs %+v", i, def[i], at[i])
		}
	}
	if len(def) != 1 || def[0].Twin != "b" {
		t.Errorf("got %+v, want a paired with b at 1.2x", def)
	}
}

func TestBuildMatchedPairsAtRejectsAnImpossibleRatio(t *testing.T) {
	sizes := map[string]int{"a": 100, "b": 120}
	// Below 1.0 the call falls back to the default rather than silently
	// pairing nothing, which would look like a corpus with no size matches.
	if got := BuildMatchedPairsAt([]string{"a"}, sizes, 0.5); len(got) != 1 {
		t.Errorf("got %d pairs at an impossible ratio, want the default's 1", len(got))
	}
}

// Two runs at different ratios measured different things, and comparing them
// would produce a table of deltas that look like findings.
func TestCompareRefusesAcrossSizeRatios(t *testing.T) {
	base := Report{
		CaseSet: "abc123", Cases: 40, Target: TargetTouched, Collapse: CollapseMean,
		Aggregates: []Aggregate{{Strategy: "lectio", PrecisionA: 0.4, MatchedA: 0.55}},
	}
	a, b := base, base
	a.SizeRatio, b.SizeRatio = 1.25, 2.0

	c := Compare(a, b)
	if c.Comparable {
		t.Error("a 1.25x run compared cleanly against a 2.0x run")
	}
	if !strings.Contains(c.Why, "size-matching ratios") {
		t.Errorf("refusal says %q, which does not name the ratio", c.Why)
	}

	// An unset ratio means the default, so it must compare against an explicit
	// one — otherwise every report written before the flag existed becomes
	// incomparable with every report written after.
	implicit, explicit := base, base
	explicit.SizeRatio = MaxSizeRatio
	if !Compare(implicit, explicit).Comparable {
		t.Error("a report predating the flag would not compare against an explicit default")
	}
}
