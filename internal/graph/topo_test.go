package graph

import (
	"reflect"
	"testing"
)

func TestSCCFindsComponents(t *testing.T) {
	// a <-> b is a cycle; c and d hang off it in a line.
	g := build([][2]string{{"a", "b"}, {"b", "a"}, {"b", "c"}, {"c", "d"}})
	comp, count := SCC(g)

	if count != 3 {
		t.Fatalf("component count = %d, want 3", count)
	}
	ai, _ := g.Index("a")
	bi, _ := g.Index("b")
	ci, _ := g.Index("c")
	if comp[ai] != comp[bi] {
		t.Error("a and b form a cycle and belong to one component")
	}
	if comp[ci] == comp[ai] {
		t.Error("c is not part of the a<->b cycle")
	}
	// Sink-first numbering: everything a component points into is lower.
	if !(comp[ci] < comp[ai]) {
		t.Errorf("components must be numbered sink-first: comp(c)=%d comp(a)=%d", comp[ci], comp[ai])
	}
}

func TestSCCSelfLoop(t *testing.T) {
	g := build([][2]string{{"rec", "rec"}})
	comp, count := SCC(g)
	if count != 1 || comp[0] != 0 {
		t.Errorf("self-loop: comp=%v count=%d, want one component", comp, count)
	}
}

func TestDependencyDepthOnAChain(t *testing.T) {
	// handler -> service -> store -> types
	g := build([][2]string{{"handler", "service"}, {"service", "store"}, {"store", "types"}})
	depth := DepthByID(g)

	want := map[string]int{"types": 0, "store": 1, "service": 2, "handler": 3}
	if !reflect.DeepEqual(depth, want) {
		t.Errorf("depths = %v, want %v", depth, want)
	}
}

func TestDependencyDepthTakesLongestChain(t *testing.T) {
	// top has a short path and a long path; the long one decides.
	g := build([][2]string{
		{"top", "short"},
		{"top", "mid"},
		{"mid", "low"},
		{"low", "base"},
	})
	depth := DepthByID(g)
	if depth["top"] != 3 {
		t.Errorf("depth(top) = %d, want 3 (longest chain, not shortest)", depth["top"])
	}
}

func TestDependencyDepthCollapsesCycles(t *testing.T) {
	g := build([][2]string{{"a", "b"}, {"b", "a"}, {"a", "leaf"}})
	depth := DepthByID(g)

	if depth["a"] != depth["b"] {
		t.Errorf("mutually recursive symbols must share a depth: a=%d b=%d", depth["a"], depth["b"])
	}
	if depth["leaf"] != 0 {
		t.Errorf("depth(leaf) = %d, want 0", depth["leaf"])
	}
	if depth["a"] != 1 {
		t.Errorf("depth(a) = %d, want 1", depth["a"])
	}
}

func TestTopoOrderPutsDependenciesFirst(t *testing.T) {
	g := build([][2]string{{"handler", "service"}, {"service", "store"}, {"store", "types"}})
	got := TopoOrder(g)
	want := []string{"types", "store", "service", "handler"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TopoOrder = %v, want %v", got, want)
	}
}

// A reading path that reshuffles between runs is not a path anyone trusts.
func TestTopoOrderIsDeterministic(t *testing.T) {
	edges := [][2]string{{"z", "m"}, {"a", "m"}, {"m", "core"}, {"q", "core"}}
	first := TopoOrder(build(edges))
	for i := 0; i < 5; i++ {
		if got := TopoOrder(build(edges)); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed: %v vs %v", i, got, first)
		}
	}
	// Depths are core=0, {m,q}=1, {a,z}=2; ties inside a depth break on id.
	if !reflect.DeepEqual(first, []string{"core", "m", "q", "a", "z"}) {
		t.Errorf("TopoOrder = %v, want core,m,q,a,z", first)
	}
}

// Deeply nested chains must not blow the stack; the iterative Tarjan exists
// for exactly this.
func TestSCCDeepChain(t *testing.T) {
	g := New(0)
	const n = 50000
	prev := "n0"
	g.Add(prev)
	for i := 1; i < n; i++ {
		cur := "n" + itoa(i)
		g.AddEdge(prev, cur)
		prev = cur
	}
	_, count := SCC(g)
	if count != n {
		t.Errorf("component count = %d, want %d", count, n)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
