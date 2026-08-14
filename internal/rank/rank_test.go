package rank

import (
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/graph"
	"github.com/EzraStone/Lectio/internal/index"
)

var refNow = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// builder assembles a synthetic View. Signals are pure functions of the view,
// so a hand-built one is both faster and far clearer about what a test is
// actually asserting than a fixture repo would be.
type builder struct {
	v *index.View
}

func newView() *builder {
	return &builder{v: &index.View{
		Symbols:   map[core.SymbolID]core.Symbol{},
		CoveredBy: map[core.SymbolID][]core.SymbolID{},
		Calls:     graph.New(0),
		Now:       refNow,
	}}
}

// sym adds a symbol in a file.
func (b *builder) sym(id core.SymbolID, file string) *builder {
	name := string(id)
	for j := len(name) - 1; j >= 0; j-- {
		if name[j] == '.' {
			name = name[j+1:]
			break
		}
	}
	b.v.Symbols[id] = core.Symbol{ID: id, Name: name, Kind: core.KindFunc, File: file}
	b.v.Calls.Add(string(id))
	return b
}

// calls adds a dependency edge, creating both symbols in the given file if new.
func (b *builder) calls(from, to core.SymbolID) *builder {
	b.v.Calls.AddEdge(string(from), string(to))
	return b
}

// commit records a revision.
func (b *builder) commit(when time.Time, subject, author string, files ...string) *builder {
	c := core.Commit{
		Hash: subject, Subject: subject, AuthorEmail: author, When: when,
	}
	for _, f := range files {
		c.Files = append(c.Files, core.FileChange{Path: f, Added: 10})
	}
	b.v.Commits = append(b.v.Commits, c)
	return b
}

func (b *builder) aiCommit(when time.Time, subject string, files ...string) *builder {
	b.commit(when, subject, "dev@example.com", files...)
	b.v.Commits[len(b.v.Commits)-1].AIAssisted = true
	return b
}

func (b *builder) authorship(path, author string, lines int, lastActive time.Time) *builder {
	b.v.Authorship = append(b.v.Authorship, core.Authorship{
		Path: path, Author: author, Lines: lines, LastActive: lastActive,
	})
	return b
}

func (b *builder) imports(from, to string) *builder {
	b.v.Imports = append(b.v.Imports, core.ImportEdge{From: from, To: to})
	return b
}

func (b *builder) build() *index.View {
	b.v.StaticCalls = b.v.Calls
	return b.v
}

func params() Params {
	p := DefaultParams()
	p.Now = refNow
	return p
}

// ------------------------------------------------------------- centrality --

func TestCentralityRanksFanIn(t *testing.T) {
	b := newView().
		sym("pkg.parseInterval", "interval.go").
		sym("pkg.scheduler", "sched.go").
		sym("pkg.billing", "billing.go").
		sym("pkg.retry", "retry.go").
		sym("pkg.main", "main.go")
	b.calls("pkg.scheduler", "pkg.parseInterval")
	b.calls("pkg.billing", "pkg.parseInterval")
	b.calls("pkg.retry", "pkg.parseInterval")
	b.calls("pkg.main", "pkg.scheduler")

	got := Centrality{}.Compute(b.build(), params())
	if got["pkg.parseInterval"] <= got["pkg.main"] {
		t.Errorf("parseInterval (%v) should outrank main (%v)",
			got["pkg.parseInterval"], got["pkg.main"])
	}
	if got["pkg.parseInterval"] <= got["pkg.scheduler"] {
		t.Errorf("parseInterval (%v) should outrank its own caller (%v)",
			got["pkg.parseInterval"], got["pkg.scheduler"])
	}
}

func TestCentralityIgnoresTestSymbols(t *testing.T) {
	b := newView().sym("pkg.A", "a.go").sym("pkg.TestA", "a_test.go")
	b.calls("pkg.TestA", "pkg.A")

	got := Centrality{}.Compute(b.build(), params())
	if _, ok := got["pkg.TestA"]; ok {
		t.Error("a test symbol was scored for a reading path")
	}
	if _, ok := got["pkg.A"]; !ok {
		t.Error("production symbol missing")
	}
}

func TestDepthAndDependents(t *testing.T) {
	b := newView().sym("pkg.handler", "h.go").sym("pkg.store", "s.go").sym("pkg.types", "t.go")
	b.calls("pkg.handler", "pkg.store")
	b.calls("pkg.store", "pkg.types")
	v := b.build()

	depth := Depth(v)
	if depth["pkg.types"] != 0 || depth["pkg.handler"] != 2 {
		t.Errorf("depths = %v", depth)
	}
	deps := Dependents(v)
	if deps["pkg.types"] != 1 || deps["pkg.handler"] != 0 {
		t.Errorf("dependent counts = %v", deps)
	}
}

// ------------------------------------------------------------------ churn --

func TestChurnCountsWindowedCommits(t *testing.T) {
	b := newView().sym("pkg.hot", "hot.go").sym("pkg.cold", "cold.go")
	for i := 0; i < 5; i++ {
		b.commit(refNow.AddDate(0, 0, -i), "change", "dev@example.com", "hot.go")
	}
	b.commit(refNow.AddDate(0, 0, -1), "change", "dev@example.com", "cold.go")

	got := Churn{}.Compute(b.build(), params())
	if got["pkg.hot"] <= got["pkg.cold"] {
		t.Errorf("hot=%v cold=%v", got["pkg.hot"], got["pkg.cold"])
	}
}

func TestChurnExcludesCommitsOutsideTheWindow(t *testing.T) {
	b := newView().sym("pkg.ancient", "ancient.go")
	b.commit(refNow.AddDate(-3, 0, 0), "old change", "dev@example.com", "ancient.go")

	got := Churn{}.Compute(b.build(), params())
	if got["pkg.ancient"] != 0 {
		t.Errorf("a three-year-old commit contributed %v to churn", got["pkg.ancient"])
	}
}

// Recency weighting removes the window's cliff edge.
func TestChurnWeightsRecentCommitsHigher(t *testing.T) {
	recent := newView().sym("pkg.a", "a.go")
	recent.commit(refNow.AddDate(0, 0, -7), "c", "dev@example.com", "a.go")

	old := newView().sym("pkg.a", "a.go")
	old.commit(refNow.AddDate(0, -10, 0), "c", "dev@example.com", "a.go")

	r := Churn{}.Compute(recent.build(), params())["pkg.a"]
	o := Churn{}.Compute(old.build(), params())["pkg.a"]
	if r <= o {
		t.Errorf("a week-old commit (%v) should outweigh a ten-month-old one (%v)", r, o)
	}
}

// Moving a file must not reset its history to zero, or the most-reorganized
// code in a repo — usually the code under active work — reads as untouched.
func TestChurnFollowsRenames(t *testing.T) {
	b := newView().sym("pkg.moved", "new/path.go")
	b.commit(refNow.AddDate(0, 0, -30), "earlier work", "dev@example.com", "old/path.go")

	v := b.build()
	v.Commits = append(v.Commits, core.Commit{
		Hash: "mv", Subject: "move", When: refNow.AddDate(0, 0, -10),
		Files: []core.FileChange{{Path: "new/path.go", Renamed: "old/path.go", Added: 1}},
	})

	got := Churn{}.Compute(v, params())
	if got["pkg.moved"] == 0 {
		t.Error("a renamed file lost all of its churn")
	}
}

// ------------------------------------------------------------ fix density --

func TestFixDensityFavorsRepeatedlyFixedFiles(t *testing.T) {
	b := newView().sym("pkg.tricky", "tricky.go").sym("pkg.calm", "calm.go")

	for i := 0; i < 4; i++ {
		b.commit(refNow.AddDate(0, 0, -i*5), "fix: another edge case", "dev@example.com", "tricky.go")
	}
	b.commit(refNow.AddDate(0, 0, -3), "add feature", "dev@example.com", "tricky.go")

	for i := 0; i < 5; i++ {
		b.commit(refNow.AddDate(0, 0, -i*5), "add feature", "dev@example.com", "calm.go")
	}

	got := FixDensity{}.Compute(b.build(), params())
	if got["pkg.calm"] != 0 {
		t.Errorf("a file with no fixes scored %v", got["pkg.calm"])
	}
	if got["pkg.tricky"] <= 0 {
		t.Error("a repeatedly fixed file scored nothing")
	}
}

// Density and count both matter: four fixes in five commits is a different
// object from four fixes in two hundred.
func TestFixDensityWeighsShareNotJustCount(t *testing.T) {
	concentrated := newView().sym("pkg.a", "a.go")
	for i := 0; i < 4; i++ {
		concentrated.commit(refNow.AddDate(0, 0, -i), "fix: bug", "d@e.com", "a.go")
	}
	concentrated.commit(refNow, "feature", "d@e.com", "a.go")

	diluted := newView().sym("pkg.a", "a.go")
	for i := 0; i < 4; i++ {
		diluted.commit(refNow.AddDate(0, 0, -i), "fix: bug", "d@e.com", "a.go")
	}
	for i := 0; i < 100; i++ {
		diluted.commit(refNow.AddDate(0, 0, -i), "feature", "d@e.com", "a.go")
	}

	c := FixDensity{}.Compute(concentrated.build(), params())["pkg.a"]
	d := FixDensity{}.Compute(diluted.build(), params())["pkg.a"]
	if c <= d {
		t.Errorf("4-of-5 fixes (%v) should outrank 4-of-104 (%v)", c, d)
	}
}

func TestRevertsCountAsFixes(t *testing.T) {
	b := newView().sym("pkg.a", "a.go")
	b.commit(refNow, "Revert \"add caching\"", "d@e.com", "a.go")

	if got := (FixDensity{}).Compute(b.build(), params())["pkg.a"]; got <= 0 {
		t.Error("a revert should register as corrective")
	}
}
