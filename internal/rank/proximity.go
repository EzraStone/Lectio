package rank

import (
	"math"
	"path"
	"sort"
	"strings"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/graph"
	"github.com/EzraStone/Lectio/internal/index"
)

// Proximity scores how near a symbol is to the area the user named.
//
// Source: graph distance from a user-supplied scope. Why it matters: it is the
// difference between "what should I read to work here" and "what should I read
// eventually", and it is what makes the tool useful in month three rather than
// only in week one.
//
// Optional by design. With no task named it contributes nothing at all, and
// the ranking is the general onboarding one.
type Proximity struct{}

// Signal implements Computer.
func (Proximity) Signal() Signal { return SignalProximity }

// Normalized marks this signal as already living in [0,1].
//
// Percentile normalization would be actively wrong here. It is relative to the
// population, so in a repo where nothing is within nine hops of the named area
// the nearest symbol would still score 1 — reporting "very close to your work"
// about something that is not. Distance means the same thing in every repo and
// deserves an absolute scale.
func (Proximity) Normalized() bool { return true }

// proximityDecay is the hop distance at which relevance has fallen by half.
// Two hops is close enough to be part of the same change; beyond about four,
// the connection is real but not something anyone would call related.
const proximityDecay = 2.0

// Compute returns exponential decay in hop distance from the task seeds.
func (Proximity) Compute(v *index.View, p Params) Scores {
	if len(p.Task) == 0 || v.Calls == nil || v.Calls.N() == 0 {
		return Scores{}
	}

	seeds := make([]string, 0, len(p.Task))
	for _, id := range p.Task {
		seeds = append(seeds, string(id))
	}

	dist := graph.Proximity(v.Calls, seeds)
	out := make(Scores, len(dist))
	for i, d := range dist {
		if d < 0 {
			continue // unreachable: no relationship at all, not a distant one
		}
		id := core.SymbolID(v.Calls.ID(i))
		if sym, ok := v.Symbols[id]; !ok || sym.IsTest() {
			continue
		}
		out[id] = math.Exp2(-float64(d) / proximityDecay)
	}
	return out
}

// ResolveTask turns what a user typed into graph seeds.
//
// The input is whatever came most easily to hand — a package path, a
// directory, a file, a function name, or a fragment of one. Someone who has
// been in a codebase for four days does not know its vocabulary yet, which is
// the entire reason they are running this, so the resolver accepts all of them
// and reports what it matched rather than demanding a canonical form.
//
// Matching is ordered by decreasing precision. An exact hit stops the search,
// so naming a real package never also drags in every symbol whose name happens
// to contain that word.
func ResolveTask(v *index.View, query string) (seeds []core.SymbolID, matched string) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, ""
	}
	q = strings.Trim(strings.ToLower(strings.ReplaceAll(q, "\\", "/")), "/")

	if seeds := matchExactSymbol(v, q); len(seeds) > 0 {
		return seeds, "symbol " + string(seeds[0])
	}
	if seeds, pkg := matchPackage(v, q); len(seeds) > 0 {
		return seeds, "package " + pkg
	}
	if seeds, dir := matchPath(v, q); len(seeds) > 0 {
		return seeds, "path " + dir
	}
	if seeds := matchSymbolName(v, q); len(seeds) > 0 {
		return seeds, "symbols matching " + query
	}
	return nil, ""
}

func matchExactSymbol(v *index.View, q string) []core.SymbolID {
	for id, sym := range v.Symbols {
		if sym.IsTest() {
			continue
		}
		if strings.ToLower(string(id)) == q || strings.ToLower(id.Short()) == q {
			return []core.SymbolID{id}
		}
	}
	return nil
}

// matchPackage prefers an exact package path, then the longest suffix match,
// so "billing" finds "github.com/acme/service/internal/billing" without also
// matching "github.com/acme/service/internal/billingreports".
func matchPackage(v *index.View, q string) ([]core.SymbolID, string) {
	packages := make(map[string]bool)
	for _, sym := range v.Symbols {
		if !sym.IsTest() && sym.Package != "" {
			packages[sym.Package] = true
		}
	}

	var best string
	for pkg := range packages {
		lower := strings.ToLower(pkg)
		switch {
		case lower == q:
			best = pkg
		case strings.HasSuffix(lower, "/"+q):
			if best == "" || len(pkg) < len(best) {
				best = pkg
			}
		}
		if strings.ToLower(best) == q {
			break
		}
	}
	if best == "" {
		return nil, ""
	}

	var seeds []core.SymbolID
	for id, sym := range v.Symbols {
		if sym.Package == best && !sym.IsTest() {
			seeds = append(seeds, id)
		}
	}
	sortIDs(seeds)
	return seeds, best
}

// matchPath accepts a file or a directory prefix.
func matchPath(v *index.View, q string) ([]core.SymbolID, string) {
	var seeds []core.SymbolID
	for id, sym := range v.Symbols {
		if sym.IsTest() {
			continue
		}
		file := strings.ToLower(sym.File)
		if file == q || strings.HasPrefix(file, q+"/") || path.Dir(file) == q {
			seeds = append(seeds, id)
		}
	}
	sortIDs(seeds)
	return seeds, q
}

// matchSymbolName is the last resort: a substring of a symbol name.
func matchSymbolName(v *index.View, q string) []core.SymbolID {
	var seeds []core.SymbolID
	for id, sym := range v.Symbols {
		if !sym.IsTest() && strings.Contains(strings.ToLower(sym.Name), q) {
			seeds = append(seeds, id)
		}
	}
	sortIDs(seeds)

	// A fragment matching most of the repo is not a task scope, it is a common
	// word. Scoping to everything is the same as scoping to nothing, and the
	// honest response is to say nothing matched.
	if len(seeds) > len(v.Symbols)/4 {
		return nil
	}
	return seeds
}

func sortIDs(ids []core.SymbolID) {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
}
