package adapter

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
)

type fake struct {
	name string
	ok   bool
	conf float64
}

func (f fake) Name() string                                           { return f.name }
func (f fake) Detect(string) (bool, float64)                          { return f.ok, f.conf }
func (f fake) Symbols(context.Context, string) ([]core.Symbol, error) { return nil, nil }
func (f fake) CallEdges(context.Context, string) ([]core.CallEdge, []core.ImportEdge, error) {
	return nil, nil, nil
}
func (f fake) TestCoverage(context.Context, string) ([]core.CoverageEdge, error) { return nil, nil }
func (f fake) FileHistory(context.Context, string, time.Time) ([]core.Commit, error) {
	return nil, nil
}

func reset() {
	mu.Lock()
	registered = nil
	mu.Unlock()
}

func TestSelectPicksHighestConfidence(t *testing.T) {
	reset()
	defer reset()

	Register(fake{name: "js", ok: true, conf: 0.3})
	Register(fake{name: "go", ok: true, conf: 0.9})
	Register(fake{name: "rust", ok: false, conf: 1.0})

	got, err := Select("/repo")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got.Name() != "go" {
		t.Errorf("Select picked %q, want %q", got.Name(), "go")
	}
}

func TestSelectNoMatch(t *testing.T) {
	reset()
	defer reset()

	Register(fake{name: "go", ok: false})
	_, err := Select("/repo")
	if err == nil {
		t.Fatal("expected an error when no adapter matches")
	}
	if !strings.Contains(err.Error(), "go") {
		t.Errorf("error should list registered adapters, got %q", err)
	}
}

func TestByName(t *testing.T) {
	reset()
	defer reset()

	Register(fake{name: "go", ok: true, conf: 1})
	if _, err := ByName("go"); err != nil {
		t.Fatalf("ByName(go): %v", err)
	}
	if _, err := ByName("cobol"); err == nil {
		t.Fatal("expected an error for an unregistered adapter")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	reset()
	defer reset()

	defer func() {
		if recover() == nil {
			t.Fatal("registering the same adapter name twice should panic")
		}
	}()
	Register(fake{name: "go"})
	Register(fake{name: "go"})
}
