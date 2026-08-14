package golang

import (
	"context"
	"testing"

	"github.com/EzraStone/Lectio/internal/adapter"
	"github.com/EzraStone/Lectio/internal/core"
)

func TestCoverageIsOffByDefault(t *testing.T) {
	edges, err := New().TestCoverage(context.Background(), sampleRoot(t))
	if err != nil {
		t.Fatalf("TestCoverage: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("coverage ran without being asked: %d edges", len(edges))
	}
}

func TestCoverageMapsTestsToSymbols(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the fixture's test suite")
	}
	opts := adapter.DefaultOptions()
	opts.RunTests = true

	a := New()
	a.Configure(opts)
	edges, err := a.TestCoverage(context.Background(), sampleRoot(t))
	if err != nil {
		t.Fatalf("TestCoverage: %v", err)
	}
	if len(edges) == 0 {
		t.Fatal("no coverage edges; the fixture has tests that exercise Parse")
	}

	covered := make(map[core.SymbolID]core.CoverageEdge)
	for _, e := range edges {
		covered[e.Symbol] = e
	}

	for _, want := range []core.SymbolID{
		"example.com/sample.Parse",
		"example.com/sample.parseInterval",
	} {
		e, ok := covered[want]
		if !ok {
			t.Errorf("no coverage recorded for %s", want)
			continue
		}
		if !IsTestBinary(e.Test) {
			t.Errorf("coverage for %s is attributed to %q, which is not a test binary", want, e.Test)
		}
		if e.Fraction <= 0 || e.Fraction > 1 {
			t.Errorf("%s coverage fraction = %v, want (0,1]", want, e.Fraction)
		}
	}

	// Nothing exercises Describe, so it must not appear as covered.
	if _, ok := covered["example.com/sample.(Interval).Describe"]; ok {
		t.Error("Describe is never called by the tests but was reported as covered")
	}
}

func TestSpanIndexFindsEnclosingSymbol(t *testing.T) {
	idx := newSpanIndex([]core.Symbol{
		{ID: "p.A", File: "a.go", StartLine: 10, EndLine: 20},
		{ID: "p.B", File: "a.go", StartLine: 25, EndLine: 40},
		{ID: "p.C", File: "b.go", StartLine: 1, EndLine: 5},
	})

	cases := []struct {
		file string
		line int
		want core.SymbolID
		ok   bool
	}{
		{"a.go", 15, "p.A", true},
		{"a.go", 10, "p.A", true},
		{"a.go", 20, "p.A", true},
		{"a.go", 30, "p.B", true},
		{"a.go", 22, "", false}, // between symbols
		{"a.go", 100, "", false},
		{"b.go", 3, "p.C", true},
		{"c.go", 1, "", false},
	}
	for _, c := range cases {
		got, ok := idx.at(c.file, c.line)
		if got != c.want || ok != c.ok {
			t.Errorf("at(%s, %d) = (%q, %v), want (%q, %v)", c.file, c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestTestBinaryIDRoundTrip(t *testing.T) {
	id := TestBinaryID("example.com/sample/billing")
	if !IsTestBinary(id) {
		t.Errorf("IsTestBinary(%q) = false", id)
	}
	if IsTestBinary("example.com/sample/billing.Cycle") {
		t.Error("a real symbol was mistaken for a test binary")
	}
}
