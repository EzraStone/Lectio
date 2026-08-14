package graph

import "sort"

// SCC computes strongly connected components with Tarjan's algorithm,
// returning a component id per node index and the component count.
//
// Components are numbered in reverse topological order of the condensation:
// if there is an edge from component A to component B, then B < A. Callers
// downstream depend on that property to compute depths in a single pass.
//
// The implementation is iterative rather than recursive. Real call graphs run
// several thousand frames deep through initialization chains, and a goroutine
// stack is not the place to find that out.
func SCC(g *Graph) (comp []int, count int) {
	n := g.N()
	comp = make([]int, n)
	idx := make([]int, n)
	low := make([]int, n)
	onStack := make([]bool, n)
	for i := range idx {
		idx[i] = -1
		comp[i] = -1
	}

	var stack []int
	var counter int

	type frame struct{ v, next int }
	var call []frame

	for root := 0; root < n; root++ {
		if idx[root] != -1 {
			continue
		}
		call = append(call, frame{v: root})

		for len(call) > 0 {
			f := &call[len(call)-1]
			v := f.v

			if f.next == 0 && idx[v] == -1 {
				idx[v] = counter
				low[v] = counter
				counter++
				stack = append(stack, v)
				onStack[v] = true
			}

			descended := false
			for f.next < len(g.out[v]) {
				w := g.out[v][f.next]
				f.next++
				if idx[w] == -1 {
					call = append(call, frame{v: w})
					descended = true
					break
				}
				if onStack[w] && idx[w] < low[v] {
					low[v] = idx[w]
				}
			}
			if descended {
				continue
			}

			if low[v] == idx[v] {
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					comp[w] = count
					if w == v {
						break
					}
				}
				count++
			}

			call = call[:len(call)-1]
			if len(call) > 0 {
				if p := call[len(call)-1].v; low[v] < low[p] {
					low[p] = low[v]
				}
			}
		}
	}
	return comp, count
}

// DependencyDepth returns, per node, the length of the longest chain of
// dependencies beneath it. A symbol that depends on nothing indexed has depth
// 0; one whose deepest dependency chain is three calls long has depth 3.
//
// Cycles are collapsed to their strongly connected component and share a
// depth. Mutual recursion is normal in real code and there is no honest answer
// to "which of these two comes first" — so the ordering treats them as one
// unit rather than picking arbitrarily and pretending.
func DependencyDepth(g *Graph) []int {
	comp, count := SCC(g)
	n := g.N()

	members := make([][]int, count)
	for v, c := range comp {
		members[c] = append(members[c], v)
	}

	// Components are emitted sink-first, so every component a node points into
	// is already resolved by the time we reach it. One pass, no second sort.
	compDepth := make([]int, count)
	for c := 0; c < count; c++ {
		d := 0
		for _, v := range members[c] {
			for _, w := range g.out[v] {
				if comp[w] == c {
					continue
				}
				if compDepth[comp[w]]+1 > d {
					d = compDepth[comp[w]] + 1
				}
			}
		}
		compDepth[c] = d
	}

	depth := make([]int, n)
	for v, c := range comp {
		depth[v] = compDepth[c]
	}
	return depth
}

// TopoOrder returns node ids in dependency-first order: everything a symbol
// needs appears before the symbol itself.
//
// This is the spec's "ordering is not ranking". You cannot understand a handler
// before its core types, so once relevance has picked what to read, depth picks
// the sequence. Ties break on id so the same repo always produces the same
// reading path — a path that reshuffles between runs is not a path anyone
// trusts.
func TopoOrder(g *Graph) []string {
	depth := DependencyDepth(g)
	order := make([]int, g.N())
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		if depth[ia] != depth[ib] {
			return depth[ia] < depth[ib]
		}
		return g.ID(ia) < g.ID(ib)
	})

	out := make([]string, len(order))
	for i, v := range order {
		out[i] = g.ID(v)
	}
	return out
}

// DepthByID is DependencyDepth keyed by symbol id, for callers that never see
// node indices.
func DepthByID(g *Graph) map[string]int {
	depth := DependencyDepth(g)
	out := make(map[string]int, len(depth))
	for i, d := range depth {
		out[g.ID(i)] = d
	}
	return out
}
