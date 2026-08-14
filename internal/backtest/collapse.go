package backtest

import (
	"fmt"
	"sort"

	"github.com/EzraStone/Lectio/internal/rank"
)

// Collapse is how per-symbol scores become a per-file ranking.
//
// The ranker works in symbols; Gate A is scored in files. Something has to
// bridge that, and the choice is not a detail — it decides how much of the
// comparison is about ranking quality and how much is about file size.
//
// The original harness used max implicitly, by walking the score-sorted symbol
// list and taking each file at its first appearance. That reads as neutral and
// is not: a file with sixty symbols gets sixty chances to place high, a file
// with three gets three. Measured across five corpus repositories, it put
// lectio's top ten files at 3.5x the repository's median symbol count and
// overlapping the largest-files baseline 6/10 — which means the harness was
// scoring a partial reimplementation of the baseline lectio lost to.
type Collapse string

const (
	// CollapseMax scores a file by its single best symbol. Biased toward large
	// files, in proportion to how many symbols they hold.
	CollapseMax Collapse = "max"
	// CollapseMean scores a file by the average of its symbols. Size-neutral by
	// construction: adding an unremarkable symbol to a file lowers its score
	// rather than buying another lottery ticket.
	CollapseMean Collapse = "mean"
	// CollapseSum scores a file by the total. Included because it is the
	// obvious third option and because naming it makes the bias visible — sum
	// is size-weighting stated outright, and it ranks 5.0x the median.
	CollapseSum Collapse = "sum"
)

// DefaultCollapse is the mean.
//
// Not because mean is obviously right — on repositories whose signals are
// themselves size-correlated it barely moves the ranking — but because it is
// the only one of the three that does not add a size prior of its own. Any
// size effect that survives it is an effect in the signals, which is a fact
// about the ranking rather than a fact about the harness.
const DefaultCollapse = CollapseMean

// ParseCollapse validates a rule name.
func ParseCollapse(s string) (Collapse, error) {
	switch c := Collapse(s); c {
	case CollapseMax, CollapseMean, CollapseSum:
		return c, nil
	case "":
		return DefaultCollapse, nil
	default:
		return "", fmt.Errorf("unknown collapse rule %q (want max, mean, or sum)", s)
	}
}

// fileScores reduces scored symbols to scored files under the given rule.
//
// Symbols scoring zero are dropped before aggregation rather than averaged in.
// A zero score means no signal had anything to say about that symbol, and
// counting an absence of evidence as a low score would make mean punish files
// for holding trivia — which is a different bias, not the removal of one.
func fileScores(items []rank.Item, c Collapse) map[string]float64 {
	type acc struct {
		max, sum float64
		n        int
	}

	accs := make(map[string]*acc, 64)
	for _, item := range items {
		if item.Score <= 0 {
			continue
		}
		a := accs[item.Symbol.File]
		if a == nil {
			a = &acc{}
			accs[item.Symbol.File] = a
		}
		if item.Score > a.max {
			a.max = item.Score
		}
		a.sum += item.Score
		a.n++
	}

	out := make(map[string]float64, len(accs))
	for file, a := range accs {
		switch c {
		case CollapseMean:
			out[file] = a.sum / float64(a.n)
		case CollapseSum:
			out[file] = a.sum
		default:
			out[file] = a.max
		}
	}
	return out
}

// rankFloat sorts paths by descending score, breaking ties on path so a run
// repeats exactly.
func rankFloat(scores map[string]float64) []string {
	out := make([]string, 0, len(scores))
	for path, s := range scores {
		if s > 0 {
			out = append(out, path)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if scores[out[i]] != scores[out[j]] {
			return scores[out[i]] > scores[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}
