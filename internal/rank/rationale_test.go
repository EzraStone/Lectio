package rank

import (
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

func TestRationalesQuoteCheckableEvidence(t *testing.T) {
	v := fullView().build()
	p := params()
	f := Gather(v, p)

	res := Rank(v, p, DefaultWeights())
	items := Annotate(res.Path(10), f, "")

	if len(items) == 0 {
		t.Fatal("no items to explain")
	}
	for _, it := range items {
		if it.Rationale == "" {
			t.Errorf("%s has no rationale", it.Symbol.ID)
		}
		// A rationale that quotes a score is not checkable, which defeats the
		// purpose.
		if strings.Contains(it.Rationale, "0.") {
			t.Errorf("%s: rationale quotes a raw score: %q", it.Symbol.ID, it.Rationale)
		}
	}
}

func TestCouplingRationaleNamesThePartner(t *testing.T) {
	v := fullView().build()
	p := params()
	f := Gather(v, p)

	item := Item{
		Symbol:        core.Symbol{ID: "mod/api.Serialize", File: "api/serialize.go"},
		Contributions: map[Signal]float64{SignalCoupling: 0.9},
	}
	got := f.Explain(item, "")

	if !strings.Contains(got, "mobile/schema.json") {
		t.Errorf("rationale should name the coupled file, got %q", got)
	}
	if !strings.Contains(got, "imports") {
		t.Errorf("rationale should say why the pair is surprising, got %q", got)
	}
}

func TestCentralityRationaleCountsCallers(t *testing.T) {
	v := fullView().build()
	f := Gather(v, params())

	item := Item{
		Symbol:        core.Symbol{ID: "mod/core.parseInterval", File: "core/interval.go"},
		Contributions: map[Signal]float64{SignalCentrality: 0.9},
	}
	got := f.Explain(item, "")

	if !strings.Contains(got, "2 callers") {
		t.Errorf("rationale = %q, want a caller count", got)
	}
}

func TestFixRationaleQuotesTheRatio(t *testing.T) {
	v := fullView().build()
	f := Gather(v, params())

	item := Item{
		Symbol:        core.Symbol{ID: "mod/core.parseInterval", File: "core/interval.go"},
		Contributions: map[Signal]float64{SignalFixDensity: 0.9},
	}
	got := f.Explain(item, "")

	if !strings.Contains(got, "8 of its 8") {
		t.Errorf("rationale = %q, want the fix-to-commit ratio", got)
	}
}

func TestOrphaningRationaleQuotesAPercentage(t *testing.T) {
	v := fullView().build()
	f := Gather(v, params())

	item := Item{
		Symbol:        core.Symbol{ID: "mod/core.parseInterval", File: "core/interval.go"},
		Contributions: map[Signal]float64{SignalOrphaning: 0.9},
	}
	got := f.Explain(item, "")

	if !strings.Contains(got, "100%") || !strings.Contains(got, "90 days") {
		t.Errorf("rationale = %q, want an orphaned percentage and the threshold", got)
	}
}

func TestProximityRationaleNamesTheTask(t *testing.T) {
	b := taskView()
	v := b.build()
	p := params()
	p.Task = []core.SymbolID{"mod/billing.Cycle"}
	f := Gather(v, p)

	seed := Item{
		Symbol:        core.Symbol{ID: "mod/billing.Cycle", File: "internal/billing/cycle.go"},
		Contributions: map[Signal]float64{SignalProximity: 1},
	}
	if got := f.Explain(seed, "package mod/billing"); !strings.Contains(got, "inside package mod/billing") {
		t.Errorf("seed rationale = %q", got)
	}

	near := Item{
		Symbol:        core.Symbol{ID: "mod/core.Parse", File: "internal/core/parse.go"},
		Contributions: map[Signal]float64{SignalProximity: 0.7},
	}
	if got := f.Explain(near, "package mod/billing"); !strings.Contains(got, "1 hop") {
		t.Errorf("neighbor rationale = %q, want a hop count", got)
	}
}

// A symbol carried by an even spread rather than one standout has no specific
// reason to quote, and saying so beats inventing one.
func TestRationaleFallsBackHonestly(t *testing.T) {
	f := Gather(newView().build(), params())

	bare := Item{Symbol: core.Symbol{ID: "mod.X", File: "x.go"}}
	got := f.Explain(bare, "")
	if got == "" {
		t.Fatal("no fallback rationale")
	}
	if !strings.Contains(got, "without one standing out") {
		t.Errorf("fallback = %q", got)
	}

	documented := Item{Symbol: core.Symbol{ID: "mod.Y", File: "y.go", Doc: "Y coordinates the retry loop."}}
	if got := f.Explain(documented, ""); got != "Y coordinates the retry loop." {
		t.Errorf("a documented symbol should fall back to its doc line, got %q", got)
	}
}

func TestPlural(t *testing.T) {
	cases := map[int]string{0: "0 callers", 1: "1 caller", 2: "2 callers"}
	for n, want := range cases {
		if got := plural(n, "caller"); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestGatherBoundsTransitiveDependents(t *testing.T) {
	b := newView()
	// A ten-deep chain: everything transitively depends on the tail, but the
	// count reported is bounded at three hops.
	prev := core.SymbolID("mod.n0")
	b.sym(prev, "n.go")
	for i := 1; i < 10; i++ {
		cur := core.SymbolID("mod.n" + string(rune('0'+i)))
		b.sym(cur, "n.go")
		b.calls(cur, prev)
		prev = cur
	}

	f := Gather(b.build(), params())
	if got := f.TransitiveDeps["mod.n0"]; got != 3 {
		t.Errorf("transitive dependents of the chain tail = %d, want 3 (bounded)", got)
	}
}
