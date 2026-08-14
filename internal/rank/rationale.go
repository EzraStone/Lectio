package rank

import (
	"fmt"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/graph"
	"github.com/EzraStone/Lectio/internal/index"
)

// Facts holds the concrete numbers behind a ranking, so rationales can quote
// evidence instead of scores.
//
// "0.94 centrality" tells a reader nothing they can check. "nine callers"
// tells them something they can go and verify in thirty seconds, and being
// checkable is what makes the difference between a tool someone argues with
// and a tool someone stops trusting.
type Facts struct {
	DirectDependents map[core.SymbolID]int
	TransitiveDeps   map[core.SymbolID]int

	Commits       map[string]int
	Fixes         map[string]int
	OrphanedShare map[string]float64
	AIShare       map[string]float64
	TopPair       map[string]Pair

	Distance map[core.SymbolID]int
}

// Gather computes the evidence behind a ranking, once.
func Gather(v *index.View, p Params) *Facts {
	f := &Facts{
		DirectDependents: make(map[core.SymbolID]int),
		TransitiveDeps:   make(map[core.SymbolID]int),
		Commits:          make(map[string]int),
		Fixes:            make(map[string]int),
		AIShare:          make(map[string]float64),
		TopPair:          make(map[string]Pair),
		Distance:         make(map[core.SymbolID]int),
	}

	for i := 0; i < v.Calls.N(); i++ {
		id := core.SymbolID(v.Calls.ID(i))
		f.DirectDependents[id] = v.Calls.InDegree(i)
	}
	// Transitive dependents are bounded at three hops. Beyond that, in a
	// well-connected service the answer is "most of the repo", which is true
	// and useless as a reason to read something.
	reverse := v.Calls.Reverse()
	for id := range v.Symbols {
		if i, ok := reverse.Index(string(id)); ok {
			f.TransitiveDeps[id] = len(graph.Reachable(reverse, []int{i}, 3)) - 1
		}
	}

	window := p.ChurnWindow
	if window <= 0 {
		window = 365 * 24 * time.Hour
	}
	cutoff := p.Now.Add(-window)

	aiLines := make(map[string]int)
	totalLines := make(map[string]int)
	for _, c := range v.Commits {
		if c.When.Before(cutoff) {
			continue
		}
		isFix := c.IsFix() || c.IsRevert()
		for _, ch := range c.Files {
			f.Commits[ch.Path]++
			if isFix {
				f.Fixes[ch.Path]++
			}
			totalLines[ch.Path] += ch.Added
			if c.AIAssisted {
				aiLines[ch.Path] += ch.Added
			}
		}
	}
	for path, total := range totalLines {
		if total > 0 && aiLines[path] > 0 {
			f.AIShare[path] = float64(aiLines[path]) / float64(total)
		}
	}

	f.OrphanedShare = OrphanedShare(v, p.Now)

	for _, pair := range (HiddenCoupling{}).Pairs(v, p) {
		if !pair.Hidden {
			continue
		}
		// Pairs come back strongest-first, so the first sighting of a file is
		// its strongest coupling.
		if _, seen := f.TopPair[pair.A]; !seen {
			f.TopPair[pair.A] = pair
		}
		if _, seen := f.TopPair[pair.B]; !seen {
			f.TopPair[pair.B] = pair
		}
	}

	if len(p.Task) > 0 {
		seeds := make([]string, 0, len(p.Task))
		for _, id := range p.Task {
			seeds = append(seeds, string(id))
		}
		for i, d := range graph.Proximity(v.Calls, seeds) {
			if d >= 0 {
				f.Distance[core.SymbolID(v.Calls.ID(i))] = d
			}
		}
	}
	return f
}

// Explain writes the one-line reason an item is on the list.
//
// One reason, not all seven. A list where every entry recites the same six
// clauses is a list nobody reads past the third row; the strongest signal is
// what the person needs to know, and the rest is available by asking.
func (f *Facts) Explain(item Item, taskLabel string) string {
	sig, _ := item.Top()
	switch sig {
	case SignalCoupling:
		if pair, ok := f.TopPair[item.Symbol.File]; ok {
			other := pair.B
			if other == item.Symbol.File {
				other = pair.A
			}
			return fmt.Sprintf("changes with %s in %d commits, and neither one imports the other",
				other, pair.Together)
		}

	case SignalCentrality:
		direct := f.DirectDependents[item.Symbol.ID]
		transitive := f.TransitiveDeps[item.Symbol.ID]
		if transitive > direct {
			return fmt.Sprintf("%s %s it directly, %d within three hops",
				plural(direct, "caller"), agree(direct, "call"), transitive)
		}
		if direct > 0 {
			return fmt.Sprintf("%s across the repo", plural(direct, "caller"))
		}

	case SignalChurn:
		if n := f.Commits[item.Symbol.File]; n > 0 {
			return fmt.Sprintf("its file changed in %s this year", plural(n, "commit"))
		}

	case SignalFixDensity:
		fixes, total := f.Fixes[item.Symbol.File], f.Commits[item.Symbol.File]
		if fixes > 0 && total > 0 {
			return fmt.Sprintf("%d of its %d recent commits were fixes or reverts", fixes, total)
		}

	case SignalOrphaning:
		if share, ok := f.OrphanedShare[item.Symbol.File]; ok && share > 0 {
			return fmt.Sprintf("%d%% of it was written by people who have not committed in 90 days",
				int(share*100+0.5))
		}

	case SignalAIDensity:
		if share, ok := f.AIShare[item.Symbol.File]; ok && share > 0 {
			return fmt.Sprintf("%d%% of its recent lines came from machine-assisted commits",
				int(share*100+0.5))
		}

	case SignalProximity:
		if d, ok := f.Distance[item.Symbol.ID]; ok {
			if d == 0 {
				return fmt.Sprintf("inside %s", orDefault(taskLabel, "the area you named"))
			}
			return fmt.Sprintf("%s from %s", plural(d, "hop"), orDefault(taskLabel, "the area you named"))
		}
	}

	// Reached when the strongest signal has no quotable evidence — a symbol
	// carried by a broad, even spread rather than by one thing. Saying that
	// plainly beats inventing a specific reason.
	if item.Symbol.Doc != "" {
		return item.Symbol.Doc
	}
	return "ranks high across several signals without one standing out"
}

// Annotate fills in the rationale for each item.
func Annotate(items []Item, f *Facts, taskLabel string) []Item {
	out := make([]Item, len(items))
	copy(out, items)
	for i := range out {
		out[i].Rationale = f.Explain(out[i], taskLabel)
	}
	return out
}

// agree conjugates a verb for a subject of n things.
func agree(n int, verb string) string {
	if n == 1 {
		return verb + "s"
	}
	return verb
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
