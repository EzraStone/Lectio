package backtest

import (
	"context"
	"testing"

	golangadapter "github.com/EzraStone/Lectio/internal/adapter/golang"
	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
)

// fakeHunks stands in for git so attribution can be tested without a
// repository, which keeps the drift and rename cases explicit rather than
// hidden inside a fixture's history.
type fakeHunks struct {
	hunks map[string]map[string][]core.LineRange // commit -> path -> ranges
	files map[string]map[string]string           // commit -> path -> source
}

func (f fakeHunks) Hunks(_ context.Context, _, commit string) (map[string][]core.LineRange, error) {
	return f.hunks[commit], nil
}

func (f fakeHunks) FileAt(_ context.Context, _, commit, path string) ([]byte, error) {
	return []byte(f.files[commit][path]), nil
}

func viewWith(syms ...core.Symbol) *index.View {
	v := &index.View{Symbols: map[core.SymbolID]core.Symbol{}}
	for _, s := range syms {
		v.Symbols[s.ID] = s
	}
	return v
}

// The whole reason attribution goes through names: by the time a newcomer
// commits, the file has moved under them. Here Parse sits at lines 3-5 in the
// index and at lines 103-105 when the change lands. A position-based match
// would attribute the change to whatever now occupies lines 3-5.
func TestAttributionSurvivesLineDrift(t *testing.T) {
	const later = `package sample

// ... a hundred lines of other people's work elided ...

func Unrelated() {}

func Parse(s string) error {
	return nil
}
`
	h := fakeHunks{
		hunks: map[string]map[string][]core.LineRange{
			"c1": {"parse.go": {{Start: 7, Count: 1}}}, // inside Parse in `later`
		},
		files: map[string]map[string]string{"c1": {"parse.go": later}},
	}
	v := viewWith(
		core.Symbol{ID: "m/pkg.Parse", Name: "Parse", Package: "m/pkg", File: "parse.go", StartLine: 3, EndLine: 5},
		core.Symbol{ID: "m/pkg.Unrelated", Name: "Unrelated", Package: "m/pkg", File: "parse.go", StartLine: 7, EndLine: 7},
	)

	got, err := AttributeSymbols(context.Background(), h, golangadapter.New(), v, "/repo",
		[]core.Commit{{Hash: "c1"}}, NewPathTracker(map[string]bool{"parse.go": true}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "m/pkg.Parse" {
		t.Errorf("got %v, want [m/pkg.Parse] — attribution followed line numbers instead of names", got)
	}
}

// A contributor editing internal/rank/score.go is editing the file that was
// rank.go when they arrived. Scoring against a path that did not exist then
// would count a correct prediction as a miss.
func TestAttributionFollowsRenames(t *testing.T) {
	const src = "package p\n\nfunc Score() int { return 1 }\n"
	h := fakeHunks{
		hunks: map[string]map[string][]core.LineRange{
			"c2": {"internal/rank/score.go": {{Start: 3, Count: 1}}},
		},
		files: map[string]map[string]string{"c2": {"internal/rank/score.go": src}},
	}
	v := viewWith(core.Symbol{
		ID: "m/rank.Score", Name: "Score", Package: "m/rank", File: "rank.go", StartLine: 3, EndLine: 3,
	})

	paths := NewPathTracker(map[string]bool{"rank.go": true})
	paths.Observe(core.Commit{Files: []core.FileChange{{Path: "internal/rank/score.go", Renamed: "rank.go"}}})

	got, _ := AttributeSymbols(context.Background(), h, golangadapter.New(), v, "/repo",
		[]core.Commit{{Hash: "c2"}}, paths)
	if len(got) != 1 || got[0] != "m/rank.Score" {
		t.Errorf("got %v, want [m/rank.Score] across the rename", got)
	}
}

// Files the newcomer created did not exist at the rewind point, so nothing
// could have recommended reading them and they are not a prediction target.
func TestAttributionIgnoresFilesCreatedAfterTheRewind(t *testing.T) {
	h := fakeHunks{
		hunks: map[string]map[string][]core.LineRange{
			"c3": {"brand_new.go": {{Start: 3, Count: 1}}},
		},
		files: map[string]map[string]string{"c3": {"brand_new.go": "package p\n\nfunc New() {}\n"}},
	}
	v := viewWith(core.Symbol{ID: "m/p.New", Name: "New", Package: "m/p", File: "brand_new.go"})

	got, _ := AttributeSymbols(context.Background(), h, golangadapter.New(), v, "/repo",
		[]core.Commit{{Hash: "c3"}}, NewPathTracker(map[string]bool{"old.go": true}))
	if len(got) != 0 {
		t.Errorf("got %v, want nothing — a file created after the rewind was scored", got)
	}
}

// Every package has a New and half have a String. Matching on a bare name
// would make one contributor's edit attribute to symbols across the repo.
func TestAttributionDoesNotCollideAcrossFiles(t *testing.T) {
	const src = "package p\n\nfunc New() int { return 1 }\n"
	h := fakeHunks{
		hunks: map[string]map[string][]core.LineRange{"c4": {"a.go": {{Start: 3, Count: 1}}}},
		files: map[string]map[string]string{"c4": {"a.go": src}},
	}
	v := viewWith(
		core.Symbol{ID: "m/a.New", Name: "New", Package: "m/a", File: "a.go", StartLine: 3, EndLine: 3},
		core.Symbol{ID: "m/b.New", Name: "New", Package: "m/b", File: "b.go", StartLine: 3, EndLine: 3},
	)

	got, _ := AttributeSymbols(context.Background(), h, golangadapter.New(), v, "/repo",
		[]core.Commit{{Hash: "c4"}}, NewPathTracker(map[string]bool{"a.go": true, "b.go": true}))
	if len(got) != 1 || got[0] != "m/a.New" {
		t.Errorf("got %v, want [m/a.New] only", got)
	}
}

func TestAttributionIsDeterministic(t *testing.T) {
	const src = "package p\n\nfunc A() {}\n\nfunc B() {}\n\nfunc C() {}\n"
	h := fakeHunks{
		hunks: map[string]map[string][]core.LineRange{
			"c5": {"x.go": {{Start: 3, Count: 1}, {Start: 5, Count: 1}, {Start: 7, Count: 1}}},
		},
		files: map[string]map[string]string{"c5": {"x.go": src}},
	}
	v := viewWith(
		core.Symbol{ID: "m/p.C", Name: "C", Package: "m/p", File: "x.go"},
		core.Symbol{ID: "m/p.A", Name: "A", Package: "m/p", File: "x.go"},
		core.Symbol{ID: "m/p.B", Name: "B", Package: "m/p", File: "x.go"},
	)

	first, _ := AttributeSymbols(context.Background(), h, golangadapter.New(), v, "/repo",
		[]core.Commit{{Hash: "c5"}}, NewPathTracker(map[string]bool{"x.go": true}))
	for i := 0; i < 20; i++ {
		got, _ := AttributeSymbols(context.Background(), h, golangadapter.New(), v, "/repo",
			[]core.Commit{{Hash: "c5"}}, NewPathTracker(map[string]bool{"x.go": true}))
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("order moved between runs: %v then %v", first, got)
			}
		}
	}
	if len(first) != 3 || first[0] != "m/p.A" {
		t.Errorf("got %v, want all three sorted by ID", first)
	}
}

// A rename chain a -> b -> c must resolve all the way back, or a file that
// moved twice inside the horizon stops being scorable.
func TestPathTrackerFollowsChains(t *testing.T) {
	p := NewPathTracker(map[string]bool{"a.go": true})
	p.Observe(core.Commit{Files: []core.FileChange{{Path: "b.go", Renamed: "a.go"}}})
	p.Observe(core.Commit{Files: []core.FileChange{{Path: "c.go", Renamed: "b.go"}}})

	if origin, ok := p.Origin("c.go"); !ok || origin != "a.go" {
		t.Errorf("Origin(c.go) = %q, %v — want a.go", origin, ok)
	}
	if _, ok := p.Origin("a.go"); ok {
		t.Error("the old path is still resolvable after the move")
	}
	if _, ok := p.Origin("b.go"); ok {
		t.Error("an intermediate path is still resolvable")
	}
}

func TestAttributionDegradesWhenNoResolverIsAvailable(t *testing.T) {
	got, err := AttributeSymbols(context.Background(), fakeHunks{}, nil, viewWith(), "/repo", nil, NewPathTracker(nil))
	if err != nil || got != nil {
		t.Errorf("an adapter without a span resolver should yield nothing, quietly: %v %v", got, err)
	}
}
