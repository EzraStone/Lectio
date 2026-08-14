package rank

import (
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

func taskView() *builder {
	b := newView()
	b.v.Symbols["mod/billing.Cycle"] = core.Symbol{
		ID: "mod/billing.Cycle", Name: "Cycle", File: "internal/billing/cycle.go", Package: "mod/billing"}
	b.v.Symbols["mod/billing.Draft"] = core.Symbol{
		ID: "mod/billing.Draft", Name: "Draft", File: "internal/billing/draft.go", Package: "mod/billing"}
	b.v.Symbols["mod/core.Parse"] = core.Symbol{
		ID: "mod/core.Parse", Name: "Parse", File: "internal/core/parse.go", Package: "mod/core"}
	b.v.Symbols["mod/ui.Render"] = core.Symbol{
		ID: "mod/ui.Render", Name: "Render", File: "internal/ui/render.go", Package: "mod/ui"}
	b.v.Symbols["mod/ui.Theme"] = core.Symbol{
		ID: "mod/ui.Theme", Name: "Theme", File: "internal/ui/theme.go", Package: "mod/ui"}
	for id := range b.v.Symbols {
		b.v.Calls.Add(string(id))
	}
	b.calls("mod/billing.Draft", "mod/billing.Cycle")
	b.calls("mod/billing.Cycle", "mod/core.Parse")
	b.calls("mod/ui.Render", "mod/ui.Theme")
	return b
}

func TestProximityContributesNothingWithoutATask(t *testing.T) {
	if got := (Proximity{}).Compute(taskView().build(), params()); len(got) != 0 {
		t.Errorf("no task named, yet proximity scored %v", got)
	}
}

func TestProximityDecaysWithDistance(t *testing.T) {
	p := params()
	p.Task = []core.SymbolID{"mod/billing.Cycle"}

	got := Proximity{}.Compute(taskView().build(), p)

	if got["mod/billing.Cycle"] != 1 {
		t.Errorf("the seed itself = %v, want 1", got["mod/billing.Cycle"])
	}
	// Draft calls Cycle, Parse is called by Cycle: both one hop, direction
	// ignored.
	if got["mod/billing.Draft"] != got["mod/core.Parse"] {
		t.Errorf("callers and callees at one hop should score alike: %v vs %v",
			got["mod/billing.Draft"], got["mod/core.Parse"])
	}
	if got["mod/billing.Draft"] >= 1 || got["mod/billing.Draft"] <= 0 {
		t.Errorf("one hop out = %v, want between 0 and 1", got["mod/billing.Draft"])
	}
	// The UI component is disconnected entirely: no relationship, not a
	// distant one.
	if _, ok := got["mod/ui.Render"]; ok {
		t.Error("an unreachable symbol was given a proximity score")
	}
}

// Percentile normalization would report the nearest available symbol as
// maximally near even at nine hops. This signal is on an absolute scale.
func TestProximityIsSelfNormalizing(t *testing.T) {
	if !(Proximity{}).Normalized() {
		t.Fatal("Proximity must declare itself pre-normalized")
	}
	p := params()
	p.Task = []core.SymbolID{"mod/billing.Cycle"}
	for id, v := range (Proximity{}).Compute(taskView().build(), p) {
		if v < 0 || v > 1 {
			t.Errorf("%s scored %v, outside [0,1]", id, v)
		}
	}
}

func TestResolveTaskAcceptsWhateverCameToHand(t *testing.T) {
	v := taskView().build()

	cases := []struct {
		query     string
		wantSeeds []core.SymbolID
		label     string
	}{
		{"mod/billing", []core.SymbolID{"mod/billing.Cycle", "mod/billing.Draft"}, "full package path"},
		{"billing", []core.SymbolID{"mod/billing.Cycle", "mod/billing.Draft"}, "package suffix"},
		{"internal/ui", []core.SymbolID{"mod/ui.Render", "mod/ui.Theme"}, "directory"},
		{"internal/core/parse.go", []core.SymbolID{"mod/core.Parse"}, "file"},
		{"mod/core.Parse", []core.SymbolID{"mod/core.Parse"}, "exact symbol id"},
		{"core.Parse", []core.SymbolID{"mod/core.Parse"}, "short symbol name"},
		{"cycle", []core.SymbolID{"mod/billing.Cycle"}, "bare symbol name"},
	}

	for _, c := range cases {
		seeds, matched := ResolveTask(v, c.query)
		if len(seeds) != len(c.wantSeeds) {
			t.Errorf("%s: ResolveTask(%q) = %v (%s), want %v", c.label, c.query, seeds, matched, c.wantSeeds)
			continue
		}
		for i := range seeds {
			if seeds[i] != c.wantSeeds[i] {
				t.Errorf("%s: ResolveTask(%q)[%d] = %s, want %s", c.label, c.query, i, seeds[i], c.wantSeeds[i])
			}
		}
		if matched == "" {
			t.Errorf("%s: ResolveTask(%q) reported no match description", c.label, c.query)
		}
	}
}

func TestResolveTaskReportsNoMatch(t *testing.T) {
	v := taskView().build()
	if seeds, matched := ResolveTask(v, "nonexistent-area"); len(seeds) != 0 || matched != "" {
		t.Errorf("ResolveTask on an unknown area = %v (%q), want nothing", seeds, matched)
	}
	if seeds, _ := ResolveTask(v, "   "); len(seeds) != 0 {
		t.Errorf("blank query = %v, want nothing", seeds)
	}
}

// Scoping to most of the repo is the same as scoping to nothing.
func TestResolveTaskRejectsOverbroadFragments(t *testing.T) {
	b := newView()
	for _, name := range []string{"GetUser", "GetOrder", "GetItem", "GetCart", "SetPrice"} {
		id := core.SymbolID("mod/pkg." + name)
		b.v.Symbols[id] = core.Symbol{ID: id, Name: name, File: "pkg/a.go", Package: "mod/pkg"}
		b.v.Calls.Add(string(id))
	}

	if seeds, _ := ResolveTask(b.build(), "get"); len(seeds) != 0 {
		t.Errorf("a fragment matching most of the repo returned %d seeds; that is a common word, not a scope", len(seeds))
	}
}

// An exact package hit must not also drag in every symbol whose name contains
// the word.
func TestResolveTaskPrefersPrecision(t *testing.T) {
	b := taskView()
	b.v.Symbols["mod/other.BillingHelper"] = core.Symbol{
		ID: "mod/other.BillingHelper", Name: "BillingHelper", File: "internal/other/h.go", Package: "mod/other"}
	b.v.Calls.Add("mod/other.BillingHelper")

	seeds, matched := ResolveTask(b.build(), "billing")
	for _, s := range seeds {
		if s == "mod/other.BillingHelper" {
			t.Errorf("resolving %q pulled in a name-substring match: %v", matched, seeds)
		}
	}
}
