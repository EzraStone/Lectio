package backtest

import (
	"fmt"
	"strings"
	"testing"
)

// coverageOf records observations as (ground, pairs), one per repository, so
// the repository count matches the scored count unless a test says otherwise.
func coverageOf(observations ...[2]int) MatchedCoverage {
	var c MatchedCoverage
	seen := map[string]bool{}
	for i, o := range observations {
		c.observeCoverage(o[0], o[1], fmt.Sprintf("repo%02d", i), seen)
	}
	return c
}

// coverageIn records every observation against one repository.
func coverageIn(repo string, observations ...[2]int) MatchedCoverage {
	var c MatchedCoverage
	seen := map[string]bool{}
	for _, o := range observations {
		c.observeCoverage(o[0], o[1], repo, seen)
	}
	return c
}

func TestCoverageSplitsTheTwoLimits(t *testing.T) {
	c := coverageOf(
		[2]int{20, 12}, // scored
		[2]int{15, 8},  // scored
		[2]int{10, 3},  // built pairs, below the floor
		[2]int{12, 0},  // nothing pairable at all
		[2]int{8, 0},   // nothing pairable at all
	)
	if c.Scored != 2 || c.BelowFloor != 1 || c.Unpairable != 2 {
		t.Errorf("got scored=%d belowFloor=%d unpairable=%d, want 2/1/2",
			c.Scored, c.BelowFloor, c.Unpairable)
	}
	if c.Cases() != 5 {
		t.Errorf("Cases() = %d, want 5", c.Cases())
	}
	if c.Ground != 65 || c.Paired != 23 {
		t.Errorf("got ground=%d paired=%d, want 65 and 23", c.Ground, c.Paired)
	}
	closeTo(t, c.Share(), 23.0/65.0, 1e-12, "paired share")
}

// A case with no answer set was never offered to the pairing, and counting it
// as a coverage failure would blame the bound for a case that has no ground
// truth at this target.
func TestCoverageIgnoresCasesWithNoGroundTruth(t *testing.T) {
	c := coverageOf([2]int{0, 0}, [2]int{20, 12})
	if c.Cases() != 1 || c.Scored != 1 {
		t.Errorf("got %d cases (%d scored), want 1 and 1", c.Cases(), c.Scored)
	}
	if c.Ground != 20 {
		t.Errorf("ground = %d, want 20 — the empty case contributed", c.Ground)
	}
}

// The summary has to name which limit is binding, because the two point at
// different fixes and only one of them is expensive.
func TestCoverageNamesTheBindingLimit(t *testing.T) {
	tight := coverageOf(
		[2]int{20, 12}, [2]int{12, 0}, [2]int{8, 0}, [2]int{9, 0}, [2]int{10, 2},
	)
	got := tight.Summary()
	if !strings.Contains(got, "found no size-matched twin at all") ||
		!strings.Contains(got, "--size-ratio") {
		t.Errorf("a bound-limited run does not point at the bound:\n%s", got)
	}

	thin := coverageOf(
		[2]int{20, 12}, [2]int{10, 3}, [2]int{9, 2}, [2]int{8, 4}, [2]int{12, 0},
	)
	got = thin.Summary()
	if !strings.Contains(got, "the corpus being thin rather than the bound being tight") {
		t.Errorf("a floor-limited run does not point at the corpus:\n%s", got)
	}
	if strings.Contains(got, "--size-ratio") {
		t.Errorf("a floor-limited run recommends loosening the bound:\n%s", got)
	}
}

func TestCoverageSummaryOnAFullyPairedRun(t *testing.T) {
	got := coverageOf([2]int{20, 12}, [2]int{15, 9}).Summary()
	if !strings.Contains(got, "every case cleared the floor") {
		t.Errorf("a fully paired run does not say so:\n%s", got)
	}
}

func TestCoverageSummaryOnNothing(t *testing.T) {
	if got := (MatchedCoverage{}).Summary(); got != "" {
		t.Errorf("got %q from no cases", got)
	}
	if got := (MatchedCoverage{}).Share(); got != 0 {
		t.Errorf("Share() = %v with no ground truth", got)
	}
}

// The numbers the write-up quotes, in the shape it quotes them: at file
// granularity the pairing reached 33 of 77 cases, and the summary has to make
// the denominator unmissable.
func TestCoverageSummaryStatesBothDenominators(t *testing.T) {
	var c MatchedCoverage
	seen := map[string]bool{}
	for i := 0; i < 33; i++ {
		c.observeCoverage(20, 10, fmt.Sprintf("repo%02d", i%17), seen)
	}
	for i := 0; i < 44; i++ {
		c.observeCoverage(20, 0, fmt.Sprintf("repo%02d", i%17), seen)
	}
	got := c.Summary()
	for _, want := range []string{
		"33 of 77 cases across 17 repositories", "330 of 1540 touched items", "(21%)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary is missing %q:\n%s", want, got)
		}
	}
}

// The repository count is what bounds the file-level measure, and it is the
// number raising --cases does not move: more cases in the same repositories
// buy pairs and no power, because both the bootstrap and the sign test
// resample repositories.
func TestCoverageCountsRepositoriesNotCases(t *testing.T) {
	one := coverageIn("only-repo", [2]int{20, 12}, [2]int{20, 12}, [2]int{20, 12}, [2]int{20, 12})
	if one.Scored != 4 {
		t.Fatalf("scored %d cases, want 4", one.Scored)
	}
	if one.Repos != 1 {
		t.Errorf("four cases from one repository counted %d repositories", one.Repos)
	}

	spread := coverageOf([2]int{20, 12}, [2]int{20, 12}, [2]int{20, 12}, [2]int{20, 12})
	if spread.Repos != 4 {
		t.Errorf("four cases from four repositories counted %d", spread.Repos)
	}
}

// A repository whose only cases fell below the floor contributed nothing to
// the interval or the sign test, and must not be counted as though it had.
func TestCoverageCountsOnlyRepositoriesThatScored(t *testing.T) {
	c := coverageIn("thin", [2]int{20, 2}, [2]int{20, 3})
	if c.Repos != 0 {
		t.Errorf("a repository with no scored case counted %d", c.Repos)
	}
	if c.BelowFloor != 2 {
		t.Errorf("got %d below the floor, want 2", c.BelowFloor)
	}
}
