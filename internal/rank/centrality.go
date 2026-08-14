package rank

import (
	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/graph"
	"github.com/EzraStone/Lectio/internal/index"
)

// Centrality scores a symbol by how much of the codebase leans on it.
//
// Source: reverse PageRank over the call graph. Why it matters: high fan-in
// means you cannot avoid it — you will meet this symbol whatever you are asked
// to work on, so meeting it deliberately beats meeting it during an incident.
//
// This signal is computable by anyone and roughly matches intuition, which is
// exactly why it cannot be the differentiator. It is the floor: a ranking that
// does not respect centrality is obviously broken, and one that only respects
// centrality is a list of your biggest files with extra steps.
type Centrality struct{}

// Signal implements Computer.
func (Centrality) Signal() Signal { return SignalCentrality }

// Compute runs PageRank over the full call graph, including CHA's
// over-approximated edges.
//
// Including dynamic edges here is deliberate and is the opposite of what
// grading does. For "how entangled is this symbol", a call that might reach it
// is still a reason to read it — the person maintaining an interface has to
// understand every implementation behind it, whether or not this particular
// program takes that branch.
func (Centrality) Compute(v *index.View, p Params) Scores {
	if v.Calls == nil || v.Calls.N() == 0 {
		return Scores{}
	}

	ranks := graph.Centrality(v.Calls)
	out := make(Scores, len(ranks))
	for i, r := range ranks {
		id := core.SymbolID(v.Calls.ID(i))
		sym, ok := v.Symbols[id]
		if !ok || sym.IsTest() {
			continue
		}
		out[id] = r
	}
	return out
}

// Depth reports how deep a symbol sits in the dependency graph. It is not one
// of the seven scored signals — it never contributes to relevance — but the
// ordering step needs it, and computing it here keeps the graph work in one
// place.
func Depth(v *index.View) map[core.SymbolID]int {
	out := make(map[core.SymbolID]int, len(v.Symbols))
	for id, d := range graph.DepthByID(v.Calls) {
		out[core.SymbolID(id)] = d
	}
	return out
}

// Dependents counts direct callers, used in rationale text where a raw count
// reads better than a percentile ("nine callers" beats "0.94 centrality").
func Dependents(v *index.View) map[core.SymbolID]int {
	out := make(map[core.SymbolID]int, len(v.Symbols))
	for i := 0; i < v.Calls.N(); i++ {
		out[core.SymbolID(v.Calls.ID(i))] = v.Calls.InDegree(i)
	}
	return out
}
