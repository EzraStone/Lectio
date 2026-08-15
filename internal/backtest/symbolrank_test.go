package backtest

import (
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
)

// staticFiles is a Baseline with a fixed answer, so the lifting logic can be
// tested without building an index.
type staticFiles struct {
	name  string
	order []string
}

func (s staticFiles) Name() string                                { return s.name }
func (s staticFiles) RankFiles(*index.View, rank.Params) []string { return s.order }

func symView(syms ...core.Symbol) *index.View {
	v := &index.View{Symbols: map[core.SymbolID]core.Symbol{}}
	for _, s := range syms {
		v.Symbols[s.ID] = s
	}
	return v
}

// The faithful translation of "read this file" is "read the declarations in
// it, in the order you would meet them" — not "read its longest declaration".
func TestFileBaselineLiftsToSymbolsInSourceOrder(t *testing.T) {
	v := symView(
		core.Symbol{ID: "p.Third", Package: "p", File: "a.go", StartLine: 30, EndLine: 31},
		core.Symbol{ID: "p.First", Package: "p", File: "a.go", StartLine: 10, EndLine: 25},
		core.Symbol{ID: "p.Second", Package: "p", File: "a.go", StartLine: 27, EndLine: 28},
		core.Symbol{ID: "p.Elsewhere", Package: "p", File: "b.go", StartLine: 5, EndLine: 6},
	)
	lifted := FromFileBaseline{Baseline: staticFiles{"fake", []string{"a.go", "b.go"}}}

	got := lifted.RankSymbols(v, rank.DefaultParams())
	want := []core.SymbolID{"p.First", "p.Second", "p.Third", "p.Elsewhere"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestLiftedBaselineKeepsItsName(t *testing.T) {
	for _, b := range SymbolBaselines() {
		if b.Name() == "" {
			t.Error("a lifted baseline lost its name")
		}
	}
	names := map[string]bool{}
	for _, b := range SymbolBaselines() {
		names[b.Name()] = true
	}
	for _, b := range Baselines() {
		if !names[b.Name()] {
			t.Errorf("baseline %q has no symbol-level counterpart", b.Name())
		}
	}
}

// If the longest declarations predict as well as seven signals, size followed
// the measure down from files and changing granularity bought nothing.
func TestLargestSymbolsRanksBySpan(t *testing.T) {
	v := symView(
		core.Symbol{ID: "p.Small", Package: "p", File: "a.go", StartLine: 1, EndLine: 2},
		core.Symbol{ID: "p.Huge", Package: "p", File: "a.go", StartLine: 10, EndLine: 200},
		core.Symbol{ID: "p.Medium", Package: "p", File: "a.go", StartLine: 210, EndLine: 240},
	)
	got := LargestSymbols{}.RankSymbols(v, rank.DefaultParams())
	if len(got) != 3 || got[0] != "p.Huge" || got[1] != "p.Medium" || got[2] != "p.Small" {
		t.Errorf("got %v, want [p.Huge p.Medium p.Small]", got)
	}
}

// Controls explain a result and must never decide one, at either granularity.
func TestSymbolControlsAreNotBaselines(t *testing.T) {
	base := map[string]bool{}
	for _, b := range SymbolBaselines() {
		base[b.Name()] = true
	}
	var sawLargestSymbols bool
	for _, c := range SymbolControls() {
		if base[c.Name()] {
			t.Errorf("%q is both a symbol baseline and a symbol control", c.Name())
		}
		if c.Name() == "largest symbols" {
			sawLargestSymbols = true
		}
	}
	if !sawLargestSymbols {
		t.Error("the largest-symbols control is missing")
	}
}

func TestLectioSymbolsNamesItsVariant(t *testing.T) {
	if got := (LectioSymbols{}).Name(); got != DefaultVariant {
		t.Errorf("unlabelled variant = %q, want %q", got, DefaultVariant)
	}
	if got := (LectioSymbols{Label: "lectio −churn"}).Name(); got != "lectio −churn" {
		t.Errorf("labelled variant = %q", got)
	}
}
