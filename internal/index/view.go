package index

import (
	"context"
	"sort"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/graph"
	"github.com/EzraStone/Lectio/internal/store"
)

// View is the in-memory read model. Ranking, probes, and the reading path all
// run against it, and none of them touch SQL.
//
// It holds two call graphs, which is the single most important thing in this
// file. Calls includes CHA's over-approximated edges and is what ranking uses,
// because for "how entangled is this symbol" a possible caller is still a
// reason to read it. StaticCalls holds only type-checker-proven edges and is
// what grading uses. A probe answer is never scored against an edge the tool
// merely suspects.
type View struct {
	Root    string
	Head    string
	Symbols map[core.SymbolID]core.Symbol

	Calls       *graph.Graph
	StaticCalls *graph.Graph

	// CoveredBy maps a symbol to the test binaries that exercise it.
	CoveredBy map[core.SymbolID][]core.SymbolID

	Commits    []core.Commit
	Imports    []core.ImportEdge
	Authorship []core.Authorship

	// Now is the reference time for every time-dependent signal. Fixing it
	// once means a ranking run is reproducible and a backtest can pretend to
	// stand at an arbitrary date.
	Now time.Time
}

// Load reads the index into memory.
func Load(ctx context.Context, s *store.Store) (*View, error) {
	v := &View{
		Symbols:   make(map[core.SymbolID]core.Symbol),
		CoveredBy: make(map[core.SymbolID][]core.SymbolID),
		Now:       time.Now().UTC(),
	}

	var err error
	if v.Root, err = s.Meta(ctx, "root"); err != nil {
		return nil, err
	}
	if v.Head, err = s.Meta(ctx, "head"); err != nil {
		return nil, err
	}

	syms, err := s.Symbols(ctx)
	if err != nil {
		return nil, err
	}
	for _, sym := range syms {
		v.Symbols[sym.ID] = sym
	}

	edges, err := s.CallEdges(ctx, false)
	if err != nil {
		return nil, err
	}
	v.Calls = graph.New(len(syms))
	v.StaticCalls = graph.New(len(syms))
	// Seed both graphs with every symbol so an isolated one still has a node.
	// Without this, a function nothing calls and that calls nothing simply
	// vanishes from ranking — and "nobody depends on this" is a fact about it,
	// not a reason to pretend it does not exist.
	for _, sym := range syms {
		v.Calls.Add(string(sym.ID))
		v.StaticCalls.Add(string(sym.ID))
	}
	for _, e := range edges {
		v.Calls.AddEdge(string(e.Caller), string(e.Callee))
		if e.Confident() {
			v.StaticCalls.AddEdge(string(e.Caller), string(e.Callee))
		}
	}
	v.Calls.Dedup()
	v.StaticCalls.Dedup()

	cov, err := s.Coverage(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range cov {
		if c.Covers() {
			v.CoveredBy[c.Symbol] = append(v.CoveredBy[c.Symbol], c.Test)
		}
	}
	for _, tests := range v.CoveredBy {
		sort.Slice(tests, func(i, j int) bool { return tests[i] < tests[j] })
	}

	if v.Commits, err = s.Commits(ctx, time.Time{}); err != nil {
		return nil, err
	}
	if v.Imports, err = s.ImportEdges(ctx); err != nil {
		return nil, err
	}
	if v.Authorship, err = s.Authorship(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

// Readable returns the symbols eligible for a reading path: production code
// only. Test functions are indexed because coverage needs them, but nobody
// onboards by reading the test suite in ranked order.
func (v *View) Readable() []core.Symbol {
	out := make([]core.Symbol, 0, len(v.Symbols))
	for _, s := range v.Symbols {
		if s.IsTest() {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SymbolsInFile returns the symbols declared in a file, in source order.
func (v *View) SymbolsInFile(path string) []core.Symbol {
	var out []core.Symbol
	for _, s := range v.Symbols {
		if s.File == path {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartLine < out[j].StartLine })
	return out
}

// Files returns every path carrying indexed symbols.
func (v *View) Files() []string {
	seen := make(map[string]bool, len(v.Symbols))
	var out []string
	for _, s := range v.Symbols {
		if !seen[s.File] {
			seen[s.File] = true
			out = append(out, s.File)
		}
	}
	sort.Strings(out)
	return out
}

// BlastRadius returns the production symbols that transitively depend on id.
//
// Static edges only. This is the set a probe is graded against, and an
// over-approximated edge here means marking a correct answer wrong.
//
// Individual test functions are excluded, and that exclusion is the difference
// between a fair question and an impossible one. A test helper calling a
// symbol is a real call-graph dependent, but nobody answering "what breaks"
// enumerates test functions by name — asking someone to produce
// "cli.runWithInput" alongside "cli.Main" grades their memory of the test
// suite rather than their model of the code. What actually breaks, from a
// test's point of view, is a whole suite going red, and CoveringTests reports
// that separately at the granularity a person would answer in.
func (v *View) BlastRadius(id core.SymbolID, maxDepth int) map[core.SymbolID]int {
	out := make(map[core.SymbolID]int)
	for dep, dist := range graph.Dependents(v.StaticCalls, string(id), maxDepth) {
		depID := core.SymbolID(dep)
		if sym, ok := v.Symbols[depID]; ok && sym.IsTest() {
			continue
		}
		out[depID] = dist
	}
	return out
}

// CoveringTests returns the test binaries that would go red.
func (v *View) CoveringTests(id core.SymbolID) []core.SymbolID {
	return v.CoveredBy[id]
}
