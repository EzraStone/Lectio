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

// withCI attaches an interval to a strategy on a report, so a comparison has
// something to overlap.
func withCI(r Report, strategy string, point, lo, hi float64) Report {
	out := r
	out.Aggregates = append([]Aggregate(nil), r.Aggregates...)
	for i := range out.Aggregates {
		if out.Aggregates[i].Strategy != strategy {
			continue
		}
		out.Aggregates[i].MatchedA = point
		out.Aggregates[i].MatchedCI = Interval{Point: point, Lo: lo, Hi: hi, Level: 0.95, Units: 17}
	}
	return out
}

func ciBase() Report {
	return Report{
		CaseSet: "876c5a2edb25", Cases: 161, Target: TargetTouched, Collapse: CollapseMean,
		Aggregates: []Aggregate{{Strategy: "lectio", PrecisionA: 0.336}},
	}
}

// The mistake this flag exists to prevent, in its original numbers: +1.9
// points between two intervals that overlap along almost their whole length.
func TestOverlappingIntervalsAreNotADelta(t *testing.T) {
	a := withCI(ciBase(), "lectio", 0.534, 0.483, 0.570)
	b := withCI(ciBase(), "lectio", 0.553, 0.493, 0.597)

	c := Compare(a, b)
	if !c.Comparable {
		t.Fatalf("the two runs did not compare: %s", c.Why)
	}
	row := c.Rows[0]
	if !row.MatchedIntervals {
		t.Fatal("both runs carry intervals and the row says otherwise")
	}
	if !row.MatchedOverlap {
		t.Errorf("[48.3, 57.0] and [49.3, 59.7] were reported as disjoint")
	}
	if row.Decisive() {
		t.Error("a +1.9 point delta inside both intervals was called decisive")
	}
	if got := row.MatchedDelta * 100; got < 1.8 || got > 2.0 {
		t.Errorf("delta is %.1f points, want about +1.9 — the fixture has drifted", got)
	}
}

func TestDisjointIntervalsAreADelta(t *testing.T) {
	a := withCI(ciBase(), "lectio", 0.487, 0.443, 0.535)
	b := withCI(ciBase(), "lectio", 0.612, 0.560, 0.664)

	row := Compare(a, b).Rows[0]
	if row.MatchedOverlap {
		t.Error("[44.3, 53.5] and [56.0, 66.4] were reported as overlapping")
	}
	if !row.Decisive() {
		t.Error("a delta clear of both intervals was not called decisive")
	}
}

// Intervals that touch at exactly one point overlap. Calling that disjoint
// would make the flag depend on the last decimal place of a bootstrap.
func TestTouchingIntervalsOverlap(t *testing.T) {
	a := withCI(ciBase(), "lectio", 0.50, 0.45, 0.55)
	b := withCI(ciBase(), "lectio", 0.60, 0.55, 0.65)
	if !Compare(a, b).Rows[0].MatchedOverlap {
		t.Error("intervals sharing exactly their endpoint were reported as disjoint")
	}
}

// A report written before the interval work has no intervals, and that is not
// the same as intervals that overlap.
func TestReportsWithoutIntervalsSaySo(t *testing.T) {
	a := ciBase()
	a.Aggregates[0].MatchedA = 0.534
	b := withCI(ciBase(), "lectio", 0.553, 0.493, 0.597)

	row := Compare(a, b).Rows[0]
	if row.MatchedIntervals {
		t.Error("a run with no intervals was treated as carrying them")
	}
	if row.Decisive() {
		t.Error("a row with no intervals was called decisive")
	}
	if row.MatchedOverlap {
		t.Error("a row with no intervals reported an overlap")
	}
}

func TestIntervalsOverlapIsSymmetric(t *testing.T) {
	for _, tc := range []struct {
		a, b Interval
		want bool
	}{
		{Interval{Lo: 0.4, Hi: 0.6}, Interval{Lo: 0.5, Hi: 0.7}, true},
		{Interval{Lo: 0.4, Hi: 0.6}, Interval{Lo: 0.61, Hi: 0.7}, false},
		{Interval{Lo: 0.4, Hi: 0.9}, Interval{Lo: 0.5, Hi: 0.6}, true}, // contained
	} {
		if got := intervalsOverlap(tc.a, tc.b); got != tc.want {
			t.Errorf("overlap(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
		if got := intervalsOverlap(tc.b, tc.a); got != tc.want {
			t.Errorf("overlap is not symmetric for %v and %v", tc.a, tc.b)
		}
	}
}
