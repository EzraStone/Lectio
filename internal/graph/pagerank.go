package graph

import "math"

// PageRankOptions tunes the power iteration.
type PageRankOptions struct {
	// Damping is the probability the random surfer follows an edge rather than
	// teleporting. 0.85 is the standard value and there is no reason to be
	// clever here.
	Damping float64
	// MaxIter bounds the iteration count for graphs that converge slowly.
	MaxIter int
	// Tolerance is the L1 change below which iteration stops.
	Tolerance float64
	// Personalization biases the teleport distribution. Nil means uniform.
	// Values are normalized internally; negatives are treated as zero.
	Personalization []float64
}

// DefaultPageRank returns the settings used for centrality.
func DefaultPageRank() PageRankOptions {
	return PageRankOptions{Damping: 0.85, MaxIter: 100, Tolerance: 1e-8}
}

// PageRank computes the stationary distribution of a random surfer over g,
// returning one score per node index. Scores sum to 1.
//
// Two details matter for call graphs specifically:
//
// Dangling nodes are the common case, not an edge case — every leaf function
// that calls nothing is dangling. Their rank is collected each iteration and
// redistributed through the teleport vector, so mass is conserved rather than
// draining into the leaves.
//
// The graph should be deduplicated first. Parallel edges are meaningful
// elsewhere, but here they would let one caller with a six-times-repeated call
// site hand six shares of its rank to a single callee.
func PageRank(g *Graph, opts PageRankOptions) []float64 {
	n := g.N()
	if n == 0 {
		return nil
	}
	if opts.Damping <= 0 || opts.Damping >= 1 {
		opts.Damping = 0.85
	}
	if opts.MaxIter <= 0 {
		opts.MaxIter = 100
	}
	if opts.Tolerance <= 0 {
		opts.Tolerance = 1e-8
	}

	teleport := normalizedTeleport(opts.Personalization, n)

	rank := make([]float64, n)
	next := make([]float64, n)
	copy(rank, teleport)

	// Precompute out-degrees once; the inner loop reads them n times per
	// iteration and len() on a slice header is cheap but the indirection is not.
	outDeg := make([]float64, n)
	for i := 0; i < n; i++ {
		outDeg[i] = float64(len(g.out[i]))
	}

	for iter := 0; iter < opts.MaxIter; iter++ {
		// Rank stranded on dangling nodes, redistributed via teleport.
		var dangling float64
		for i := 0; i < n; i++ {
			if outDeg[i] == 0 {
				dangling += rank[i]
			}
		}

		base := (1 - opts.Damping) + opts.Damping*dangling
		for i := 0; i < n; i++ {
			next[i] = base * teleport[i]
		}

		// Pull formulation: each node sums contributions from its predecessors.
		// Pulling rather than pushing keeps writes local to next[i] and avoids
		// the scattered writes that dominate cache misses on large graphs.
		for i := 0; i < n; i++ {
			var sum float64
			for _, p := range g.in[i] {
				sum += rank[p] / outDeg[p]
			}
			next[i] += opts.Damping * sum
		}

		var delta float64
		for i := 0; i < n; i++ {
			delta += math.Abs(next[i] - rank[i])
		}
		rank, next = next, rank
		if delta < opts.Tolerance {
			break
		}
	}
	return rank
}

// Centrality scores symbols by how much of the codebase ultimately leans on
// them. This is the spec's "reverse PageRank on the call graph".
//
// On the orientation, because it is easy to get backwards: edges run
// caller -> callee, so the surfer walks from a dependent into what it needs,
// and rank accumulates at the far end. A symbol therefore scores high when many
// symbols call it, weighted by how important those callers are themselves.
// High fan-in means you cannot avoid reading it — which is the whole point of
// the signal.
//
// The input graph is deduplicated as a side effect.
func Centrality(callGraph *Graph) []float64 {
	return PageRank(callGraph.Dedup(), DefaultPageRank())
}

// PersonalizedCentrality is Centrality with the teleport distribution
// concentrated on seeds, which scores "important, as seen from here" rather
// than "important globally". Unknown seed IDs are ignored; if none resolve, the
// result falls back to uniform centrality.
func PersonalizedCentrality(callGraph *Graph, seeds []string) []float64 {
	g := callGraph.Dedup()
	opts := DefaultPageRank()

	p := make([]float64, g.N())
	var any bool
	for _, s := range seeds {
		if i, ok := g.Index(s); ok {
			p[i] = 1
			any = true
		}
	}
	if any {
		opts.Personalization = p
	}
	return PageRank(g, opts)
}

// normalizedTeleport turns an optional personalization vector into a
// probability distribution over n nodes.
func normalizedTeleport(p []float64, n int) []float64 {
	out := make([]float64, n)
	if len(p) != n {
		u := 1 / float64(n)
		for i := range out {
			out[i] = u
		}
		return out
	}
	var total float64
	for i, v := range p {
		if v > 0 {
			out[i] = v
			total += v
		}
	}
	if total == 0 {
		u := 1 / float64(n)
		for i := range out {
			out[i] = u
		}
		return out
	}
	for i := range out {
		out[i] /= total
	}
	return out
}
