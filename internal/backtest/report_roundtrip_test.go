package backtest

import (
	"encoding/json"
	"strings"
	"testing"
)

// A report is the unit this project publishes: every number quoted in the
// write-up came out of one, and `lectio compare` reads them back months later.
// A field that does not survive the round trip is a number that quietly
// becomes zero in whatever reads it next.
func fullReport() Report {
	return Report{
		Schema: ReportSchema, Cases: 74, Failed: 41, Degraded: 12, Unscorable: 3,
		K: 10, Target: TargetTouched, Collapse: CollapseMean, Variants: VariantsCandidates,
		CaseSet: "72749af8fce5", MatchedPairs: 793, MatchedCases: 41, SizeRatio: 1.25,
		FailureReasons: map[string]int{"nothing type-checkable at that revision": 37, "other": 4},
		Medians:        map[string]float64{"lectio": 0.4},
		Aggregates: []Aggregate{{
			Strategy: "lectio", Cases: 74, PrecisionA: 0.391, RecallA: 0.292, MRR: 0.647,
			MatchedA:      0.540,
			MatchedCI:     Interval{Point: 0.540, Lo: 0.488, Hi: 0.593, Level: 0.95, Units: 17},
			MatchedWilson: Interval{Point: 0.520, Lo: 0.485, Hi: 0.554, Level: 0.95, Units: 793},
			MatchedRepos:  Consistency{Above: 11, Below: 4, Level: 2, P: 0.118},
			MatchedByRepo: []RepoAccuracy{{Repo: "a/b", Cases: 3, Pairs: 30, Accuracy: 0.48}},
		}},
		Strata: []StratumAggregate{{Strategy: "lectio", Stratum: 0, Label: "Q1 smallest", Cases: 74, Precision: 0.245, Spread: 23}},
		Sweep: []RatioAggregate{{
			Ratio: 1.1, Strategy: "lectio", Matched: 0.518, Cases: 36, Pairs: 696,
			CI: Interval{Point: 0.518, Lo: 0.462, Hi: 0.577, Level: 0.95, Units: 14},
		}},
		PerCase: []CaseScore{
			{Case: "a/b@rev1/who", Repo: "a/b", Strategy: "lectio", Precision: 0.4, Matched: 0.55, Pairs: 10},
			{Case: "a/b@rev1/who", Repo: "a/b", Strategy: "largest files", Precision: 0.5},
		},
		Verdict: Verdict{Lost: []string{"largest files"}, Note: "did not beat largest files"},
	}
}

func TestReportRoundTripsThroughJSON(t *testing.T) {
	want := fullReport()
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.CaseSet != want.CaseSet || got.Cases != want.Cases || got.SizeRatio != want.SizeRatio {
		t.Errorf("header lost fields: %+v", got)
	}
	if len(got.PerCase) != len(want.PerCase) {
		t.Fatalf("per-case scores: got %d, want %d", len(got.PerCase), len(want.PerCase))
	}
	if got.PerCase[0] != want.PerCase[0] {
		t.Errorf("per-case score changed: %+v vs %+v", got.PerCase[0], want.PerCase[0])
	}
	// The second score has Matched and Pairs at zero, which omitzero drops.
	// Dropping them is fine; coming back as anything else is not.
	if got.PerCase[1] != want.PerCase[1] {
		t.Errorf("a zero-matched score changed: %+v vs %+v", got.PerCase[1], want.PerCase[1])
	}

	a := got.Aggregates[0]
	if a.MatchedCI != want.Aggregates[0].MatchedCI {
		t.Errorf("interval changed: %+v", a.MatchedCI)
	}
	if a.MatchedRepos != want.Aggregates[0].MatchedRepos {
		t.Errorf("consistency changed: %+v", a.MatchedRepos)
	}
	if len(a.MatchedByRepo) != 1 || a.MatchedByRepo[0] != want.Aggregates[0].MatchedByRepo[0] {
		t.Errorf("per-repository breakdown changed: %+v", a.MatchedByRepo)
	}
	if len(got.Sweep) != 1 || got.Sweep[0] != want.Sweep[0] {
		t.Errorf("sweep changed: %+v", got.Sweep)
	}
	if len(got.FailureReasons) != 2 || got.FailureReasons["other"] != 4 {
		t.Errorf("failure reasons changed: %+v", got.FailureReasons)
	}
}

// Two reports that round-trip must still compare, or every stored report
// becomes incomparable with itself.
func TestARoundTrippedReportComparesWithItself(t *testing.T) {
	data, err := json.Marshal(fullReport())
	if err != nil {
		t.Fatal(err)
	}
	var a, b Report
	if err := json.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}

	c := Compare(a, b)
	if !c.Comparable {
		t.Fatalf("a report did not compare with itself: %s", c.Why)
	}
	if c.Intersected {
		t.Error("identical reports were intersected")
	}
	for _, row := range c.Rows {
		if row.PrecisionDelta != 0 || row.MatchedDelta != 0 {
			t.Errorf("%s moved against itself: %+v", row.Strategy, row)
		}
		if !row.MatchedIntervals && row.MatchedA > 0 {
			t.Errorf("%s lost its interval through JSON", row.Strategy)
		}
	}
}

// omitzero on the interval fields means a report from a strategy that produced
// no pairs stays small. It also means the absent case has to be distinguishable
// from a real zero, which is what Units is for.
func TestAbsentIntervalsSerializeAsAbsent(t *testing.T) {
	r := Report{
		Schema: 1, Cases: 1, CaseSet: "x", K: 10, Medians: map[string]float64{},
		Aggregates: []Aggregate{{Strategy: "lectio", PrecisionA: 0.4}},
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); strings.Contains(s, "matched_ci") || strings.Contains(s, "matched_repos") {
		t.Errorf("an aggregate with no pairs serialized empty interval fields:\n%s", s)
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Aggregates[0].MatchedCI.Units != 0 {
		t.Error("an absent interval came back with units")
	}
}
