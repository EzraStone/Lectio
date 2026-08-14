package golang

import (
	"context"
	"testing"

	"github.com/EzraStone/Lectio/internal/adapter"
	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/graph"
)

func loadEdges(t *testing.T, opts adapter.Options) ([]core.CallEdge, []core.ImportEdge) {
	t.Helper()
	a := New()
	a.Configure(opts)
	edges, imports, err := a.CallEdges(context.Background(), sampleRoot(t))
	if err != nil {
		t.Fatalf("CallEdges: %v", err)
	}
	return edges, imports
}

func hasEdge(edges []core.CallEdge, from, to core.SymbolID, kind core.EdgeKind) bool {
	for _, e := range edges {
		if e.Caller == from && e.Callee == to && e.Kind == kind {
			return true
		}
	}
	return false
}

func TestStaticEdgesAreResolvedExactly(t *testing.T) {
	edges, _ := loadEdges(t, adapter.DefaultOptions())

	want := [][2]core.SymbolID{
		{"example.com/sample.Parse", "example.com/sample.parseInterval"},
		{"example.com/sample/scheduler.(*Scheduler).Next", "example.com/sample.Parse"},
		{"example.com/sample/billing.Cycle", "example.com/sample.Parse"},
		{"example.com/sample/billing.Draft", "example.com/sample/billing.Cycle"},
		{"example.com/sample/retry.Backoff", "example.com/sample.Parse"},
		{"example.com/sample/retry.Requeue", "example.com/sample/retry.Backoff"},
		{"example.com/sample/retry.Totals", "example.com/sample/retry.countAll"},
	}
	for _, w := range want {
		if !hasEdge(edges, w[0], w[1], core.EdgeStatic) {
			t.Errorf("missing static edge %s -> %s", w[0], w[1])
		}
	}
}

// Edges into the standard library are dropped: a reading path should never
// send someone to strconv.Atoi, and stdlib nodes would swamp centrality.
func TestEdgesStayInsideTheRepo(t *testing.T) {
	edges, _ := loadEdges(t, adapter.DefaultOptions())
	for _, e := range edges {
		for _, id := range []core.SymbolID{e.Caller, e.Callee} {
			if pkg := id.Package(); pkg == "time" || pkg == "strings" || pkg == "strconv" || pkg == "errors" {
				t.Errorf("edge escaped the repo: %s -> %s", e.Caller, e.Callee)
			}
		}
	}
}

// The trust property: an edge CHA guessed at must never be labelled static.
func TestInterfaceDispatchIsMarkedDynamic(t *testing.T) {
	edges, _ := loadEdges(t, adapter.DefaultOptions())

	const next = core.SymbolID("example.com/sample/scheduler.(*Scheduler).Next")
	const realNow = core.SymbolID("example.com/sample/scheduler.(realClock).Now")
	const fixedNow = core.SymbolID("example.com/sample/scheduler.(fixedClock).Now")

	if !hasEdge(edges, next, realNow, core.EdgeDynamic) {
		t.Error("CHA should resolve the Clock.Now() call to realClock.Now")
	}
	// CHA is sound and imprecise: it reaches both implementations even though
	// only one is installed on this Scheduler. That is expected, and exactly
	// why these edges never grade anything.
	if !hasEdge(edges, next, fixedNow, core.EdgeDynamic) {
		t.Error("CHA should also reach fixedClock.Now; over-approximation is the contract")
	}
	if hasEdge(edges, next, realNow, core.EdgeStatic) {
		t.Error("an interface call was labelled static; grading would then punish a correct answer")
	}
}

func TestResolveDynamicCanBeDisabled(t *testing.T) {
	opts := adapter.DefaultOptions()
	opts.ResolveDynamic = false
	edges, _ := loadEdges(t, opts)

	for _, e := range edges {
		if e.Kind == core.EdgeDynamic {
			t.Fatalf("dynamic edge present with resolution disabled: %s -> %s", e.Caller, e.Callee)
		}
	}
	// The static graph must still be there.
	if !hasEdge(edges, "example.com/sample.Parse", "example.com/sample.parseInterval", core.EdgeStatic) {
		t.Error("disabling dynamic resolution lost the static edges too")
	}
}

// The end-to-end claim from the spec's figure: you are changing parseInterval,
// what breaks?
func TestBlastRadiusOfParseInterval(t *testing.T) {
	edges, _ := loadEdges(t, adapter.DefaultOptions())

	g := graph.New(0)
	for _, e := range edges {
		if e.Kind == core.EdgeStatic {
			g.AddEdge(string(e.Caller), string(e.Callee))
		}
	}
	got := graph.Dependents(g, "example.com/sample.parseInterval", 0)

	for _, want := range []string{
		"example.com/sample.Parse",
		"example.com/sample/scheduler.(*Scheduler).Next",
		"example.com/sample/billing.Cycle",
		"example.com/sample/billing.Draft",
		"example.com/sample/retry.Backoff",
		"example.com/sample/retry.Requeue",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("blast radius missing %s; got %v", want, got)
		}
	}
	if got["example.com/sample.Parse"] != 1 {
		t.Errorf("Parse should be one hop from parseInterval, got %d", got["example.com/sample.Parse"])
	}
	if got["example.com/sample/billing.Draft"] != 3 {
		t.Errorf("Draft should be three hops out, got %d", got["example.com/sample/billing.Draft"])
	}
}

func TestGenericCallsCollapseToOrigin(t *testing.T) {
	edges, _ := loadEdges(t, adapter.DefaultOptions())

	var n int
	for _, e := range edges {
		if e.Caller == "example.com/sample/retry.Totals" && e.Callee == "example.com/sample/retry.countAll" {
			n++
		}
	}
	// Two instantiations, two call sites, one callee symbol.
	if n != 2 {
		t.Errorf("countAll call sites = %d, want 2 (both resolving to the same origin symbol)", n)
	}
}

func TestImportEdgesStayInsideTheRepo(t *testing.T) {
	_, imports := loadEdges(t, adapter.DefaultOptions())

	want := map[core.ImportEdge]bool{
		{From: "example.com/sample/scheduler", To: "example.com/sample"}: false,
		{From: "example.com/sample/billing", To: "example.com/sample"}:   false,
		{From: "example.com/sample/retry", To: "example.com/sample"}:     false,
	}
	for _, e := range imports {
		if e.To == "time" || e.To == "strings" {
			t.Errorf("import edge to the standard library: %+v", e)
		}
		if _, ok := want[e]; ok {
			want[e] = true
		}
	}
	for e, found := range want {
		if !found {
			t.Errorf("missing import edge %+v", e)
		}
	}
}

func TestCallSitesCarryPositions(t *testing.T) {
	edges, _ := loadEdges(t, adapter.DefaultOptions())
	for _, e := range edges {
		if e.Kind != core.EdgeStatic {
			continue
		}
		if e.File == "" || e.Line == 0 {
			t.Errorf("static edge %s -> %s has no call site position", e.Caller, e.Callee)
		}
	}
}
