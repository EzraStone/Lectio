package rank

import (
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

func item(id core.SymbolID, score float64, depth int) Item {
	return Item{Symbol: core.Symbol{ID: id}, Score: score, Depth: depth}
}

// The spec's claim: you cannot understand a handler before its core types, so
// the top-scoring item is frequently not the first thing to read.
func TestSequenceOrdersDependenciesFirstWithinATier(t *testing.T) {
	got := Sequence([]Item{
		item("handler", 1.00, 3),
		item("service", 0.95, 2),
		item("types", 0.90, 0),
	})

	want := []core.SymbolID{"types", "service", "handler"}
	for i, w := range want {
		if got[i].Symbol.ID != w {
			t.Errorf("position %d = %s, want %s (full order: %v)", i, got[i].Symbol.ID, w, ids(got))
		}
	}
}

// Depth has authority inside a tier and none across tiers: a barely-relevant
// leaf must not lead the path just because it depends on nothing.
func TestSequenceDoesNotLetDepthOutrankRelevance(t *testing.T) {
	got := Sequence([]Item{
		item("central", 1.00, 5),
		item("irrelevant-leaf", 0.10, 0),
	})

	if got[0].Symbol.ID != "central" {
		t.Errorf("first item = %s, want central; a low-relevance leaf jumped tiers", got[0].Symbol.ID)
	}
	if got[0].Tier == got[1].Tier {
		t.Errorf("scores 1.00 and 0.10 landed in the same tier (%d)", got[0].Tier)
	}
}

func TestTiersAreRelativeToTheTopScore(t *testing.T) {
	got := Sequence([]Item{
		item("a", 1.00, 0),
		item("b", 0.75, 0),
		item("c", 0.50, 0),
		item("d", 0.10, 0),
	})

	tiers := map[core.SymbolID]int{}
	for _, it := range got {
		tiers[it.Symbol.ID] = it.Tier
	}
	if tiers["a"] != 1 || tiers["b"] != 1 {
		t.Errorf("top of the list = tiers %d and %d, want 1", tiers["a"], tiers["b"])
	}
	if tiers["c"] != 2 {
		t.Errorf("half the top score = tier %d, want 2", tiers["c"])
	}
	if tiers["d"] != 3 {
		t.Errorf("a tenth the top score = tier %d, want 3", tiers["d"])
	}
}

// Scores are a composite whose absolute scale depends on how many signals
// fired, so a repo where fewer fire must still produce the same shape.
func TestTiersSurviveADifferentAbsoluteScale(t *testing.T) {
	high := Sequence([]Item{item("a", 0.90, 0), item("b", 0.63, 0), item("c", 0.20, 0)})
	low := Sequence([]Item{item("a", 0.09, 0), item("b", 0.063, 0), item("c", 0.02, 0)})

	for i := range high {
		if high[i].Tier != low[i].Tier {
			t.Errorf("position %d: tier %d at high scale, %d at low", i, high[i].Tier, low[i].Tier)
		}
	}
}

func TestSequenceIsStableAndTotal(t *testing.T) {
	in := []Item{
		item("a", 0.5, 1), item("b", 0.5, 1), item("c", 0.5, 1),
	}
	first := ids(Sequence(in))
	for i := 0; i < 10; i++ {
		got := ids(Sequence(in))
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d differs at %d: %v vs %v", i, j, got, first)
			}
		}
	}
	if len(first) != 3 {
		t.Errorf("Sequence dropped items: %v", first)
	}
}

func TestSequenceEmpty(t *testing.T) {
	if got := Sequence(nil); got != nil {
		t.Errorf("Sequence(nil) = %v, want nil", got)
	}
}

func TestSequenceAllZeroScores(t *testing.T) {
	got := Sequence([]Item{item("a", 0, 0), item("b", 0, 1)})
	for _, it := range got {
		if it.Tier != TierCount {
			t.Errorf("%s landed in tier %d; nothing scored, so nothing is a priority", it.Symbol.ID, it.Tier)
		}
	}
}

func TestTierLabels(t *testing.T) {
	for _, tier := range []int{1, 2, 3} {
		if TierLabel(tier) == "" {
			t.Errorf("tier %d has no label", tier)
		}
	}
}

func ids(items []Item) []core.SymbolID {
	out := make([]core.SymbolID, len(items))
	for i, it := range items {
		out[i] = it.Symbol.ID
	}
	return out
}
