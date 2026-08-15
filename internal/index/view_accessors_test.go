package index

import (
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

func accessorView() *View {
	return &View{
		Symbols: map[core.SymbolID]core.Symbol{
			"m/p.Third":  {ID: "m/p.Third", File: "b.go", StartLine: 5},
			"m/p.First":  {ID: "m/p.First", File: "a.go", StartLine: 30},
			"m/p.Second": {ID: "m/p.Second", File: "a.go", StartLine: 10},
			"m/p.Zeroth": {ID: "m/p.Zeroth", File: "a.go", StartLine: 3},
		},
		CoveredBy: map[core.SymbolID][]core.SymbolID{
			"m/p.First": {"m/p.TestFirst", "m/p.TestOther"},
		},
	}
}

// Source order, not map order. This feeds the CLI's per-file listings, and a
// list that reshuffles between runs is one nobody trusts.
func TestSymbolsInFileIsInSourceOrder(t *testing.T) {
	got := accessorView().SymbolsInFile("a.go")
	if len(got) != 3 {
		t.Fatalf("got %d symbols for a.go, want 3", len(got))
	}
	want := []core.SymbolID{"m/p.Zeroth", "m/p.Second", "m/p.First"}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("position %d is %s, want %s", i, got[i].ID, id)
		}
	}
}

func TestSymbolsInFileIsEmptyForAnUnknownPath(t *testing.T) {
	if got := accessorView().SymbolsInFile("nope.go"); len(got) != 0 {
		t.Errorf("got %v for a file with no symbols", got)
	}
}

func TestFilesAreDistinctAndSorted(t *testing.T) {
	got := accessorView().Files()
	if len(got) != 2 {
		t.Fatalf("got %v, want two distinct files", got)
	}
	if got[0] != "a.go" || got[1] != "b.go" {
		t.Errorf("got %v, want [a.go b.go] sorted", got)
	}
}

// Determinism across runs, not just within one. Map iteration order is
// randomized per process, so a single call proves nothing.
func TestFilesOrderIsStable(t *testing.T) {
	v := accessorView()
	first := v.Files()
	for i := 0; i < 100; i++ {
		got := v.Files()
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("Files() reordered between calls: %v then %v", first, got)
			}
		}
	}
}

func TestCoveringTests(t *testing.T) {
	v := accessorView()

	got := v.CoveringTests("m/p.First")
	if len(got) != 2 {
		t.Errorf("got %v, want two covering tests", got)
	}
	// A symbol nothing covers is a fact, not a missing entry.
	if got := v.CoveringTests("m/p.Third"); len(got) != 0 {
		t.Errorf("got %v for an uncovered symbol, want none", got)
	}
}

func TestFilesOnAnEmptyView(t *testing.T) {
	v := &View{Symbols: map[core.SymbolID]core.Symbol{}}
	if got := v.Files(); len(got) != 0 {
		t.Errorf("got %v from an empty view", got)
	}
	if got := v.SymbolsInFile("a.go"); len(got) != 0 {
		t.Errorf("got %v from an empty view", got)
	}
}
