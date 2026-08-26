package backtest

import "sort"

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
	Matched   float64 `json:"matched,omitzero"`
	Pairs     int     `json:"pairs,omitzero"`
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
	matchedObs := map[string][]Observation{}
	matchedWins := map[string]float64{}
	matchedPairs := map[string]int{}
	var order []string
	cases := map[string]bool{}

	for _, cs := range r.PerCase {
		if !keep[cs.Case] {
			continue
		}
		cases[cs.Case] = true
		if _, seen := precisions[cs.Strategy]; !seen {
			order = append(order, cs.Strategy)
		}
		precisions[cs.Strategy] = append(precisions[cs.Strategy], cs.Precision)
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
		a := Aggregate{
			Strategy:   name,
			Cases:      len(precisions[name]),
			PrecisionA: mean(precisions[name]),
		}
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
