package golang

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

// sampleRoot returns the fixture module. It lives under testdata with its own
// go.mod so the go tool ignores it, and it depends on nothing outside the
// standard library so loading it needs no network.
func sampleRoot(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	abs, err := filepath.Abs(filepath.Join("testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func symbolIndex(t *testing.T, syms []core.Symbol) map[core.SymbolID]core.Symbol {
	t.Helper()
	m := make(map[core.SymbolID]core.Symbol, len(syms))
	for _, s := range syms {
		m[s.ID] = s
	}
	return m
}

func TestDetect(t *testing.T) {
	a := New()
	ok, conf := a.Detect(sampleRoot(t))
	if !ok || conf < 0.9 {
		t.Errorf("Detect on a module root = (%v, %v), want (true, >=0.9)", ok, conf)
	}
	if ok, _ := a.Detect(t.TempDir()); ok {
		t.Error("Detect claimed an empty directory")
	}
}

func TestSymbolsExtractsEveryKind(t *testing.T) {
	root := sampleRoot(t)
	syms, err := New().Symbols(context.Background(), root)
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("no symbols extracted")
	}
	byID := symbolIndex(t, syms)

	cases := []struct {
		id   core.SymbolID
		kind core.SymbolKind
		file string
	}{
		{"example.com/sample.parseInterval", core.KindFunc, "interval.go"},
		{"example.com/sample.Parse", core.KindFunc, "interval.go"},
		{"example.com/sample.Interval", core.KindType, "interval.go"},
		{"example.com/sample.ErrBadInterval", core.KindVar, "interval.go"},
		{"example.com/sample.DefaultInterval", core.KindConst, "interval.go"},
		{"example.com/sample.(Interval).Describe", core.KindMethod, "interval.go"},
		{"example.com/sample/scheduler.(*Scheduler).Next", core.KindMethod, "scheduler/scheduler.go"},
		{"example.com/sample/scheduler.Clock", core.KindType, "scheduler/scheduler.go"},
		{"example.com/sample/billing.Cycle", core.KindFunc, "billing/billing.go"},
	}
	for _, c := range cases {
		got, ok := byID[c.id]
		if !ok {
			t.Errorf("missing symbol %s", c.id)
			continue
		}
		if got.Kind != c.kind {
			t.Errorf("%s kind = %q, want %q", c.id, got.Kind, c.kind)
		}
		if got.File != c.file {
			t.Errorf("%s file = %q, want %q", c.id, got.File, c.file)
		}
		if got.StartLine == 0 || got.EndLine < got.StartLine {
			t.Errorf("%s span = %d..%d", c.id, got.StartLine, got.EndLine)
		}
	}
}

func TestSymbolsRecordsExportedness(t *testing.T) {
	syms, err := New().Symbols(context.Background(), sampleRoot(t))
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	byID := symbolIndex(t, syms)

	if byID["example.com/sample.parseInterval"].Exported {
		t.Error("parseInterval is unexported")
	}
	if !byID["example.com/sample.Parse"].Exported {
		t.Error("Parse is exported")
	}
}

func TestSymbolsCapturesDoc(t *testing.T) {
	syms, err := New().Symbols(context.Background(), sampleRoot(t))
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	byID := symbolIndex(t, syms)

	doc := byID["example.com/sample.parseInterval"].Doc
	if doc == "" {
		t.Fatal("parseInterval has a doc comment that was not captured")
	}
	if len(doc) > 200 {
		t.Errorf("doc should be one trimmed line, got %d chars", len(doc))
	}
}

// Every instantiation of a generic must normalize to one symbol, or a generic
// helper fragments into several entries with separate ranking and separate
// probe history — and the set of entries changes whenever a caller changes.
func TestGenericsNormalizeToOrigin(t *testing.T) {
	syms, err := New().Symbols(context.Background(), sampleRoot(t))
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	byID := symbolIndex(t, syms)

	if _, ok := byID["example.com/sample/retry.countAll"]; !ok {
		t.Error("generic function countAll missing; instantiations should collapse to one symbol")
	}
	if _, ok := byID["example.com/sample/retry.(*Stack).Push"]; !ok {
		t.Error("generic method should be keyed without type arguments")
	}
	for id := range byID {
		if containsAny(string(id), "[", "]") {
			t.Errorf("symbol id %q carries type arguments", id)
		}
	}
}

func TestSymbolsAreRepoRelativeAndSorted(t *testing.T) {
	syms, err := New().Symbols(context.Background(), sampleRoot(t))
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	for i, s := range syms {
		if filepath.IsAbs(s.File) {
			t.Fatalf("symbol %s has an absolute path %q", s.ID, s.File)
		}
		if i > 0 && syms[i-1].ID > s.ID {
			t.Fatalf("symbols out of order at %d: %q then %q", i, syms[i-1].ID, s.ID)
		}
	}
}

func TestTestSymbolsAreIncludedButFlagged(t *testing.T) {
	syms, err := New().Symbols(context.Background(), sampleRoot(t))
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	byID := symbolIndex(t, syms)

	got, ok := byID["example.com/sample.TestParse"]
	if !ok {
		t.Fatal("test functions should be indexed; they are how coverage maps back to code")
	}
	if !got.IsTest() {
		t.Error("a symbol in a _test.go file must report IsTest")
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

// Generated code is indexed because it is real and things call it, but it is
// large, heavily depended on, and re-emitted wholesale on every schema change
// — so it scores well on size, centrality and churn at once. Left in a reading
// path it is exactly the "top ten fills with trivia" failure the spec warns of.
func TestGeneratedFilesAreFlagged(t *testing.T) {
	syms, err := New().Symbols(context.Background(), sampleRoot(t))
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	byID := symbolIndex(t, syms)

	gen, ok := byID["example.com/sample/generated.(*Request).GetSpec"]
	if !ok {
		t.Fatal("generated symbol was not indexed; it should be indexed, just not readable")
	}
	if !gen.Generated {
		t.Error("a file with the DO NOT EDIT header was not flagged as generated")
	}

	hand, ok := byID["example.com/sample.parseInterval"]
	if !ok {
		t.Fatal("parseInterval missing")
	}
	if hand.Generated {
		t.Error("hand-written code was flagged as generated")
	}
}
