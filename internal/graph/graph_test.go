package graph

import (
	"reflect"
	"sort"
	"testing"
)

func build(edges [][2]string) *Graph {
	g := New(0)
	for _, e := range edges {
		g.AddEdge(e[0], e[1])
	}
	return g
}

func idsOf(g *Graph, idx []int) []string {
	out := make([]string, 0, len(idx))
	for _, i := range idx {
		out = append(out, g.ID(i))
	}
	sort.Strings(out)
	return out
}

func TestAddEdgeMaintainsBothDirections(t *testing.T) {
	g := build([][2]string{{"a", "b"}, {"c", "b"}})

	b, ok := g.Index("b")
	if !ok {
		t.Fatal("b missing")
	}
	if got, want := idsOf(g, g.In(b)), []string{"a", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("In(b) = %v, want %v", got, want)
	}
	if g.OutDegree(b) != 0 {
		t.Errorf("OutDegree(b) = %d, want 0", g.OutDegree(b))
	}
	if g.N() != 3 || g.Edges() != 2 {
		t.Errorf("N=%d Edges=%d, want 3 and 2", g.N(), g.Edges())
	}
}

func TestParallelEdgesKeptUntilDedup(t *testing.T) {
	g := build([][2]string{{"a", "b"}, {"a", "b"}, {"a", "b"}})
	if g.Edges() != 3 {
		t.Errorf("Edges() = %d, want 3 before Dedup", g.Edges())
	}
	g.Dedup()
	if g.Edges() != 1 {
		t.Errorf("Edges() = %d, want 1 after Dedup", g.Edges())
	}
	a, _ := g.Index("a")
	if got := len(g.Out(a)); got != 1 {
		t.Errorf("Out(a) has %d entries after Dedup, want 1", got)
	}
}

func TestReverseFlipsEveryEdge(t *testing.T) {
	g := build([][2]string{{"a", "b"}, {"b", "c"}})
	r := g.Reverse()

	b, _ := r.Index("b")
	if got, want := idsOf(r, r.Out(b)), []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("reversed Out(b) = %v, want %v", got, want)
	}
	if got, want := idsOf(r, r.In(b)), []string{"c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("reversed In(b) = %v, want %v", got, want)
	}
	// Reversing must not disturb the original.
	ob, _ := g.Index("b")
	if got, want := idsOf(g, g.Out(ob)), []string{"c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("original mutated: Out(b) = %v, want %v", got, want)
	}
}

func TestSubgraphInduces(t *testing.T) {
	g := build([][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}})
	sub := g.Subgraph(func(id string) bool { return id != "c" })

	if sub.Has("c") {
		t.Error("dropped node still present")
	}
	if sub.N() != 3 {
		t.Errorf("N() = %d, want 3", sub.N())
	}
	// b -> c and c -> d both vanish; only a -> b survives.
	if sub.Edges() != 1 {
		t.Errorf("Edges() = %d, want 1", sub.Edges())
	}
}

func TestAddIsIdempotent(t *testing.T) {
	g := New(0)
	if g.Add("x") != g.Add("x") {
		t.Error("Add returned different indices for the same id")
	}
	if g.N() != 1 {
		t.Errorf("N() = %d, want 1", g.N())
	}
}
