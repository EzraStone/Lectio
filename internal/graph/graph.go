// Package graph implements the directed-graph primitives the ranker and the
// probe grader run on: centrality, reachability, and dependency-depth ordering.
//
// Nodes are dense integer indices internally and opaque string IDs at the API
// boundary. Every algorithm here runs on repos with hundreds of thousands of
// edges, so the hot paths avoid maps entirely.
package graph

import "sort"

// Graph is a directed graph with both forward and reverse adjacency
// maintained. Both directions are needed constantly — forward for "what does
// this depend on", reverse for "what depends on this" — and building the
// reverse view lazily turned out to be the thing that dominated indexing time.
type Graph struct {
	ids   []string
	index map[string]int
	out   [][]int
	in    [][]int
	edges int
}

// New returns an empty graph, pre-sized for hint nodes.
func New(hint int) *Graph {
	if hint < 0 {
		hint = 0
	}
	return &Graph{
		ids:   make([]string, 0, hint),
		index: make(map[string]int, hint),
		out:   make([][]int, 0, hint),
		in:    make([][]int, 0, hint),
	}
}

// Add returns the index for id, creating the node if it is new.
func (g *Graph) Add(id string) int {
	if i, ok := g.index[id]; ok {
		return i
	}
	i := len(g.ids)
	g.ids = append(g.ids, id)
	g.index[id] = i
	g.out = append(g.out, nil)
	g.in = append(g.in, nil)
	return i
}

// AddEdge records from -> to, creating either endpoint as needed.
//
// Duplicate edges are kept. Call-graph extraction emits one edge per call site,
// and how many times A calls B is real signal — a function called from six
// places in one caller is more entangled than one called once. Deduplicate at
// the point of use if a particular algorithm needs a simple graph.
func (g *Graph) AddEdge(from, to string) {
	f, t := g.Add(from), g.Add(to)
	g.out[f] = append(g.out[f], t)
	g.in[t] = append(g.in[t], f)
	g.edges++
}

// The read-only accessors below tolerate a nil receiver, and that is a
// deliberate choice rather than defensive habit.
//
// index.View is a plain struct with exported fields, built by index.Load in
// production and by hand everywhere else — tests, the backtest's fixtures, any
// caller assembling a partial view to exercise one signal. A View whose Calls
// is nil is a perfectly ordinary thing to construct, and rank.Rank walking
// into it should report an empty graph rather than crash the process. A nil
// graph has no nodes; saying so is more useful than a stack trace.
//
// The mutating methods do not do this. Adding to a nil graph is a programming
// error with no sensible answer, and silently discarding the edge would be
// worse than the panic.

// N returns the node count.
func (g *Graph) N() int {
	if g == nil {
		return 0
	}
	return len(g.ids)
}

// Edges returns the edge count, including duplicates.
func (g *Graph) Edges() int {
	if g == nil {
		return 0
	}
	return g.edges
}

// ID returns the string identifier for a node index.
func (g *Graph) ID(i int) string { return g.ids[i] }

// IDs returns every node identifier, in index order.
func (g *Graph) IDs() []string {
	if g == nil {
		return nil
	}
	return g.ids
}

// Index returns the index for id, and whether it exists.
func (g *Graph) Index(id string) (int, bool) {
	if g == nil {
		return 0, false
	}
	i, ok := g.index[id]
	return i, ok
}

// Has reports whether id is present.
func (g *Graph) Has(id string) bool {
	if g == nil {
		return false
	}
	_, ok := g.index[id]
	return ok
}

// Out returns the successors of i: the nodes i depends on.
func (g *Graph) Out(i int) []int { return g.out[i] }

// In returns the predecessors of i: the nodes that depend on i.
func (g *Graph) In(i int) []int { return g.in[i] }

// OutDegree and InDegree count edges including duplicates.
func (g *Graph) OutDegree(i int) int { return len(g.out[i]) }
func (g *Graph) InDegree(i int) int  { return len(g.in[i]) }

// Dedup collapses parallel edges in place and returns g, so a caller can write
// g := graph.New(0).Dedup() style pipelines. Algorithms that model probability
// mass — PageRank especially — need a simple graph or high-multiplicity edges
// dominate the result.
func (g *Graph) Dedup() *Graph {
	for i := range g.out {
		g.out[i] = uniq(g.out[i])
		g.in[i] = uniq(g.in[i])
	}
	g.edges = 0
	for i := range g.out {
		g.edges += len(g.out[i])
	}
	return g
}

// Reverse returns a new graph with every edge flipped. The reversed call graph
// is the blast-radius graph: successors become "who breaks if I change this".
func (g *Graph) Reverse() *Graph {
	r := &Graph{
		ids:   append([]string(nil), g.ids...),
		index: make(map[string]int, len(g.ids)),
		out:   make([][]int, len(g.ids)),
		in:    make([][]int, len(g.ids)),
		edges: g.edges,
	}
	for id, i := range g.index {
		r.index[id] = i
	}
	for i := range g.ids {
		r.out[i] = append([]int(nil), g.in[i]...)
		r.in[i] = append([]int(nil), g.out[i]...)
	}
	return r
}

// Subgraph returns the induced subgraph over keep, with fresh indices.
func (g *Graph) Subgraph(keep func(id string) bool) *Graph {
	sub := New(g.N())
	for i, id := range g.ids {
		if !keep(id) {
			continue
		}
		sub.Add(id)
		for _, j := range g.out[i] {
			if keep(g.ids[j]) {
				sub.AddEdge(id, g.ids[j])
			}
		}
	}
	return sub
}

func uniq(xs []int) []int {
	if len(xs) < 2 {
		return xs
	}
	sort.Ints(xs)
	out := xs[:1]
	for _, x := range xs[1:] {
		if x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}
