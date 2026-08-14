package backtest

import (
	"errors"
	"testing"
)

func TestParseTarget(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Target
		ok   bool
	}{
		{"touched", TargetTouched, true},
		{"corrected", TargetCorrected, true},
		{"", DefaultTarget, true},
		{"reverted", "", false},
	} {
		got, err := ParseTarget(tc.in)
		if tc.ok != (err == nil) {
			t.Errorf("ParseTarget(%q) err = %v", tc.in, err)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("ParseTarget(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Swapping the target after a failing run and quoting only the new number
// would be choosing the measure that flatters. The default has to stay the
// spec's primary one.
func TestDefaultTargetIsTheSpecsPrimaryMeasure(t *testing.T) {
	if DefaultTarget != TargetTouched {
		t.Errorf("DefaultTarget = %q, want touched", DefaultTarget)
	}
	var opts RunOptions
	if opts.Target != "" {
		t.Errorf("zero value should be empty, got %q", opts.Target)
	}
	got, err := ParseTarget(string(opts.Target))
	if err != nil || got != TargetTouched {
		t.Errorf("zero value did not resolve to touched: %q %v", got, err)
	}
}

func TestCaseGroundSelectsByTarget(t *testing.T) {
	c := Case{
		TouchedExisting: []string{"a.go", "b.go", "c.go"},
		Corrected:       []string{"b.go"},
	}
	if got := c.Ground(TargetTouched); len(got) != 3 {
		t.Errorf("touched ground = %v, want 3 files", got)
	}
	if got := c.Ground(TargetCorrected); len(got) != 1 || got[0] != "b.go" {
		t.Errorf("corrected ground = %v, want [b.go]", got)
	}
	// An unknown target must not silently score against nothing.
	if got := c.Ground("nonsense"); len(got) != 3 {
		t.Errorf("unknown target should fall back to touched, got %v", got)
	}
}

func TestScorableGuardsTheCorrectiveTarget(t *testing.T) {
	thin := Case{TouchedExisting: []string{"a.go", "b.go"}, Corrected: []string{"a.go"}}
	if thin.Scorable(TargetCorrected) {
		t.Error("one corrected file should not be scorable — a single commit would be the whole result")
	}
	if !thin.Scorable(TargetTouched) {
		t.Error("the case is fine on the primary target")
	}

	ok := Case{TouchedExisting: []string{"a.go", "b.go"}, Corrected: []string{"a.go", "b.go"}}
	if !ok.Scorable(TargetCorrected) {
		t.Errorf("%d corrected files should clear a minimum of %d", len(ok.Corrected), MinCorrectedFiles)
	}
}

// A case with no corrective commits is not a miss, and scoring it zero would
// reward whichever strategy predicts nothing. It also must not be indexed
// first: rewinding and type-checking a revision costs minutes, and the answer
// is knowable from the case alone.
func TestUnscorableCasesAreSkippedBeforeIndexing(t *testing.T) {
	c := Case{
		Repo:            "/nonexistent-repo-that-would-fail-to-rewind",
		RewindTo:        "deadbeef",
		TouchedExisting: []string{"a.go", "b.go", "c.go"},
		Corrected:       nil,
	}
	res := RunCase(t.Context(), c, RunOptions{K: 10, Target: TargetCorrected})

	var unscorable *UnscorableError
	if !errors.As(res.Err, &unscorable) {
		t.Fatalf("want UnscorableError, got %v", res.Err)
	}
	if unscorable.Target != TargetCorrected {
		t.Errorf("error names target %q", unscorable.Target)
	}
	// A rewind of a nonexistent repo would have produced a different error.
	if len(res.Scores) != 0 {
		t.Error("an unscorable case was scored anyway")
	}
}

// Unscorable and degraded are different facts and a report that merges them
// makes the corrective target look like it breaks the harness.
func TestSummarizeCountsUnscorableApartFromDegraded(t *testing.T) {
	results := []CaseResult{
		{Scores: []Score{{Strategy: "lectio", Precision: 0.5}}, Collapse: CollapseMean, Target: TargetCorrected},
		{Err: &UnscorableError{Target: TargetCorrected, Have: 1}},
		{Err: &DegradedError{Health: IndexHealth{PackagesLoaded: 10, PackagesFailed: 9}}},
	}
	rep := Summarize(results, 10)

	if rep.Cases != 1 {
		t.Errorf("Cases = %d, want 1", rep.Cases)
	}
	if rep.Failed != 2 {
		t.Errorf("Failed = %d, want 2", rep.Failed)
	}
	if rep.Degraded != 1 {
		t.Errorf("Degraded = %d, want 1", rep.Degraded)
	}
	if rep.Unscorable != 1 {
		t.Errorf("Unscorable = %d, want 1", rep.Unscorable)
	}
	if rep.Target != TargetCorrected {
		t.Errorf("Target = %q, want corrected", rep.Target)
	}
}
