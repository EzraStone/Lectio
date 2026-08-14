package graph

// Reachable performs a breadth-first walk from seeds along forward edges,
// returning node index -> hop distance. Seeds are at distance 0 and are
// included in the result.
//
// maxDepth <= 0 means unbounded. Bounding matters in practice: in a
// well-connected service the transitive dependents of a core utility are
// most of the repo, and "everything breaks" is not an answer anyone can use.
func Reachable(g *Graph, seeds []int, maxDepth int) map[int]int {
	dist := make(map[int]int, len(seeds)*8)
	frontier := make([]int, 0, len(seeds))
	for _, s := range seeds {
		if s < 0 || s >= g.N() {
			continue
		}
		if _, seen := dist[s]; !seen {
			dist[s] = 0
			frontier = append(frontier, s)
		}
	}

	for d := 1; len(frontier) > 0; d++ {
		if maxDepth > 0 && d > maxDepth {
			break
		}
		next := frontier[:0:0]
		for _, v := range frontier {
			for _, w := range g.out[v] {
				if _, seen := dist[w]; seen {
					continue
				}
				dist[w] = d
				next = append(next, w)
			}
		}
		frontier = next
	}
	return dist
}

// Dependents returns everything that transitively depends on id, mapped to hop
// distance. This is the blast-radius question: you are changing id, what
// breaks?
//
// The seed itself is excluded — "parseInterval breaks if you change
// parseInterval" is not a useful element of the answer set, and including it
// would inflate the F1 score of anyone who names the symbol back at us.
func Dependents(g *Graph, id string, maxDepth int) map[string]int {
	return walkFrom(g.Reverse(), id, maxDepth)
}

// Dependencies returns everything id transitively depends on. This is the
// reading-prerequisite question: what do I have to understand first?
func Dependencies(g *Graph, id string, maxDepth int) map[string]int {
	return walkFrom(g, id, maxDepth)
}

func walkFrom(g *Graph, id string, maxDepth int) map[string]int {
	seed, ok := g.Index(id)
	if !ok {
		return map[string]int{}
	}
	dist := Reachable(g, []int{seed}, maxDepth)
	out := make(map[string]int, len(dist))
	for i, d := range dist {
		if i == seed {
			continue
		}
		out[g.ID(i)] = d
	}
	return out
}

// Proximity returns the undirected hop distance from the nearest seed to every
// node, or -1 where no seed reaches it.
//
// Direction is deliberately ignored here, unlike everywhere else in this
// package. When someone says "I am working on billing", both what billing calls
// and what calls billing are nearby in the sense that matters — the scope of a
// task is a neighborhood, not a cone.
func Proximity(g *Graph, seeds []string) []int {
	n := g.N()
	dist := make([]int, n)
	for i := range dist {
		dist[i] = -1
	}

	frontier := make([]int, 0, len(seeds))
	for _, s := range seeds {
		if i, ok := g.Index(s); ok && dist[i] != 0 {
			dist[i] = 0
			frontier = append(frontier, i)
		}
	}

	for d := 1; len(frontier) > 0; d++ {
		next := frontier[:0:0]
		for _, v := range frontier {
			for _, w := range g.out[v] {
				if dist[w] == -1 {
					dist[w] = d
					next = append(next, w)
				}
			}
			for _, w := range g.in[v] {
				if dist[w] == -1 {
					dist[w] = d
					next = append(next, w)
				}
			}
		}
		frontier = next
	}
	return dist
}
