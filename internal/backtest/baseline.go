package backtest

import (
	"sort"
	"time"

	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
)

// Baseline is a cheap ranking strategy the real ranker must beat.
//
// All four are things anyone could compute in an afternoon. That is the point:
// if a seven-signal ranking cannot beat "show me the biggest files", the seven
// signals are decoration.
type Baseline interface {
	Name() string
	// RankFiles returns repo-relative paths, most relevant first.
	RankFiles(v *index.View, p rank.Params) []string
}

// Baselines returns the four from the spec, in the spec's order.
func Baselines() []Baseline {
	return []Baseline{
		LargestFiles{},
		MostChurned{},
		MostRecent{},
		MostAuthors{},
	}
}

// LargestFiles ranks by the total lines its symbols span, the cheapest
// available proxy for size.
//
// Size is measured over symbol spans rather than raw file bytes because the
// index holds symbols and does not hold file lengths — and because the
// difference is meaningful rather than incidental: imports, licence headers
// and a thousand lines of generated tables are not a bigger read than two
// hundred lines of dense logic, and none of them carry a symbol.
//
// This is the baseline that won the first run at scale, which makes its
// definition load-bearing. FileSizes is the single implementation, shared with
// the size controls, so "size" cannot come to mean two different things in one
// report.
type LargestFiles struct{}

// Name implements Baseline.
func (LargestFiles) Name() string { return "largest files" }

// RankFiles implements Baseline.
func (LargestFiles) RankFiles(v *index.View, _ rank.Params) []string {
	return rankByScore(FileSizes(v))
}

// MostChurned ranks by commits touching the file in the window.
//
// The strongest of the four, and the one the real ranker has to beat by a
// visible margin rather than a rounding error — churn genuinely predicts what
// people touch next, which is exactly why the spec warns that a ranking which
// only reproduces churn has optimized the proxy instead of the goal.
type MostChurned struct{}

// Name implements Baseline.
func (MostChurned) Name() string { return "most churned, 12mo" }

// RankFiles implements Baseline.
func (MostChurned) RankFiles(v *index.View, p rank.Params) []string {
	cutoff := windowStart(p)
	counts := make(map[string]int)
	for _, c := range v.Commits {
		if c.When.Before(cutoff) {
			continue
		}
		for _, f := range c.Files {
			counts[f.Path]++
		}
	}
	return rankByScore(filterToIndexed(v, counts))
}

// MostRecent ranks by last modification time.
type MostRecent struct{}

// Name implements Baseline.
func (MostRecent) Name() string { return "most recently modified" }

// RankFiles implements Baseline.
func (MostRecent) RankFiles(v *index.View, _ rank.Params) []string {
	latest := make(map[string]int64)
	for _, c := range v.Commits {
		ts := c.When.Unix()
		for _, f := range c.Files {
			if ts > latest[f.Path] {
				latest[f.Path] = ts
			}
		}
	}

	scores := make(map[string]int, len(latest))
	for path, ts := range latest {
		scores[path] = int(ts)
	}
	return rankByScore(filterToIndexed(v, scores))
}

// MostAuthors ranks by distinct contributors.
type MostAuthors struct{}

// Name implements Baseline.
func (MostAuthors) Name() string { return "most distinct authors" }

// RankFiles implements Baseline.
func (MostAuthors) RankFiles(v *index.View, p rank.Params) []string {
	cutoff := windowStart(p)
	authors := make(map[string]map[string]bool)
	for _, c := range v.Commits {
		if c.When.Before(cutoff) {
			continue
		}
		who := c.Author()
		for _, f := range c.Files {
			if authors[f.Path] == nil {
				authors[f.Path] = make(map[string]bool)
			}
			authors[f.Path][who] = true
		}
	}

	counts := make(map[string]int, len(authors))
	for path, set := range authors {
		counts[path] = len(set)
	}
	return rankByScore(filterToIndexed(v, counts))
}

// Lectio wraps the real ranker as a strategy, so it is scored by exactly the
// same code path as the baselines. Anything else invites the comparison to be
// quietly unfair.
type Lectio struct {
	// Label distinguishes ablation variants in a report. Empty means the
	// shipped weighting.
	Label   string
	Weights rank.Weights
	// Collapse is how symbol scores become file scores. Empty means
	// DefaultCollapse, and the zero value being the unbiased rule is
	// deliberate — the biased one should have to be asked for by name.
	Collapse Collapse
}

// Name implements Baseline.
func (l Lectio) Name() string {
	if l.Label == "" {
		return "lectio"
	}
	return l.Label
}

// RankFiles implements Baseline.
//
// The ranker works in symbols and the comparison is in files, so symbol scores
// collapse to a file score under l.Collapse. See collapse.go for why that
// choice is not a formality — the rule this originally used was max, and max
// hands large files more chances to place well.
func (l Lectio) RankFiles(v *index.View, p rank.Params) []string {
	w := l.Weights
	if w == nil {
		w = rank.DefaultWeights()
	}
	c := l.Collapse
	if c == "" {
		c = DefaultCollapse
	}

	return rankFloat(fileScores(rank.Rank(v, p, w).Items, c))
}

// rankByScore sorts paths by descending score, breaking ties on path so runs
// are reproducible.
func rankByScore(scores map[string]int) []string {
	out := make([]string, 0, len(scores))
	for path, n := range scores {
		if n > 0 {
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

// filterToIndexed drops paths that carry no indexed source.
//
// Without this the history-driven baselines rank README.md and go.sum, which
// are genuinely the most-churned files in many repos. That would not be a fair
// fight — it would be a broken one, and beating a broken baseline proves
// nothing.
func filterToIndexed(v *index.View, scores map[string]int) map[string]int {
	indexed := make(map[string]bool)
	for _, sym := range v.Symbols {
		if !sym.IsTest() {
			indexed[sym.File] = true
		}
	}

	out := make(map[string]int, len(scores))
	for path, n := range scores {
		if indexed[path] {
			out[path] = n
		}
	}
	return out
}

func windowStart(p rank.Params) time.Time {
	window := p.ChurnWindow
	if window <= 0 {
		window = 365 * 24 * time.Hour
	}
	return p.Now.Add(-window)
}
