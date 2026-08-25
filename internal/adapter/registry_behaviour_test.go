package adapter

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
)

// stubAdapter is the smallest thing the registry will accept, so registry
// behaviour can be tested without pulling in a language implementation.
type stubAdapter struct {
	name       string
	confidence float64
}

func (s stubAdapter) Name() string { return s.name }
func (s stubAdapter) Detect(string) (bool, float64) {
	return s.confidence > 0, s.confidence
}
func (stubAdapter) Symbols(context.Context, string) ([]core.Symbol, error) { return nil, nil }
func (stubAdapter) CallEdges(context.Context, string) ([]core.CallEdge, []core.ImportEdge, error) {
	return nil, nil, nil
}
func (stubAdapter) TestCoverage(context.Context, string) ([]core.CoverageEdge, error) {
	return nil, nil
}
func (stubAdapter) FileHistory(context.Context, string, time.Time) ([]core.Commit, error) {
	return nil, nil
}

// Registered returns a copy, sorted. A caller that mutated the slice would
// otherwise reorder the registry for every later caller in the process.
func TestRegisteredReturnsASortedCopy(t *testing.T) {
	// Registered out of order, so sorting is doing work rather than
	// preserving an order that was already correct.
	Register(stubAdapter{name: "zeta"})
	Register(stubAdapter{name: "alpha"})

	got := Registered()
	if len(got) < 2 {
		t.Fatalf("registered two adapters, got %d", len(got))
	}

	names := make([]string, len(got))
	for i, a := range got {
		names[i] = a.Name()
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("Registered is not sorted: %v", names)
	}

	// Mutating the returned slice must not affect the next call.
	got[0] = nil
	if again := Registered(); len(again) > 0 && again[0] == nil {
		t.Error("Registered handed out its own backing array")
	}
}

// The defaults are what every command uses when nothing says otherwise, and
// two of them are decisions rather than conveniences.
func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()

	// Tests are indexed even though they are never recommended: coverage maps
	// back through them, and blast radius needs to know they exist.
	if !o.IncludeTests {
		t.Error("tests are not indexed by default; coverage cannot map back")
	}
	// CHA is what makes an interface's implementations reachable for ranking.
	if !o.ResolveDynamic {
		t.Error("dynamic dispatch is unresolved by default")
	}
	// Running a repository's tests executes its code. That has to be opt-in.
	if o.RunTests {
		t.Error("RunTests defaults to true — indexing would execute arbitrary repository code")
	}
	if o.HistoryWindow <= 0 {
		t.Errorf("HistoryWindow = %v, want a positive window", o.HistoryWindow)
	}
	// AsOf unset means "now", which is what a normal index wants; the backtest
	// sets it explicitly.
	if !o.AsOf.IsZero() {
		t.Errorf("AsOf = %v, want the zero time", o.AsOf)
	}
}

// The window is a duration rather than a date so the same options can be
// anchored anywhere. A default measured in days rather than months would
// silence churn on most repositories.
func TestDefaultHistoryWindowIsAboutAYear(t *testing.T) {
	got := DefaultOptions().HistoryWindow
	if got < 300*24*time.Hour || got > 400*24*time.Hour {
		t.Errorf("HistoryWindow = %v, want roughly twelve months", got)
	}
}
