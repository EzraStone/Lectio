package rank

import (
	"fmt"
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

// The spec's own example: touch the serializer, update the mobile schema. The
// two have no import path between them, which is exactly what makes the pair
// worth surfacing.
func TestHiddenCouplingFindsTheSurprisePair(t *testing.T) {
	b := newView().
		sym("api.Serialize", "internal/api/serialize.go").
		sym("api.Handler", "internal/api/handler.go")

	for i := 0; i < 6; i++ {
		b.commit(refNow.AddDate(0, 0, -i*7), fmt.Sprintf("change %d", i), "dev@example.com",
			"internal/api/serialize.go", "mobile/schema.json")
	}
	// Handler churns on its own, so churn alone would not distinguish it.
	for i := 0; i < 6; i++ {
		b.commit(refNow.AddDate(0, 0, -i*7-2), fmt.Sprintf("handler %d", i), "dev@example.com",
			"internal/api/handler.go")
	}

	v := b.build()
	got := HiddenCoupling{}.Compute(v, params())

	if got["api.Serialize"] <= 0 {
		t.Fatal("the serializer/schema pair was not surfaced")
	}
	if got["api.Handler"] != 0 {
		t.Errorf("a file with no hidden coupling scored %v", got["api.Handler"])
	}
}

// The subtraction is the whole idea: a pair the compiler already tells you
// about is not a surprise.
func TestStaticDependencyDisqualifiesAPair(t *testing.T) {
	b := newView().
		sym("api.Handler", "internal/api/handler.go").
		sym("store.Save", "internal/store/save.go")
	b.calls("api.Handler", "store.Save")
	b.v.Symbols["api.Handler"] = core.Symbol{
		ID: "api.Handler", File: "internal/api/handler.go", Package: "internal/api"}
	b.v.Symbols["store.Save"] = core.Symbol{
		ID: "store.Save", File: "internal/store/save.go", Package: "internal/store"}

	for i := 0; i < 8; i++ {
		b.commit(refNow.AddDate(0, 0, -i*3), "change", "dev@example.com",
			"internal/api/handler.go", "internal/store/save.go")
	}

	pairs := HiddenCoupling{}.Pairs(b.build(), params())
	if len(pairs) == 0 {
		t.Fatal("the pair was not detected at all")
	}
	for _, p := range pairs {
		if p.Hidden {
			t.Errorf("pair %s/%s has a call edge between them and must not count as hidden", p.A, p.B)
		}
	}
}

func TestImportEdgeDisqualifiesAPair(t *testing.T) {
	b := newView()
	b.v.Symbols["a.F"] = core.Symbol{ID: "a.F", File: "a/f.go", Package: "mod/a"}
	b.v.Symbols["b.G"] = core.Symbol{ID: "b.G", File: "b/g.go", Package: "mod/b"}
	b.v.Calls.Add("a.F")
	b.v.Calls.Add("b.G")
	b.imports("mod/a", "mod/b")

	for i := 0; i < 8; i++ {
		b.commit(refNow.AddDate(0, 0, -i*3), "change", "dev@example.com", "a/f.go", "b/g.go")
	}

	for _, p := range (HiddenCoupling{}).Pairs(b.build(), params()) {
		if p.Hidden {
			t.Errorf("an import path exists between %s and %s; the pair is not hidden", p.A, p.B)
		}
	}
}

func TestSamePackageFilesAreNotHidden(t *testing.T) {
	b := newView()
	b.v.Symbols["a.F"] = core.Symbol{ID: "a.F", File: "pkg/f.go", Package: "mod/pkg"}
	b.v.Symbols["a.G"] = core.Symbol{ID: "a.G", File: "pkg/g.go", Package: "mod/pkg"}
	b.v.Calls.Add("a.F")
	b.v.Calls.Add("a.G")

	for i := 0; i < 8; i++ {
		b.commit(refNow.AddDate(0, 0, -i*3), "change", "dev@example.com", "pkg/f.go", "pkg/g.go")
	}

	for _, p := range (HiddenCoupling{}).Pairs(b.build(), params()) {
		if p.Hidden {
			t.Errorf("%s and %s are in one package; a reader meets them together anyway", p.A, p.B)
		}
	}
}

// A reformat or a license sweep touches hundreds of files and produces pairs
// quadratically. A few such commits would otherwise dominate the signal.
func TestGiantCommitsAreIgnored(t *testing.T) {
	b := newView().sym("pkg.A", "a.go").sym("pkg.B", "b.go")

	var everything []string
	for i := 0; i < 60; i++ {
		everything = append(everything, fmt.Sprintf("file%02d.go", i))
	}
	everything = append(everything, "a.go", "b.go")

	for i := 0; i < 10; i++ {
		b.commit(refNow.AddDate(0, 0, -i), "chore: gofmt the tree", "dev@example.com", everything...)
	}

	if got := (HiddenCoupling{}).Compute(b.build(), params()); len(got) != 0 {
		t.Errorf("a repeated tree-wide reformat produced coupling scores: %v", got)
	}
}

func TestCoincidenceNeedsSupport(t *testing.T) {
	b := newView().sym("pkg.A", "a.go")
	// Two shared commits is coincidence; the threshold is three.
	for i := 0; i < 2; i++ {
		b.commit(refNow.AddDate(0, 0, -i), "change", "dev@example.com", "a.go", "unrelated/thing.md")
	}

	if got := (HiddenCoupling{}).Compute(b.build(), params()); got["pkg.A"] != 0 {
		t.Errorf("two co-changes scored %v; that is coincidence, not coupling", got["pkg.A"])
	}
}

// Jaccard, not raw count: a file that changes constantly should not appear
// coupled to everything it happens to overlap with.
func TestStrengthDiscountsUbiquitousFiles(t *testing.T) {
	b := newView().sym("pkg.A", "a.go")

	// a.go and partner.json always change together: 5 of 5.
	for i := 0; i < 5; i++ {
		b.commit(refNow.AddDate(0, 0, -i), "focused change", "dev@example.com", "a.go", "partner.json")
	}
	// a.go also overlaps with a file that changes constantly on its own.
	for i := 0; i < 5; i++ {
		b.commit(refNow.AddDate(0, 0, -i-10), "overlap", "dev@example.com", "a.go", "busy.json")
	}
	for i := 0; i < 80; i++ {
		b.commit(refNow.AddDate(0, 0, -i), "busy churns alone", "dev@example.com", "busy.json")
	}

	pairs := HiddenCoupling{}.Pairs(b.build(), params())
	strength := map[string]float64{}
	for _, p := range pairs {
		other := p.B
		if p.B == "a.go" {
			other = p.A
		}
		strength[other] = p.Strength
	}

	if strength["partner.json"] <= strength["busy.json"] {
		t.Errorf("a dedicated partner (%v) should outrank an omnipresent one (%v)",
			strength["partner.json"], strength["busy.json"])
	}
}

func TestHiddenPairsForRationale(t *testing.T) {
	b := newView().sym("api.Serialize", "internal/api/serialize.go")
	for i := 0; i < 6; i++ {
		b.commit(refNow.AddDate(0, 0, -i*7), "change", "dev@example.com",
			"internal/api/serialize.go", "mobile/schema.json")
	}

	got := HiddenCoupling{}.HiddenPairsFor(b.build(), params(), "internal/api/serialize.go", 3)
	if len(got) == 0 {
		t.Fatal("no pairs returned for rationale text")
	}
	if got[0].Together != 6 {
		t.Errorf("Together = %d, want 6", got[0].Together)
	}
	if got[0].Strength <= 0 || got[0].Strength > 1 {
		t.Errorf("Strength = %v, want (0,1]", got[0].Strength)
	}
}

func TestPairsAreDeterministic(t *testing.T) {
	b := newView().sym("pkg.A", "a.go").sym("pkg.B", "b.go")
	for i := 0; i < 5; i++ {
		b.commit(refNow.AddDate(0, 0, -i), "c", "d@e.com", "a.go", "x.json")
		b.commit(refNow.AddDate(0, 0, -i), "c", "d@e.com", "b.go", "y.json")
	}
	v := b.build()

	first := HiddenCoupling{}.Pairs(v, params())
	for i := 0; i < 10; i++ {
		got := HiddenCoupling{}.Pairs(v, params())
		if len(got) != len(first) {
			t.Fatalf("run %d returned %d pairs, first returned %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d differed at %d: %+v vs %+v", i, j, got[j], first[j])
			}
		}
	}
}
