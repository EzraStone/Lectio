package graph

import (
	"math"
	"testing"
)

func score(t *testing.T, g *Graph, r []float64, id string) float64 {
	t.Helper()
	i, ok := g.Index(id)
	if !ok {
		t.Fatalf("node %q missing", id)
	}
	return r[i]
}

func TestPageRankSumsToOne(t *testing.T) {
	g := build([][2]string{{"a", "b"}, {"b", "c"}, {"c", "a"}, {"a", "c"}, {"d", "a"}})
	r := PageRank(g.Dedup(), DefaultPageRank())

	var total float64
	for _, v := range r {
		total += v
	}
	if math.Abs(total-1) > 1e-6 {
		t.Errorf("ranks sum to %v, want 1", total)
	}
}

// Dangling nodes are the common case in a call graph: every leaf function is
// one. If their mass is not redistributed the distribution silently drains.
func TestPageRankConservesMassWithDanglingNodes(t *testing.T) {
	g := build([][2]string{{"a", "leaf1"}, {"a", "leaf2"}, {"b", "leaf1"}})
	r := PageRank(g.Dedup(), DefaultPageRank())

	var total float64
	for _, v := range r {
		total += v
	}
	if math.Abs(total-1) > 1e-6 {
		t.Errorf("mass leaked: ranks sum to %v, want 1", total)
	}
}

// The signal's whole claim is "high fan-in means you can't avoid it". If the
// orientation is ever flipped, this test is what catches it.
func TestCentralityFavorsHighFanIn(t *testing.T) {
	// Seven callers all depend on parseInterval; main depends on nothing but
	// the callers.
	edges := [][2]string{
		{"main", "scheduler.Next"},
		{"main", "billing.Cycle"},
		{"main", "retry.Backoff"},
		{"scheduler.Next", "parseInterval"},
		{"billing.Cycle", "parseInterval"},
		{"retry.Backoff", "parseInterval"},
		{"cron.Register", "parseInterval"},
		{"digest.Send", "parseInterval"},
		{"invoice.Draft", "parseInterval"},
		{"queue.Requeue", "parseInterval"},
	}
	g := build(edges)
	r := Centrality(g)

	pi := score(t, g, r, "parseInterval")
	for _, other := range []string{"main", "scheduler.Next", "cron.Register", "digest.Send"} {
		if pi <= score(t, g, r, other) {
			t.Errorf("parseInterval (%v) should outrank %s (%v)", pi, other, score(t, g, r, other))
		}
	}
}

func TestPersonalizedCentralityShiftsMassTowardSeeds(t *testing.T) {
	edges := [][2]string{
		{"far.A", "far.B"},
		{"far.B", "far.C"},
		{"near.X", "near.Y"},
		{"near.Y", "near.Z"},
	}
	g := build(edges)

	global := Centrality(build(edges))
	local := PersonalizedCentrality(g, []string{"near.X"})

	gz := score(t, build(edges), global, "near.Z")
	lz := score(t, g, local, "near.Z")
	if lz <= gz {
		t.Errorf("seeding near.X should raise near.Z: global=%v personalized=%v", gz, lz)
	}

	lc := score(t, g, local, "far.C")
	gc := score(t, build(edges), global, "far.C")
	if lc >= gc {
		t.Errorf("seeding near.X should lower far.C: global=%v personalized=%v", gc, lc)
	}
}

func TestPersonalizedCentralityFallsBackOnUnknownSeeds(t *testing.T) {
	g := build([][2]string{{"a", "b"}})
	r := PersonalizedCentrality(g, []string{"nonexistent"})

	var total float64
	for _, v := range r {
		total += v
	}
	if math.Abs(total-1) > 1e-6 {
		t.Errorf("unknown seeds should degrade to uniform, got sum %v", total)
	}
}

func TestPageRankEmptyGraph(t *testing.T) {
	if r := PageRank(New(0), DefaultPageRank()); r != nil {
		t.Errorf("PageRank on an empty graph = %v, want nil", r)
	}
}

// Parallel edges must not buy extra rank; that is what Dedup is for.
func TestCentralityIgnoresCallSiteMultiplicity(t *testing.T) {
	once := Centrality(build([][2]string{{"a", "x"}, {"b", "y"}}))
	many := Centrality(build([][2]string{{"a", "x"}, {"a", "x"}, {"a", "x"}, {"b", "y"}}))

	g := build([][2]string{{"a", "x"}, {"b", "y"}})
	if math.Abs(score(t, g, once, "x")-score(t, g, many, "x")) > 1e-9 {
		t.Errorf("repeated call sites changed centrality: %v vs %v",
			score(t, g, once, "x"), score(t, g, many, "x"))
	}
}
