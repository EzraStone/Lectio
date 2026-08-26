package backtest

import (
	"fmt"
	"sort"
)

// Comparison is one report set beside another.
type Comparison struct {
	// Comparable is false when the two runs measured different populations,
	// which makes every delta below meaningless.
	Comparable bool
	// Why explains an incomparable pair in words.
	Why string
	// Rows are per-strategy deltas, ordered by the size of the change.
	Rows []ComparisonRow
	// OnlyInA and OnlyInB name strategies present in one run and not the
	// other.
	OnlyInA []string
	OnlyInB []string
	// Intersected is true when the two runs scored different case sets and the
	// comparison was rebuilt over the cases they share.
	Intersected bool
	// SharedCases, DroppedFromA and DroppedFromB describe that rebuild. They
	// are the first thing to read on an intersected comparison: a diff over 40
	// shared cases out of 161 and 74 is a different object from a diff over
	// 150 of 161.
	SharedCases  int
	DroppedFromA int
	DroppedFromB int
}

// ComparisonRow is one strategy's before and after.
type ComparisonRow struct {
	Strategy string
	// Precision and Matched are the two headline measures. Delta fields are
	// B minus A, so a positive number means the second run scored higher.
	PrecisionA, PrecisionB, PrecisionDelta float64
	MatchedA, MatchedB, MatchedDelta       float64
	// SignFlip marks a matched-pair result that crossed chance. A strategy
	// moving from 52% to 48% has not merely got worse, it has changed what it
	// claims — and that is invisible in a delta of four points.
	SignFlip bool
	// MatchedOverlap is true when the two runs' matched-pair intervals overlap,
	// which means the delta beside them is not a finding.
	//
	// This project has already made that mistake once. Removing orphaning
	// looked worth three points on one corpus and was worth −0.1 on another,
	// and the three-point version was quoted for weeks because a delta printed
	// beside no interval reads as a measurement. A row where the intervals
	// overlap is a row where the second run has not shown the first was wrong.
	MatchedOverlap bool
	// MatchedIntervals is false when either run predates the interval work, so
	// a reader can tell "the intervals overlap" from "there are no intervals".
	MatchedIntervals bool
}

// Decisive reports whether a matched-pair delta is large enough, relative to
// its own error, to be worth acting on.
func (r ComparisonRow) Decisive() bool {
	return r.MatchedIntervals && !r.MatchedOverlap
}

// Compare sets two reports side by side.
//
// The first thing it decides is whether the comparison is legitimate at all.
// Which cases survive a run depends on whether each rewound revision's
// dependencies resolved, so the same command can measure 85 cases one day and
// 77 the next — and a delta between two different populations is not a
// measurement of anything. Two runs are comparable when their case sets match.
//
// Reporting the deltas anyway, with a warning, would be worse than refusing:
// the numbers are the part people quote.
func Compare(a, b Report) Comparison {
	var c Comparison

	// Before anything else: if the two runs landed on different populations
	// but both carry per-case numbers, rebuild them over the cases they share.
	// That is a real comparison where refusing outright is merely a correct
	// one, and after ten runs the same command has never twice produced the
	// same case set.
	var intersection struct {
		shared, onlyA, onlyB int
		applied              bool
	}
	if a.CaseSet != "" && b.CaseSet != "" && a.CaseSet != b.CaseSet {
		shared, onlyA, onlyB := SharedCases(a, b)
		if len(shared) >= MinSharedCases {
			keep := make(map[string]bool, len(shared))
			for _, id := range shared {
				keep[id] = true
			}
			a, b = RestrictTo(a, keep), RestrictTo(b, keep)
			intersection.shared, intersection.onlyA, intersection.onlyB = len(shared), onlyA, onlyB
			intersection.applied = true
		}
	}

	switch {
	case a.CaseSet == "" || b.CaseSet == "":
		c.Why = "one of the runs scored no cases"
	case a.CaseSet != b.CaseSet:
		shared, _, _ := SharedCases(a, b)
		detail := fmt.Sprintf("they share %d", len(shared))
		if len(a.PerCase) == 0 || len(b.PerCase) == 0 {
			detail = "at least one predates per-case reporting, so the overlap cannot be computed"
		}
		c.Why = fmt.Sprintf(
			"different case sets (%s vs %s) — %d cases against %d, and %s, below the %d "+
				"needed to compare on the overlap. Which cases survive depends on whether each "+
				"revision's dependencies resolved, so these two runs measured different "+
				"populations and the deltas would not mean anything",
			a.CaseSet, b.CaseSet, a.Cases, b.Cases, detail, MinSharedCases)
	case a.Target != b.Target:
		c.Why = fmt.Sprintf("different targets (%s vs %s) — precision over declarations is "+
			"not comparable with precision over file paths", a.Target, b.Target)
	case a.Collapse != b.Collapse && !a.Target.Symbolic():
		c.Why = fmt.Sprintf("different collapse rules (%s vs %s), which is worth several points "+
			"on its own", a.Collapse, b.Collapse)
	case a.SizeRatio.Or(MaxSizeRatio) != b.SizeRatio.Or(MaxSizeRatio):
		// Precision is unaffected by the pairing ratio, but the matched column
		// is the reason this command exists, and a looser ratio admits more
		// pairs while leaving more size information inside each one. Two runs
		// at different ratios computed two different measures.
		c.Why = fmt.Sprintf("different size-matching ratios (%.2fx vs %.2fx) — a looser ratio "+
			"admits more pairs and leaves more size inside each one, so the matched columns "+
			"are not the same measure",
			a.SizeRatio.Or(MaxSizeRatio), b.SizeRatio.Or(MaxSizeRatio))
	default:
		c.Comparable = true
	}
	if intersection.applied {
		c.Intersected = true
		c.SharedCases = intersection.shared
		c.DroppedFromA = intersection.onlyA
		c.DroppedFromB = intersection.onlyB
	}

	inA := byStrategy(a)
	inB := byStrategy(b)

	for name, av := range inA {
		bv, ok := inB[name]
		if !ok {
			c.OnlyInA = append(c.OnlyInA, name)
			continue
		}
		row := ComparisonRow{
			Strategy:       name,
			PrecisionA:     av.PrecisionA,
			PrecisionB:     bv.PrecisionA,
			PrecisionDelta: bv.PrecisionA - av.PrecisionA,
			MatchedA:       av.MatchedA,
			MatchedB:       bv.MatchedA,
			MatchedDelta:   bv.MatchedA - av.MatchedA,
		}
		// Only meaningful when both runs actually measured it.
		if av.MatchedA > 0 && bv.MatchedA > 0 {
			row.SignFlip = (av.MatchedA > MatchedChance) != (bv.MatchedA > MatchedChance)
		}
		if av.MatchedCI.Units > 0 && bv.MatchedCI.Units > 0 {
			row.MatchedIntervals = true
			row.MatchedOverlap = intervalsOverlap(av.MatchedCI, bv.MatchedCI)
		}
		c.Rows = append(c.Rows, row)
	}
	for name := range inB {
		if _, ok := inA[name]; !ok {
			c.OnlyInB = append(c.OnlyInB, name)
		}
	}

	sort.Strings(c.OnlyInA)
	sort.Strings(c.OnlyInB)
	sort.Slice(c.Rows, func(i, j int) bool {
		ai, aj := abs(c.Rows[i].PrecisionDelta), abs(c.Rows[j].PrecisionDelta)
		if ai != aj {
			return ai > aj
		}
		return c.Rows[i].Strategy < c.Rows[j].Strategy
	})
	return c
}

func byStrategy(r Report) map[string]Aggregate {
	out := make(map[string]Aggregate, len(r.Aggregates))
	for _, a := range r.Aggregates {
		out[a.Strategy] = a
	}
	return out
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// intervalsOverlap reports whether two intervals share any value.
//
// Overlap is a conservative test and deliberately so. Two non-overlapping 95%
// intervals are firm evidence of a difference; two that overlap are not proof
// of no difference, only of insufficient evidence for one. Reading it the
// stronger way would be the same error in the opposite direction.
func intervalsOverlap(a, b Interval) bool {
	return a.Lo <= b.Hi && b.Lo <= a.Hi
}
