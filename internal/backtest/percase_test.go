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

// The shape --cases 5 against --cases 12 produces, which is the comparison
// this project keeps making. Nothing was measured in one run and not the
// other, so it is a question about sample size and not about population.
func TestCompareDetectsANestedCaseSet(t *testing.T) {
	all := caseIDs("c", 10, 60)
	small := perCaseReport("aaa", all[:40], 0.53)
	large := perCaseReport("bbb", all, 0.53)

	c := Compare(small, large)
	if !c.Comparable || !c.Intersected {
		t.Fatalf("nested runs did not intersect: %s", c.Why)
	}
	if !c.Nested {
		t.Error("a strict subset was not reported as nested")
	}
	if c.DroppedFromA != 0 || c.DroppedFromB != 20 {
		t.Errorf("got droppedA=%d droppedB=%d, want 0 and 20", c.DroppedFromA, c.DroppedFromB)
	}
}

func TestCompareDetectsNestingInEitherDirection(t *testing.T) {
	all := caseIDs("c", 10, 60)
	small := perCaseReport("aaa", all[:40], 0.53)
	large := perCaseReport("bbb", all, 0.53)

	if c := Compare(large, small); !c.Nested || c.DroppedFromB != 0 {
		t.Errorf("the wider run first was not reported as nested: %+v", c)
	}
}

// Two runs that each reached cases the other did not are not nested, and
// calling them so would understate a real population difference.
func TestOverlappingButNotNestedIsNotNested(t *testing.T) {
	all := caseIDs("c", 10, 60)
	a := perCaseReport("aaa", all[:50], 0.53)
	b := perCaseReport("bbb", all[10:], 0.53)

	c := Compare(a, b)
	if !c.Intersected {
		t.Fatalf("overlapping runs did not intersect: %s", c.Why)
	}
	if c.Nested {
		t.Error("two runs that each reached unique cases were reported as nested")
	}
}

// The property the real data showed and the whole intersection rests on:
// restricting the wider run to the narrower one's cases reproduces the
// narrower run exactly.
func TestRestrictingAWiderRunReproducesTheNarrowerOne(t *testing.T) {
	all := caseIDs("c", 10, 60)
	narrow := perCaseReport("aaa", all[:40], 0.53)
	wide := perCaseReport("bbb", all, 0.53)

	keep := map[string]bool{}
	for _, id := range CaseIDs(narrow) {
		keep[id] = true
	}
	got := RestrictTo(wide, keep)
	want := RestrictTo(narrow, keep)

	if got.Cases != want.Cases || got.CaseSet != want.CaseSet {
		t.Fatalf("restricted to %d cases (%s), want %d (%s)",
			got.Cases, got.CaseSet, want.Cases, want.CaseSet)
	}
	byName := map[string]Aggregate{}
	for _, a := range want.Aggregates {
		byName[a.Strategy] = a
	}
	for _, a := range got.Aggregates {
		w := byName[a.Strategy]
		if a.PrecisionA != w.PrecisionA || a.MatchedA != w.MatchedA {
			t.Errorf("%s: got precision %.6f matched %.6f, want %.6f and %.6f",
				a.Strategy, a.PrecisionA, a.MatchedA, w.PrecisionA, w.MatchedA)
		}
		if a.MatchedCI != w.MatchedCI {
			t.Errorf("%s: intervals differ after restriction", a.Strategy)
		}
	}
}

// A replay is only useful if it reproduces the run. Everything derivable from
// the per-case scores has to come back identical, and everything not derivable
// has to be carried rather than dropped.
func TestReplayReproducesTheReport(t *testing.T) {
	ids := caseIDs("c", 12, 60)
	full := perCaseReport("aaa", ids, 0.53)
	// Recompute the aggregates the way a run would, so the fixture is a report
	// a replay should be a fixed point of.
	all := map[string]bool{}
	for _, id := range ids {
		all[id] = true
	}
	full = RestrictTo(full, all)
	full.Schema = ReportSchema
	full.Failed, full.Degraded, full.Unscorable = 41, 12, 3
	full.FailureReasons = map[string]int{"nothing type-checkable at that revision": 37}
	full.Coverage = MatchedCoverage{Scored: 60, Unpairable: 4, Ground: 900, Paired: 600}
	full.Strata = []StratumAggregate{{Strategy: "lectio", Stratum: 0, Precision: 0.245, Spread: 23}}
	full.Sweep = []RatioAggregate{{Ratio: 1.1, Strategy: "lectio", Matched: 0.518, Cases: 36, Pairs: 696}}

	got, err := Replay(full)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	if got.CaseSet != full.CaseSet || got.Cases != full.Cases {
		t.Errorf("replay changed the population: %s/%d vs %s/%d",
			got.CaseSet, got.Cases, full.CaseSet, full.Cases)
	}
	if len(got.Aggregates) != len(full.Aggregates) {
		t.Fatalf("got %d aggregates, want %d", len(got.Aggregates), len(full.Aggregates))
	}
	for i, a := range got.Aggregates {
		w := full.Aggregates[i]
		if a.Strategy != w.Strategy {
			t.Errorf("aggregate %d is %s, want %s — the table reordered", i, a.Strategy, w.Strategy)
		}
		if a.PrecisionA != w.PrecisionA || a.MatchedA != w.MatchedA || a.MatchedCI != w.MatchedCI {
			t.Errorf("%s changed through a replay: %+v vs %+v", a.Strategy, a, w)
		}
	}
	// Carried, not recomputed: a replay covers exactly the cases the run did.
	if got.Failed != 41 || got.Degraded != 12 || got.Coverage.Scored != 60 {
		t.Errorf("a replay dropped the run's failure and coverage counts: %+v", got)
	}
	if len(got.Strata) != 1 || len(got.Sweep) != 1 {
		t.Errorf("a replay dropped strata (%d) or sweep (%d)", len(got.Strata), len(got.Sweep))
	}
}

// Recall and MRR were left out of CaseScore at first. A report replayed
// without them prints two columns of zeroes that look like measurements.
func TestReplayKeepsRecallAndMRR(t *testing.T) {
	ids := caseIDs("c", 10, 40)
	r := perCaseReport("aaa", ids, 0.53)
	for i := range r.PerCase {
		r.PerCase[i].Recall = 0.29
		r.PerCase[i].MRR = 0.647
	}
	// Through RestrictTo first, so the fixture carries a real fingerprint —
	// "aaa" is not one, and Replay is right to refuse it.
	all := map[string]bool{}
	for _, id := range ids {
		all[id] = true
	}
	r = RestrictTo(r, all)

	got, err := Replay(r)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	for _, a := range got.Aggregates {
		closeTo(t, a.RecallA, 0.29, 1e-9, a.Strategy+" recall")
		closeTo(t, a.MRR, 0.647, 1e-9, a.Strategy+" MRR")
	}
}

func TestReplayRefusesAReportWithoutPerCaseScores(t *testing.T) {
	r := perCaseReport("aaa", caseIDs("c", 4, 10), 0.53)
	r.PerCase = nil
	if _, err := Replay(r); err == nil {
		t.Error("a report with no per-case scores replayed without complaint")
	} else if !strings.Contains(err.Error(), "no per-case scores") {
		t.Errorf("the error does not say what is missing: %v", err)
	}
}

// A file whose per-case scores disagree with its header has been edited, and
// silently trusting either half would be worse than failing.
func TestReplayCatchesAnAlteredFile(t *testing.T) {
	r := perCaseReport("aaa", caseIDs("c", 10, 40), 0.53)
	all := map[string]bool{}
	for _, id := range CaseIDs(r) {
		all[id] = true
	}
	r = RestrictTo(r, all)
	r.CaseSet = "deadbeef1234"

	if _, err := Replay(r); err == nil {
		t.Error("a report whose header disagrees with its scores replayed cleanly")
	} else if !strings.Contains(err.Error(), "altered since it was written") {
		t.Errorf("the error does not name the problem: %v", err)
	}
}

// A strategy in the per-case scores that the aggregates never named is a
// broken file, but dropping it silently is worse than showing it last.
func TestRestrictKeepsAnUnlistedStrategy(t *testing.T) {
	ids := caseIDs("c", 10, 40)
	r := perCaseReport("aaa", ids, 0.53)
	r.Aggregates = []Aggregate{{Strategy: "lectio"}}
	all := map[string]bool{}
	for _, id := range ids {
		all[id] = true
	}
	got := RestrictTo(r, all)

	if len(got.Aggregates) != 2 {
		t.Fatalf("got %d aggregates, want lectio plus the unlisted one", len(got.Aggregates))
	}
	if got.Aggregates[0].Strategy != "lectio" {
		t.Errorf("the listed strategy is not first: %s", got.Aggregates[0].Strategy)
	}
	if got.Aggregates[1].Strategy != "largest files" {
		t.Errorf("the unlisted strategy is %s, want largest files", got.Aggregates[1].Strategy)
	}
}

// Reports written before CaseScore carried recall and MRR still have them in
// their aggregates, and a replay covers exactly the same cases — so the stored
// means are still true of it. Recomputing them as zero is the wrong answer.
func TestReplayCarriesRecallFromReportsThatPredateIt(t *testing.T) {
	ids := caseIDs("c", 10, 40)
	r := perCaseReport("aaa", ids, 0.53)
	all := map[string]bool{}
	for _, id := range ids {
		all[id] = true
	}
	r = RestrictTo(r, all) // no per-case recall or MRR in this fixture
	for i := range r.Aggregates {
		r.Aggregates[i].RecallA, r.Aggregates[i].MRR = 0.292, 0.647
	}

	got, err := Replay(r)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	for _, a := range got.Aggregates {
		if a.RecallA != 0.292 || a.MRR != 0.647 {
			t.Errorf("%s came back with recall %.3f and MRR %.3f, want the stored 0.292 and 0.647",
				a.Strategy, a.RecallA, a.MRR)
		}
	}
}

// When the per-case scores do carry recall, that is what wins — a stored mean
// over a different set of strategies must not override a recomputed one.
func TestReplayPrefersRecomputedRecall(t *testing.T) {
	ids := caseIDs("c", 10, 40)
	r := perCaseReport("aaa", ids, 0.53)
	for i := range r.PerCase {
		r.PerCase[i].Recall = 0.31
	}
	all := map[string]bool{}
	for _, id := range ids {
		all[id] = true
	}
	r = RestrictTo(r, all)
	for i := range r.Aggregates {
		r.Aggregates[i].RecallA = 0.99
	}

	got, err := Replay(r)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	for _, a := range got.Aggregates {
		closeTo(t, a.RecallA, 0.31, 1e-9, a.Strategy+" recall")
	}
}

// A restriction can change the family the correction is over — a strategy
// present in the source and absent from the subset is one fewer test — so the
// adjusted p has to be recomputed rather than carried.
func TestRestrictRecomputesTheCorrection(t *testing.T) {
	ids := caseIDs("c", 10, 40)
	r := perCaseReport("aaa", ids, 0.53)
	all := map[string]bool{}
	for _, id := range ids {
		all[id] = true
	}
	r = RestrictTo(r, all)

	if len(r.Aggregates) != 2 {
		t.Fatalf("got %d aggregates, want 2", len(r.Aggregates))
	}
	for _, a := range r.Aggregates {
		if a.MatchedRepos.Family != 2 {
			t.Errorf("%s reports a family of %d, want the 2 strategies present",
				a.Strategy, a.MatchedRepos.Family)
		}
	}
}

// A replay recomputes the correction too, which is the point: reports written
// before it existed get it on being re-read.
func TestReplayAppliesTheCorrection(t *testing.T) {
	ids := caseIDs("c", 12, 48)
	r := perCaseReport("aaa", ids, 0.53)
	all := map[string]bool{}
	for _, id := range ids {
		all[id] = true
	}
	r = RestrictTo(r, all)
	// Strip it, as a report written before the correction would be.
	for i := range r.Aggregates {
		r.Aggregates[i].MatchedRepos.PAdjusted = 0
		r.Aggregates[i].MatchedRepos.Family = 0
	}

	got, err := Replay(r)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	for _, a := range got.Aggregates {
		if a.MatchedRepos.Above+a.MatchedRepos.Below == 0 {
			continue
		}
		if a.MatchedRepos.Family == 0 {
			t.Errorf("%s came back uncorrected after a replay", a.Strategy)
		}
		if a.MatchedRepos.PAdjusted < a.MatchedRepos.P {
			t.Errorf("%s adjusted %.4f down to %.4f",
				a.Strategy, a.MatchedRepos.P, a.MatchedRepos.PAdjusted)
		}
	}
}
