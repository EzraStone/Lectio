package backtest

import (
	"fmt"
	"sort"
)

// After ten runs the case set has moved on every repeat of every command: 85
// then 77, 138 then 161, 77 then 74. Which cases survive depends on whether
// each rewound revision's dependencies resolve, which depends on the module
// cache and on the network.
//
// Compare refuses across two different case sets, and that refusal is correct —
// a delta between two populations is not a measurement. It is also a dead end
// in practice, because two runs of the *same command* land on different
// populations, so the tool that exists to diff two runs can rarely diff any
// two runs anyone actually has.
//
// The way out is the one every paired study uses: compare on the cases both
// runs reached. That needs per-case numbers in the report, which is what this
// file adds — and it makes a report re-analyzable without re-running it, which
// is worth the kilobytes on its own.

// CaseScore is one strategy's numbers on one case.
//
// Deliberately narrow. The full CaseResult carries predictions, strata and
// timings, and serializing those would multiply a report's size by an order of
// magnitude for data no comparison reads.
type CaseScore struct {
	// Case is the same identity the fingerprint is built from.
	Case string `json:"case"`
	// Repo is carried separately because the bootstrap clusters on it, and
	// parsing it back out of Case would be a second place to get that wrong.
	Repo      string  `json:"repo"`
	Strategy  string  `json:"strategy"`
	Precision float64 `json:"precision"`
	// Recall and MRR complete the headline row. They were left out of the
	// first version on the grounds that no comparison reads them, which was
	// true of comparisons and false of replays: a report re-rendered without
	// them prints two columns of zeroes that look like measurements.
	Recall  float64 `json:"recall,omitzero"`
	MRR     float64 `json:"mrr,omitzero"`
	Matched float64 `json:"matched,omitzero"`
	Pairs   int     `json:"pairs,omitzero"`
}

// caseID is the identity a case is known by across runs.
//
// Repository, rewind point and contributor: the three things that define what
// was measured. Two runs that both reached this case measured the same thing,
// whatever else differed between them.
func caseID(c Case) string {
	return c.Repo + "@" + c.RewindTo + "/" + c.Contributor
}

// collectPerCase pulls the per-case scores out of a run's results.
func collectPerCase(results []CaseResult) []CaseScore {
	var out []CaseScore
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		id := caseID(r.Case)
		for _, s := range r.Scores {
			out = append(out, CaseScore{
				Case:      id,
				Repo:      r.Case.Repo,
				Strategy:  s.Strategy,
				Precision: s.Precision,
				Recall:    s.Recall,
				MRR:       s.MRR,
				Matched:   s.Matched,
				Pairs:     s.Pairs,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Case != out[j].Case {
			return out[i].Case < out[j].Case
		}
		return out[i].Strategy < out[j].Strategy
	})
	return out
}

// CaseIDs returns the distinct cases a report scored, sorted.
func CaseIDs(r Report) []string {
	seen := map[string]bool{}
	var out []string
	for _, cs := range r.PerCase {
		if !seen[cs.Case] {
			seen[cs.Case] = true
			out = append(out, cs.Case)
		}
	}
	sort.Strings(out)
	return out
}

// SharedCases returns the cases both reports scored, and the counts each ran
// alone.
func SharedCases(a, b Report) (shared []string, onlyA, onlyB int) {
	inB := map[string]bool{}
	for _, id := range CaseIDs(b) {
		inB[id] = true
	}
	for _, id := range CaseIDs(a) {
		if inB[id] {
			shared = append(shared, id)
			delete(inB, id)
			continue
		}
		onlyA++
	}
	return shared, onlyA, len(inB)
}

// MinSharedCases is the fewest shared cases an intersection may be built from.
//
// Twenty is below what any run in this project has produced and well above
// what a handful of coincidences would give. It exists so the intersection
// path degrades into a refusal rather than into a comparison of two dozen
// numbers computed from six cases.
const MinSharedCases = 20

// RestrictTo re-aggregates a report over a subset of its cases.
//
// Everything derivable from the per-case scores is recomputed: means,
// intervals, repository consistency, medians. Everything not carried per case
// is dropped rather than copied — strata, the sweep, the verdict, the failure
// counts. A restricted report is a comparison input, not a run.
//
// Dropping rather than copying matters. A verdict computed over 161 cases
// carried onto a 74-case subset would be a claim about a population that was
// never scored, and it would look exactly like a real one.
func RestrictTo(r Report, keep map[string]bool) Report {
	out := Report{
		Schema:    r.Schema,
		K:         r.K,
		Target:    r.Target,
		Collapse:  r.Collapse,
		Variants:  r.Variants,
		SizeRatio: r.SizeRatio,
		Medians:   map[string]float64{},
	}

	precisions := map[string][]float64{}
	recalls := map[string][]float64{}
	mrrs := map[string][]float64{}
	matchedObs := map[string][]Observation{}
	matchedWins := map[string]float64{}
	matchedPairs := map[string]int{}
	cases := map[string]bool{}

	// Strategy order follows the report being restricted, not the order the
	// per-case scores happen to be sorted in. The table's order is a choice —
	// lectio first, controls last — and rebuilding it alphabetically would
	// silently reorder every replayed report.
	order := strategyOrder(r)
	known := make(map[string]bool, len(order))
	for _, name := range order {
		known[name] = true
	}

	for _, cs := range r.PerCase {
		if !keep[cs.Case] {
			continue
		}
		cases[cs.Case] = true
		if !known[cs.Strategy] {
			// A strategy in the per-case scores that the aggregates never
			// named. Should not happen, but dropping it silently would be
			// worse than appending it at the end.
			known[cs.Strategy] = true
			order = append(order, cs.Strategy)
		}
		precisions[cs.Strategy] = append(precisions[cs.Strategy], cs.Precision)
		recalls[cs.Strategy] = append(recalls[cs.Strategy], cs.Recall)
		mrrs[cs.Strategy] = append(mrrs[cs.Strategy], cs.MRR)
		if cs.Pairs > 0 {
			matchedObs[cs.Strategy] = append(matchedObs[cs.Strategy], Observation{
				Repo: cs.Repo, Accuracy: cs.Matched, Pairs: cs.Pairs,
			})
			matchedWins[cs.Strategy] += cs.Matched * float64(cs.Pairs)
			matchedPairs[cs.Strategy] += cs.Pairs
		}
		out.PerCase = append(out.PerCase, cs)
	}
	out.Cases = len(cases)
	if out.Cases == 0 {
		return out
	}

	for _, name := range order {
		if len(precisions[name]) == 0 {
			// Named in the aggregates but absent from the kept subset.
			continue
		}
		a := Mean(name, precisions[name], recalls[name], mrrs[name])
		if obs := matchedObs[name]; len(obs) > 0 {
			a.MatchedA = meanAccuracy(obs)
			a.MatchedCI = BootstrapInterval(obs, DefaultLevel, BootstrapIters, bootstrapSeed)
			a.MatchedWilson = WilsonInterval(matchedWins[name], matchedPairs[name], DefaultLevel)
			a.MatchedRepos = RepoConsistency(obs)
			a.MatchedByRepo = ByRepo(obs)
			if n := len(obs); n > out.MatchedCases {
				out.MatchedCases = n
			}
			if n := matchedPairs[name]; n > out.MatchedPairs {
				out.MatchedPairs = n
			}
		}
		out.Aggregates = append(out.Aggregates, a)
		out.Medians[name] = Median(precisions[name])
	}

	// Recomputed, not copied: the fingerprint has to describe the subset, or
	// the restricted reports would refuse to compare with each other.
	out.CaseSet = fingerprintIDs(sortedKeys(cases))
	return out
}

// Replay recomputes a stored report's aggregates from its per-case scores,
// using whatever the current code does with them.
//
// This exists because the analysis has changed more often than the data. Every
// interval, every sign test and every leak check in this package landed after
// the runs they were needed for, and each one cost another pass over the
// corpus — forty minutes to add a column to numbers that were already on disk.
// The per-case scores are the run; the aggregates are a view of it.
//
// What a replay cannot do is change what was measured. The pairing ratio, the
// collapse rule and the target are properties of the run, and a replay carries
// them forward untouched. So is the sweep, which was computed from pairings a
// replay no longer has. Strata are kept for the same reason: they are scored
// per band at index time and are not derivable from a per-case score.
func Replay(r Report) (Report, error) {
	if len(r.PerCase) == 0 {
		return r, fmt.Errorf("this report carries no per-case scores, so there is nothing to "+
			"recompute — it predates them, or was written by a run that scored no cases "+
			"(case set %q, %d cases)", r.CaseSet, r.Cases)
	}

	keep := make(map[string]bool, len(r.PerCase))
	for _, cs := range r.PerCase {
		keep[cs.Case] = true
	}
	out := RestrictTo(r, keep)

	// RestrictTo drops what a *subset* cannot claim. A replay is not a subset:
	// it covers exactly the cases the original did, so everything describing
	// that population is still true of it.
	out.Schema = r.Schema
	out.Failed, out.Degraded, out.Unscorable = r.Failed, r.Degraded, r.Unscorable
	out.FailureReasons = r.FailureReasons
	out.Coverage = r.Coverage
	out.Strata = r.Strata
	out.Sweep = r.Sweep

	// Recall and MRR were added to CaseScore after the first reports were
	// written, so replaying one of those recomputes them as zero. A replay
	// covers exactly the cases the run did, which makes the stored means still
	// true of it — carrying them is right where recomputing them is not.
	stored := byStrategy(r)
	for i, a := range out.Aggregates {
		if a.RecallA != 0 || a.MRR != 0 {
			continue
		}
		if was, ok := stored[a.Strategy]; ok {
			out.Aggregates[i].RecallA, out.Aggregates[i].MRR = was.RecallA, was.MRR
		}
	}

	// The verdict is re-derived rather than copied. It is a reading of the
	// aggregates, and the aggregates have just been recomputed; carrying the
	// old one forward is how a stale conclusion outlives the numbers under it.
	out.Verdict = decide(out)

	if out.CaseSet != r.CaseSet {
		// The fingerprint is over the same case IDs both times, so this cannot
		// happen without the per-case scores disagreeing with the header —
		// which means the file was edited or truncated.
		return out, fmt.Errorf("replaying %s produced case set %s: the per-case scores do not "+
			"match the header, so this file has been altered since it was written",
			r.CaseSet, out.CaseSet)
	}
	return out, nil
}

// strategyOrder returns the order a report's table was printed in.
func strategyOrder(r Report) []string {
	out := make([]string, 0, len(r.Aggregates))
	for _, a := range r.Aggregates {
		out = append(out, a.Strategy)
	}
	return out
}
