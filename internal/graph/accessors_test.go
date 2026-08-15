package graph

import "testing"

func triangle() *Graph {
	g := New(0)
	g.AddEdge("a", "b")
	g.AddEdge("a", "c")
	g.AddEdge("b", "c")
	return g
}

// IDs backs every place a graph is turned into a report, and index order is
// what makes an index meaningful at all.
func TestIDsAreInIndexOrder(t *testing.T) {
	g := triangle()
	ids := g.IDs()

	if len(ids) != g.N() {
		t.Fatalf("IDs returned %d entries for %d nodes", len(ids), g.N())
	}
	for i, id := range ids {
		if got := g.ID(i); got != id {
			t.Errorf("IDs()[%d] = %q but ID(%d) = %q", i, id, i, got)
		}
		if idx, ok := g.Index(id); !ok || idx != i {
			t.Errorf("Index(%q) = %d, %v; want %d", id, idx, ok, i)
		}
	}
}

// Reversing edge orientation silently inverts centrality and blast radius,
// which is why the orientation is documented as load-bearing. Degrees are the
// cheapest place to catch it.
func TestDegreesFollowEdgeOrientation(t *testing.T) {
	g := triangle()

	a, _ := g.Index("a")
	c, _ := g.Index("c")

	if got := g.OutDegree(a); got != 2 {
		t.Errorf("a calls two things, OutDegree = %d", got)
	}
	if got := g.InDegree(a); got != 0 {
		t.Errorf("nothing calls a, InDegree = %d", got)
	}
	if got := g.InDegree(c); got != 2 {
		t.Errorf("two things call c, InDegree = %d", got)
	}
	if got := g.OutDegree(c); got != 0 {
		t.Errorf("c calls nothing, OutDegree = %d", got)
	}
}

func TestHasAndIndexOnAMissingNode(t *testing.T) {
	g := triangle()
	if g.Has("nope") {
		t.Error("Has reported a node that was never added")
	}
	if _, ok := g.Index("nope"); ok {
		t.Error("Index reported a node that was never added")
	}
}

// A symbol with no edges is still a symbol. "Nothing depends on this" is a
// fact worth ranking on, not grounds for erasing it from the graph.
func TestIsolatedNodesKeepTheirIndex(t *testing.T) {
	g := New(0)
	g.Add("lonely")
	g.AddEdge("a", "b")

	if !g.Has("lonely") {
		t.Fatal("an isolated node vanished")
	}
	i, _ := g.Index("lonely")
	if g.OutDegree(i) != 0 || g.InDegree(i) != 0 {
		t.Error("an isolated node has edges")
	}
	if g.ID(i) != "lonely" {
		t.Errorf("ID(%d) = %q", i, g.ID(i))
	}
}
