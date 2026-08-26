package backtest

import (
	"fmt"
	"strings"
	"testing"
)

// perCaseReport builds a report carrying per-case scores for n cases spread
// over the given repositories, with lectio at base and largest files at base
// minus a fixed gap.
func perCaseReport(caseSet string, ids []string, lectio float64) Report {
	r := Report{
		Schema: 1, K: 10, Cases: len(ids), CaseSet: caseSet,
		Target: TargetTouched, Collapse: CollapseMean,
		Medians: map[string]float64{},
	}
	for i, id := range ids {
		repo := strings.SplitN(id, "@", 2)[0]
		for _, s := range []struct {
			name    string
			matched float64
		}{
			{"lectio", lectio + 0.01*float64(i%5)},
			{"largest files", 0.50 + 0.01*float64(i%3)},
		} {
			r.PerCase = append(r.PerCase, CaseScore{
				Case: id, Repo: repo, Strategy: s.name,
				Precision: 0.40, Matched: s.matched, Pairs: 10,
			})
		}
	}
	return r
}

// caseIDs builds n identifiers spread over repos repositories.
func caseIDs(prefix string, repos, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("repo%02d@abc%d/%s%d", i%repos, i, prefix, i))
	}
	return out
}

func TestCaseIDsAreDistinctAndSorted(t *testing.T) {
	r := perCaseReport("aaa", caseIDs("c", 4, 12), 0.53)
	ids := CaseIDs(r)
	if len(ids) != 12 {
		t.Fatalf("got %d ids from 12 cases and 2 strategies each", len(ids))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("ids are not sorted: %q then %q", ids[i-1], ids[i])
		}
	}
}

func TestSharedCasesSplitsThreeWays(t *testing.T) {
	all := caseIDs("c", 5, 30)
	a := perCaseReport("aaa", all[:25], 0.53) // 0..24
	b := perCaseReport("bbb", all[10:], 0.55) // 10..29

	shared, onlyA, onlyB := SharedCases(a, b)
	if len(shared) != 15 || onlyA != 10 || onlyB != 5 {
		t.Errorf("got shared=%d onlyA=%d onlyB=%d, want 15/10/5", len(shared), onlyA, onlyB)
	}
}

func TestRestrictToRecomputesEverythingItKeeps(t *testing.T) {
	ids := caseIDs("c", 10, 40)
	full := perCaseReport("aaa", ids, 0.53)
	full.Aggregates = []Aggregate{{Strategy: "lectio", PrecisionA: 0.99, MatchedA: 0.99}}
	full.Verdict = Verdict{Note: "a verdict about 40 cases"}
	full.Strata = []StratumAggregate{{Strategy: "lectio", Stratum: 0, Precision: 0.9}}
	full.Sweep = []RatioAggregate{{Ratio: 2, Strategy: "lectio", Matched: 0.9}}

	keep := map[string]bool{}
	for _, id := range ids[:20] {
		keep[id] = true
	}
	got := RestrictTo(full, keep)

	if got.Cases != 20 {
		t.Errorf("restricted report has %d cases, want 20", got.Cases)
	}
	if len(got.Aggregates) != 2 {
		t.Fatalf("got %d aggregates, want one per strategy", len(got.Aggregates))
	}
	for _, a := range got.Aggregates {
		if a.PrecisionA > 0.5 {
			t.Errorf("%s kept the full run's precision %.3f instead of recomputing", a.Strategy, a.PrecisionA)
		}
		if a.Cases != 20 {
			t.Errorf("%s reports %d cases, want 20", a.Strategy, a.Cases)
		}
		if a.MatchedCI.Units != 10 {
			t.Errorf("%s bootstrapped over %d units, want the 10 repositories", a.Strategy, a.MatchedCI.Units)
		}
	}

	// Everything not carried per case is dropped, not inherited. A verdict
	// computed over 40 cases would look like a real claim about these 20.
	if got.Verdict.Note != "" {
		t.Errorf("the verdict survived the restriction: %q", got.Verdict.Note)
	}
	if len(got.Strata) != 0 || len(got.Sweep) != 0 {
		t.Errorf("strata (%d) or sweep (%d) survived a restriction that cannot recompute them",
			len(got.Strata), len(got.Sweep))
	}
}

// A restricted report has to name itself, or two restrictions of the same
// subset would refuse to compare with each other.
func TestRestrictToRenamesTheCaseSet(t *testing.T) {
	ids := caseIDs("c", 10, 40)
	a := perCaseReport("aaa", ids, 0.53)
	b := perCaseReport("bbb", ids[:30], 0.55)

	keep := map[string]bool{}
	for _, id := range ids[:30] {
		keep[id] = true
	}
	ra, rb := RestrictTo(a, keep), RestrictTo(b, keep)

	if ra.CaseSet == "aaa" || rb.CaseSet == "bbb" {
		t.Error("a restricted report kept the identity of the run it came from")
	}
	if ra.CaseSet != rb.CaseSet {
		t.Errorf("two restrictions of the same 30 cases fingerprint differently: %s vs %s",
			ra.CaseSet, rb.CaseSet)
	}
}

func TestRestrictToNothingIsEmpty(t *testing.T) {
	got := RestrictTo(perCaseReport("aaa", caseIDs("c", 4, 10), 0.53), map[string]bool{})
	if got.Cases != 0 || len(got.Aggregates) != 0 {
		t.Errorf("got %d cases and %d aggregates from an empty subset", got.Cases, len(got.Aggregates))
	}
}

// The point of the whole file: two runs on different populations now compare,
// on the population they share.
func TestCompareIntersectsDifferentCaseSets(t *testing.T) {
	all := caseIDs("c", 10, 60)
	a := perCaseReport("aaa", all[:50], 0.53)
	b := perCaseReport("bbb", all[10:], 0.56)

	c := Compare(a, b)
	if !c.Comparable {
		t.Fatalf("two runs sharing 40 cases refused to compare: %s", c.Why)
	}
	if !c.Intersected {
		t.Error("the comparison did not report that it intersected")
	}
	if c.SharedCases != 40 || c.DroppedFromA != 10 || c.DroppedFromB != 10 {
		t.Errorf("got shared=%d droppedA=%d droppedB=%d, want 40/10/10",
			c.SharedCases, c.DroppedFromA, c.DroppedFromB)
	}
	// And the deltas are between the restricted aggregates, not the originals.
	for _, row := range c.Rows {
		if row.Strategy != "lectio" {
			continue
		}
		if row.MatchedB <= row.MatchedA {
			t.Errorf("lectio scored %.3f then %.3f — b was built to be higher", row.MatchedA, row.MatchedB)
		}
	}
}

// Too small an overlap is still a refusal. Comparing two dozen numbers over
// six shared cases is worse than declining.
func TestCompareStillRefusesATinyOverlap(t *testing.T) {
	all := caseIDs("c", 10, 60)
	a := perCaseReport("aaa", all[:30], 0.53)
	b := perCaseReport("bbb", all[25:], 0.56)

	c := Compare(a, b)
	if c.Comparable {
		t.Error("two runs sharing 5 cases compared cleanly")
	}
	if !strings.Contains(c.Why, "they share 5") {
		t.Errorf("the refusal does not say how many cases overlap: %q", c.Why)
	}
	if c.Intersected {
		t.Error("a refused comparison claimed to have intersected")
	}
}

// A report written before per-case scores existed cannot be intersected, and
// the refusal has to say that rather than reporting an overlap of zero as if
// it were measured.
func TestCompareSaysWhenTheOverlapCannotBeComputed(t *testing.T) {
	a := perCaseReport("aaa", caseIDs("c", 10, 40), 0.53)
	b := a
	b.CaseSet = "bbb"
	b.PerCase = nil

	c := Compare(a, b)
	if c.Comparable {
		t.Error("a report with no per-case scores compared across case sets")
	}
	if !strings.Contains(c.Why, "predates per-case reporting") {
		t.Errorf("the refusal blames the overlap rather than the missing data: %q", c.Why)
	}
}

// Identical case sets take the direct path and must not report an
// intersection, or every ordinary comparison would look like a rebuilt one.
func TestCompareOnTheSameCaseSetDoesNotIntersect(t *testing.T) {
	ids := caseIDs("c", 10, 40)
	a := perCaseReport("aaa", ids, 0.53)
	b := perCaseReport("aaa", ids, 0.56)

	c := Compare(a, b)
	if !c.Comparable {
		t.Fatalf("identical case sets refused: %s", c.Why)
	}
	if c.Intersected || c.SharedCases != 0 {
		t.Errorf("an ordinary comparison reported an intersection of %d cases", c.SharedCases)
	}
}
