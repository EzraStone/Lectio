package backtest

import (
	"strings"
	"testing"
)

func leakReport(ratio SizeRatio, rows ...Aggregate) Report {
	return Report{
		Schema: 1, Cases: 41, K: 10, CaseSet: "abc", Target: TargetTouched,
		SizeRatio: ratio, MatchedPairs: 793, MatchedCases: 41,
		Aggregates: rows, Medians: map[string]float64{},
	}
}

func sized(name string, matched, lo, hi float64) Aggregate {
	return Aggregate{
		Strategy: name, MatchedA: matched,
		MatchedCI: Interval{Point: matched, Lo: lo, Hi: hi, Level: 0.95, Units: 17},
	}
}

// The run-10 finding, in its own numbers: largest-files at 52.9% with an
// interval from 50.2%, on pairs where it cannot know anything.
func TestPairingLeakCatchesTheRun10Case(t *testing.T) {
	got := PairingLeak(leakReport(1.25,
		sized("largest files", 0.529, 0.502, 0.562),
		sized("lectio", 0.540, 0.488, 0.593),
	))
	if got == "" {
		t.Fatal("largest files at 52.9% with an interval from 50.2% did not trip the check")
	}
	for _, want := range []string{"largest files at 52.9%", "interval from 50.2%", "1.25x", "--sweep-ratio"} {
		if !strings.Contains(got, want) {
			t.Errorf("the warning is missing %q:\n%s", want, got)
		}
	}
}

// The expected case. A leak check that fires on a clean run is worse than none.
func TestPairingLeakIsSilentOnACleanRun(t *testing.T) {
	got := PairingLeak(leakReport(1.10,
		sized("largest files", 0.505, 0.462, 0.548),
		sized("size-proportional draw", 0.485, 0.441, 0.529),
		sized("most recently modified", 0.586, 0.541, 0.630),
	))
	if got != "" {
		t.Errorf("a clean run tripped the check:\n%s", got)
	}
}

// A history strategy clearing chance is the result, not a leak. Confusing the
// two would make the check fire on every run that found anything.
func TestPairingLeakIgnoresNonSizeStrategies(t *testing.T) {
	got := PairingLeak(leakReport(1.10,
		sized("most churned, 12mo", 0.573, 0.521, 0.625),
		sized("churn only", 0.578, 0.530, 0.626),
		sized("lectio", 0.518, 0.462, 0.577),
	))
	if got != "" {
		t.Errorf("history strategies beating chance read as a leak:\n%s", got)
	}
}

// Below chance is a different failure with different causes, and it is already
// covered by the calibrations.
func TestPairingLeakIsOneSided(t *testing.T) {
	got := PairingLeak(leakReport(1.25, sized("largest files", 0.421, 0.395, 0.448)))
	if got != "" {
		t.Errorf("a size control below chance tripped the leak check:\n%s", got)
	}
}

func TestPairingLeakNamesEveryOffender(t *testing.T) {
	got := PairingLeak(leakReport(2.0,
		sized("largest files", 0.587, 0.551, 0.622),
		sized("largest symbols", 0.530, 0.508, 0.552),
		sized("size-proportional draw", 0.510, 0.470, 0.550),
	))
	for _, want := range []string{"largest files", "largest symbols"} {
		if !strings.Contains(got, want) {
			t.Errorf("the warning does not name %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "size-proportional draw") {
		t.Errorf("a control whose interval contains chance was named:\n%s", got)
	}
	// Worst first, so the sentence leads with the strongest evidence.
	if strings.Index(got, "largest files") > strings.Index(got, "largest symbols") {
		t.Errorf("offenders are not worst-first:\n%s", got)
	}
	if !strings.Contains(got, "Strategies that know") {
		t.Errorf("the plural case reads as singular:\n%s", got)
	}
}

func TestPairingLeakNeedsAnInterval(t *testing.T) {
	// A strategy with no interval — too few repositories, or a report
	// predating the interval work — cannot be judged either way.
	r := leakReport(1.25, Aggregate{Strategy: "largest files", MatchedA: 0.603})
	if got := PairingLeak(r); got != "" {
		t.Errorf("a strategy with no interval tripped the check:\n%s", got)
	}
}

func sweepCell(ratio SizeRatio, name string, matched, lo, hi float64) RatioAggregate {
	return RatioAggregate{
		Ratio: ratio, Strategy: name, Matched: matched, Cases: 40, Pairs: 800,
		CI: Interval{Point: matched, Lo: lo, Hi: hi, Level: 0.95, Units: 17},
	}
}

// The sweep names a bound that works rather than only condemning the one that
// was used — and it names the loosest such bound, because a tighter one always
// reaches fewer cases.
func TestSweepLeakFindsTheLoosestCleanRatio(t *testing.T) {
	r := leakReport(1.25)
	r.Sweep = []RatioAggregate{
		sweepCell(1.10, "largest files", 0.505, 0.462, 0.548),
		sweepCell(1.10, "lectio", 0.518, 0.462, 0.577),
		sweepCell(1.25, "largest files", 0.529, 0.502, 0.562),
		sweepCell(1.50, "largest files", 0.540, 0.512, 0.568),
		sweepCell(2.00, "largest files", 0.587, 0.551, 0.622),
	}
	got, ok := SweepLeak(r)
	if !ok {
		t.Fatal("no clean ratio found in a sweep whose 1.10x column is clean")
	}
	if got != 1.10 {
		t.Errorf("the loosest clean ratio is %.2fx, want 1.10x", float64(got))
	}
}

func TestSweepLeakReportsWhenEveryRatioLeaks(t *testing.T) {
	r := leakReport(1.25)
	r.Sweep = []RatioAggregate{
		sweepCell(1.10, "largest files", 0.560, 0.521, 0.598),
		sweepCell(2.00, "largest files", 0.587, 0.551, 0.622),
	}
	if _, ok := SweepLeak(r); ok {
		t.Error("a sweep that leaks at every ratio reported a clean one")
	}
}

func TestSweepLeakOnNoSweep(t *testing.T) {
	if _, ok := SweepLeak(leakReport(1.25)); ok {
		t.Error("a run with no sweep reported a clean ratio")
	}
}

// Run 11's shape: no size control clears chance at any ratio, so the loosest
// clean bound is the loosest bound there is.
func TestSweepLeakOnACleanLadder(t *testing.T) {
	r := leakReport(1.25)
	for _, ratio := range []SizeRatio{1.0, 1.1, 1.25, 1.5, 2.0} {
		r.Sweep = append(r.Sweep, sweepCell(ratio, "largest symbols", 0.521, 0.489, 0.553))
	}
	got, ok := SweepLeak(r)
	if !ok || got != 2.0 {
		t.Errorf("got %.2fx (ok=%v) from a ladder that never leaks, want 2.00x", float64(got), ok)
	}
}

func TestJoinAndReadsAsASentence(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a and b"},
		{[]string{"a", "b", "c"}, "a, b, and c"},
	} {
		if got := joinAnd(tc.in); got != tc.want {
			t.Errorf("joinAnd(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
