package backtest

import "sort"

// A report scores ten strategies against the same cases and prints a p-value
// beside each. At the conventional 0.05 that is about one nominal significance
// per report by construction, and the last run produced exactly that: churn-only
// at p = 0.035, the first sign test in thirteen runs to come in under the line,
// on a table of ten.
//
// Noticing that took knowing to look. It should not.

// Holm applies the Holm–Bonferroni step-down correction to a family of
// p-values and returns the adjusted values in the input's order.
//
// Holm rather than plain Bonferroni because it is uniformly more powerful and
// costs one sort: Bonferroni multiplies every p by the family size, Holm
// multiplies the smallest by m, the next by m−1, and so on, so a genuinely
// strong result is not punished for the weak ones beside it. Both control the
// same thing — the probability of *any* false positive in the family — and
// neither assumes the tests are independent, which matters here because the
// strategies are scored on identical cases and are anything but.
//
// The running maximum is what makes the result monotone: without it a larger
// raw p could adjust to a smaller value than the one below it, and a table
// ordered by adjusted p would contradict a table ordered by raw p.
func Holm(ps []float64) []float64 {
	m := len(ps)
	if m == 0 {
		return nil
	}
	order := make([]int, m)
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return ps[order[a]] < ps[order[b]] })

	out := make([]float64, m)
	running := 0.0
	for rank, idx := range order {
		adjusted := float64(m-rank) * ps[idx]
		if adjusted > 1 {
			adjusted = 1
		}
		if adjusted > running {
			running = adjusted
		}
		out[idx] = running
	}
	return out
}

// applyHolm fills in the adjusted sign-test p across a report's strategies.
//
// The family is every strategy the report prints a p for. That is the family a
// reader actually scans, which is the one the correction has to be over: the
// error being controlled is "I looked at ten numbers and quoted the smallest".
//
// It is conservative for a second reason worth stating. Leave-one-out ablation
// variants are not independent hypotheses — they are the same ranking with one
// signal removed — so the effective number of tests is smaller than the count.
// Correcting over the count anyway means a variant that survives has survived
// more than it strictly had to, which is the direction to err in a document
// that has already retracted one finding.
func applyHolm(aggs []Aggregate) {
	var idx []int
	var ps []float64
	for i, a := range aggs {
		if a.MatchedRepos.Above+a.MatchedRepos.Below == 0 {
			continue
		}
		idx = append(idx, i)
		ps = append(ps, a.MatchedRepos.P)
	}
	if len(idx) < 2 {
		// One test is not a family, and adjusting it would only invite the
		// reader to wonder what it was adjusted against.
		return
	}
	for j, adjusted := range Holm(ps) {
		aggs[idx[j]].MatchedRepos.PAdjusted = adjusted
		aggs[idx[j]].MatchedRepos.Family = len(idx)
	}
}
