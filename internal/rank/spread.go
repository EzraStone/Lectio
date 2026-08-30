package rank

// A reading path confined to one file is a file, not a path.
//
// This is not a hypothetical. Run against this repository, `lectio path`
// returned ten symbols from `internal/cli/backtest.go` and gave the same reason
// for all ten: "its file changed in 36 commits this year". The README's own
// claim beside that output is that a list where every row said the same thing
// would mean six of the seven signals were decoration.
//
// The cause is structural rather than a bug in any signal. Four of the seven —
// churn, fix density, orphaning, AI density — are computed per *file*, so every
// declaration in a heavily-worked file inherits four identical contributions.
// Given a file with forty declarations and one busy month, its symbols occupy
// the whole top of the list on merit, and the ranking is behaving exactly as
// specified while producing something nobody can read.

// MaxPerFileShare bounds how much of a reading path one file may occupy.
//
// A third, rounded up. The number errs toward respecting the ranking rather
// than overriding it: the failure being prevented is ten of ten from one file,
// and any cap well below ten prevents it, so the loosest defensible fraction
// is the right one. This document has spent fourteen runs warning against
// overriding the ranking on weak grounds, and a diversity rule is a weak
// ground compared to a score.
const MaxPerFileShare = 3

// maxPerFile is how many items one file may contribute to a path of n.
func maxPerFile(n int) int {
	if n <= 0 {
		return 0
	}
	cap := (n + MaxPerFileShare - 1) / MaxPerFileShare
	if cap < 1 {
		return 1
	}
	return cap
}

// SelectSpread returns the n highest-scoring items, preferring not to take
// more than a third of them from any one file.
//
// A preference rather than a limit, and the difference is the second pass. A
// repository whose interesting code genuinely is one file should still get a
// full list — truncating it would be the tool refusing to answer because the
// answer is concentrated. So the cap only decides *order of preference* among
// positively-scored items; if honouring it would return fewer than n, the
// skipped items come back in score order.
//
// What it never does is pad. A symbol scoring zero is one no signal had
// anything to say about, and Select's refusal to recommend those is unchanged
// here.
//
// This is a selection rule, not a scoring one. No weight moves, no signal is
// touched, and the backtest reads Result.Items directly rather than going
// through here — so nothing in Gate A shifts by a thousandth because of it.
func (r *Result) SelectSpread(n int) []Item {
	if n <= 0 {
		return nil
	}

	perFile := maxPerFile(n)
	taken := make(map[string]int, n)
	out := make([]Item, 0, n)
	var skipped []Item

	for _, item := range r.Items {
		if item.Score <= 0 {
			// Items are score-ordered, so everything below this is zero too.
			break
		}
		if len(out) >= n {
			break
		}
		if taken[item.Symbol.File] >= perFile {
			skipped = append(skipped, item)
			continue
		}
		taken[item.Symbol.File]++
		out = append(out, item)
	}

	// The second pass. Skipped items are already in score order, because the
	// loop above walked them that way.
	for _, item := range skipped {
		if len(out) >= n {
			break
		}
		out = append(out, item)
	}

	// Restore score order. The first pass emitted items out of order by
	// construction — that is what the cap does — and everything downstream,
	// tiering included, assumes a score-sorted list.
	sortByScore(out)
	return out
}

// sortByScore restores the ordering Result.Items guarantees, with the same
// tiebreak so two runs agree.
func sortByScore(items []Item) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && less(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func less(x, y Item) bool {
	if x.Score != y.Score {
		return x.Score > y.Score
	}
	return x.Symbol.ID < y.Symbol.ID
}

// Files reports how many distinct files a selection spans. A reading path of
// ten items across one file and one across seven are different objects, and
// only this number tells them apart.
func Files(items []Item) int {
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		seen[it.Symbol.File] = true
	}
	return len(seen)
}
