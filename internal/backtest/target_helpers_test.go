package backtest

import "testing"

// CommitsFor decides which of a contributor's commits get attributed. Getting
// it backwards would score the corrective target against every commit they
// made, which is the primary target wearing a different label.
func TestCommitsForSelectsByTarget(t *testing.T) {
	c := Case{
		Commits:           []string{"a", "b", "c", "d"},
		CorrectiveCommits: []string{"b", "d"},
	}

	if got := c.CommitsFor(TargetSymbols); len(got) != 4 {
		t.Errorf("symbols target got %v, want all four commits", got)
	}
	if got := c.CommitsFor(TargetCorrectedSymbols); len(got) != 2 {
		t.Errorf("corrected-symbols target got %v, want the two corrective commits", got)
	}
	if got := c.CommitsFor(TargetTouched); len(got) != 4 {
		t.Errorf("touched target got %v, want all four", got)
	}
	if got := c.CommitsFor(TargetCorrected); len(got) != 2 {
		t.Errorf("corrected target got %v, want two", got)
	}
}

func TestTargetPredicates(t *testing.T) {
	for _, tc := range []struct {
		target               Target
		symbolic, corrective bool
	}{
		{TargetTouched, false, false},
		{TargetCorrected, false, true},
		{TargetSymbols, true, false},
		{TargetCorrectedSymbols, true, true},
	} {
		if got := tc.target.Symbolic(); got != tc.symbolic {
			t.Errorf("%s.Symbolic() = %v, want %v", tc.target, got, tc.symbolic)
		}
		if got := tc.target.Corrective(); got != tc.corrective {
			t.Errorf("%s.Corrective() = %v, want %v", tc.target, got, tc.corrective)
		}
	}
}

// An unknown target must not silently behave like the corrective one, which
// would score against a fraction of the evidence and report it as the whole.
func TestUnknownTargetFallsBackToTheFullSet(t *testing.T) {
	c := Case{Commits: []string{"a", "b"}, CorrectiveCommits: []string{"a"}}
	if got := c.CommitsFor("nonsense"); len(got) != 2 {
		t.Errorf("an unknown target got %v, want the full commit set", got)
	}
}

// Scorable gates the expensive part. A symbolic target with no commits at all
// can never produce ground truth, and indexing to discover that costs minutes.
func TestScorableOnSymbolicTargets(t *testing.T) {
	none := Case{TouchedExisting: []string{"a.go", "b.go", "c.go"}}
	if none.Scorable(TargetSymbols) {
		t.Error("a case with no commits is scorable on the symbols target")
	}
	if none.Scorable(TargetCorrectedSymbols) {
		t.Error("a case with no corrective commits is scorable on corrected-symbols")
	}

	some := Case{
		TouchedExisting:   []string{"a.go"},
		Commits:           []string{"x"},
		CorrectiveCommits: []string{"x"},
	}
	if !some.Scorable(TargetSymbols) || !some.Scorable(TargetCorrectedSymbols) {
		t.Error("a case with commits is not scorable")
	}
}
