package backtest

import (
	"strings"
	"testing"
)

func cmpReport(caseSet string, cases int, scores map[string][2]float64) Report {
	r := Report{
		CaseSet: caseSet, Cases: cases, K: 10,
		Collapse: CollapseMean, Target: TargetTouched,
	}
	for name, v := range scores {
		r.Aggregates = append(r.Aggregates, Aggregate{
			Strategy: name, PrecisionA: v[0], MatchedA: v[1],
		})
	}
	return r
}

// Which cases survive a run depends on whether each revision's dependencies
// resolved, so the same command can measure 85 cases one day and 77 the next.
// A delta between two populations is not a measurement of anything.
func TestCompareRefusesDifferentCaseSets(t *testing.T) {
	a := cmpReport("aaaa", 85, map[string][2]float64{"lectio": {0.42, 0.50}})
	b := cmpReport("bbbb", 77, map[string][2]float64{"lectio": {0.41, 0.51}})

	got := Compare(a, b)
	if got.Comparable {
		t.Fatal("two different case sets compared as though they matched")
	}
	if !strings.Contains(got.Why, "different case sets") {
		t.Errorf("Why = %q", got.Why)
	}
	// The counts belong in the explanation — they are what makes the problem
	// concrete rather than procedural.
	if !strings.Contains(got.Why, "85") || !strings.Contains(got.Why, "77") {
		t.Errorf("the explanation omits the case counts: %q", got.Why)
	}
}

// Precision over declarations is not comparable with precision over file
// paths, however similar the two numbers look.
func TestCompareRefusesDifferentTargets(t *testing.T) {
	a := cmpReport("same", 80, map[string][2]float64{"lectio": {0.42, 0.50}})
	b := cmpReport("same", 80, map[string][2]float64{"lectio": {0.11, 0.49}})
	b.Target = TargetSymbols

	if got := Compare(a, b); got.Comparable {
		t.Error("a file run compared against a symbolic one")
	}
}

// The collapse rule is worth several points on its own, so two runs that
// differ in it are measuring different things.
func TestCompareRefusesDifferentCollapseRules(t *testing.T) {
	a := cmpReport("same", 80, map[string][2]float64{"lectio": {0.419, 0.50}})
	b := cmpReport("same", 80, map[string][2]float64{"lectio": {0.432, 0.50}})
	b.Collapse = CollapseMax

	got := Compare(a, b)
	if got.Comparable {
		t.Error("mean and max compared as though they were the same measure")
	}
	if !strings.Contains(got.Why, "collapse") {
		t.Errorf("Why = %q", got.Why)
	}
}

// A symbolic run never collapses, so the rule recorded on it says nothing and
// must not block an otherwise legitimate comparison.
func TestCompareIgnoresCollapseOnSymbolicRuns(t *testing.T) {
	a := cmpReport("same", 75, map[string][2]float64{"lectio": {0.105, 0.495}})
	b := cmpReport("same", 75, map[string][2]float64{"lectio": {0.107, 0.498}})
	a.Target, b.Target = TargetSymbols, TargetSymbols
	b.Collapse = CollapseMax

	if got := Compare(a, b); !got.Comparable {
		t.Errorf("two symbolic runs were refused over a collapse rule neither applied: %q", got.Why)
	}
}

// Crossing chance is a change of claim, not of degree, and a four-point delta
// looks the same whichever side of 50% it lands on.
func TestCompareFlagsCrossingChance(t *testing.T) {
	a := cmpReport("same", 80, map[string][2]float64{
		"crosses": {0.40, 0.52},
		"steady":  {0.40, 0.56},
	})
	b := cmpReport("same", 80, map[string][2]float64{
		"crosses": {0.40, 0.48},
		"steady":  {0.40, 0.60},
	})

	got := Compare(a, b)
	if !got.Comparable {
		t.Fatalf("not comparable: %s", got.Why)
	}
	by := map[string]ComparisonRow{}
	for _, r := range got.Rows {
		by[r.Strategy] = r
	}
	if !by["crosses"].SignFlip {
		t.Error("52% to 48% did not register as crossing chance")
	}
	if by["steady"].SignFlip {
		t.Error("56% to 60% registered as crossing chance")
	}
}

// A strategy that only one run scored cannot be a delta, and inventing a zero
// for the missing side would report a change that did not happen.
func TestCompareNamesStrategiesPresentInOnlyOneRun(t *testing.T) {
	a := cmpReport("same", 80, map[string][2]float64{"lectio": {0.42, 0.50}, "gone": {0.30, 0.50}})
	b := cmpReport("same", 80, map[string][2]float64{"lectio": {0.42, 0.50}, "new": {0.35, 0.50}})

	got := Compare(a, b)
	if len(got.Rows) != 1 {
		t.Errorf("compared %d strategies, want only the shared one", len(got.Rows))
	}
	if len(got.OnlyInA) != 1 || got.OnlyInA[0] != "gone" {
		t.Errorf("OnlyInA = %v", got.OnlyInA)
	}
	if len(got.OnlyInB) != 1 || got.OnlyInB[0] != "new" {
		t.Errorf("OnlyInB = %v", got.OnlyInB)
	}
}

// Biggest movement first: the row someone is looking for is the one that
// changed, not the one that happens to sort first.
func TestCompareOrdersByMovement(t *testing.T) {
	a := cmpReport("same", 80, map[string][2]float64{
		"aaa": {0.40, 0.50}, "zzz": {0.40, 0.50}, "mmm": {0.40, 0.50},
	})
	b := cmpReport("same", 80, map[string][2]float64{
		"aaa": {0.41, 0.50}, "zzz": {0.55, 0.50}, "mmm": {0.30, 0.50},
	})

	got := Compare(a, b)
	if len(got.Rows) != 3 {
		t.Fatalf("got %d rows", len(got.Rows))
	}
	if got.Rows[0].Strategy != "zzz" {
		t.Errorf("first row is %q, want the largest mover", got.Rows[0].Strategy)
	}
	if got.Rows[2].Strategy != "aaa" {
		t.Errorf("last row is %q, want the smallest mover", got.Rows[2].Strategy)
	}
}

func TestCompareRefusesAnEmptyRun(t *testing.T) {
	a := cmpReport("", 0, nil)
	b := cmpReport("same", 80, map[string][2]float64{"lectio": {0.42, 0.50}})
	if got := Compare(a, b); got.Comparable {
		t.Error("a run that scored nothing compared as legitimate")
	}
}
