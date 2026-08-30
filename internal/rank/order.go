package rank

import "sort"

// TierCount is how many relevance buckets a reading path uses.
//
// Three, because the buckets have to mean something a person can hold: read
// this first, read this next, read this when you get to it. Ten tiers is a
// leaderboard with extra steps, and one tier is no ordering at all.
const TierCount = 3

// Sequence turns a scored list into a reading path.
//
// This is the spec's "ordering is not ranking". Relevance decides what is on
// the list; dependency depth decides the order you meet it in, because you
// cannot understand a handler before its core types. The output is a sequence,
// not a leaderboard, and the difference shows: the top-scoring item is
// frequently not the first thing to read.
//
// Tiers keep the two ideas from fighting. Ordering purely by depth would bury
// the most relevant item under every leaf utility it happens to call; ordering
// purely by score hands someone a handler before the type it takes. Bucketing
// by relevance and then sorting by depth inside each bucket gives depth
// authority over items of comparable importance and no authority across them.
func Sequence(items []Item) []Item {
	if len(items) == 0 {
		return nil
	}

	out := make([]Item, len(items))
	copy(out, items)
	assignTiers(out)

	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a], out[b]
		if x.Tier != y.Tier {
			return x.Tier < y.Tier
		}
		if x.Depth != y.Depth {
			return x.Depth < y.Depth // dependencies before dependents
		}
		if x.Score != y.Score {
			return x.Score > y.Score
		}
		return x.Symbol.ID < y.Symbol.ID
	})
	return out
}

// assignTiers buckets items by score relative to the highest.
//
// Relative to the top rather than by fixed thresholds or equal counts. Fixed
// thresholds do not survive contact with a second repo, since the absolute
// scale of a composite score depends on how many signals fired. Equal counts
// force a tier boundary through the middle of a run of near-identical scores,
// which puts two items a thousandth apart into different buckets and then
// orders them by different rules.
func assignTiers(items []Item) {
	top := items[0].Score
	for _, it := range items {
		if it.Score > top {
			top = it.Score
		}
	}
	if top <= 0 {
		for i := range items {
			items[i].Tier = TierCount
		}
		return
	}

	for i := range items {
		switch ratio := items[i].Score / top; {
		case ratio >= 0.70:
			items[i].Tier = 1
		case ratio >= 0.40:
			items[i].Tier = 2
		default:
			items[i].Tier = 3
		}
	}
}

// TierLabel names a tier for display.
func TierLabel(tier int) string {
	switch tier {
	case 1:
		return "start here"
	case 2:
		return "then these"
	default:
		return "when you get to it"
	}
}

// Path is the convenience wrapper: score, take the top n, sequence them.
//
// SelectSpread rather than Select, because this is the reading path and a
// reading path drawn from one file is a file. Four of the seven signals are
// per-file, so a heavily-worked file's declarations occupy the whole top of
// the list on merit; the cap is what keeps "read these ten things" from
// meaning "read this one file". Select is unchanged for callers that want the
// scores in order and nothing else.
func (r *Result) Path(n int) []Item {
	return Sequence(r.SelectSpread(n))
}
