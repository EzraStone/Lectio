package store

import (
	"context"
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

// Import edges are written on every index and read back by nothing the test
// suite exercised, which made a silent failure here invisible.
//
// They are not decorative. `relatedness` uses them to decide whether two files
// changing together is already explained by one package importing the other,
// and hidden coupling is defined as the co-changes that explanation does not
// cover. If this returned nothing, every import-explained co-change would read
// as hidden and the differentiator would fill with noise — the same failure
// mode a degraded type-check produces, arriving by a different route.
func TestImportEdgesRoundTrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	want := []core.ImportEdge{
		{From: "app/server", To: "app/store"},
		{From: "app/server", To: "net/http"},
		{From: "app/store", To: "database/sql"},
	}
	if err := s.WriteIndex(ctx, func(w *IndexWriter) error {
		for _, e := range want {
			if err := w.ImportEdge(e); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := s.ImportEdges(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("read back %d edges, wrote %d", len(got), len(want))
	}
	seen := map[core.ImportEdge]bool{}
	for _, e := range got {
		seen[e] = true
	}
	for _, e := range want {
		if !seen[e] {
			t.Errorf("%s -> %s did not survive the round trip", e.From, e.To)
		}
	}
}

// Direction is the whole content of an import edge. A store that lost it would
// make `relatedness` symmetric in a way the graph is not.
func TestImportEdgesKeepTheirDirection(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.WriteIndex(ctx, func(w *IndexWriter) error {
		return w.ImportEdge(core.ImportEdge{From: "app", To: "lib"})
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := s.ImportEdges(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d edges, want 1", len(got))
	}
	if got[0].From != "app" || got[0].To != "lib" {
		t.Errorf("edge came back as %s -> %s, want app -> lib", got[0].From, got[0].To)
	}
}

// A repository with no imports at all — a single-package program with no
// dependencies — reads back empty rather than failing.
func TestImportEdgesOnAnIndexWithNone(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.WriteIndex(ctx, func(w *IndexWriter) error {
		return w.Symbol(core.Symbol{ID: "main.main", Name: "main", Package: "main", File: "main.go"})
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := s.ImportEdges(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d edges from an index with none: %v", len(got), got)
	}
}

// Stats counts what is on disk, and a count that disagreed with what
// ImportEdges returns would mean one of them is reading the wrong table.
func TestStatsAgreesWithImportEdges(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.WriteIndex(ctx, func(w *IndexWriter) error {
		for _, e := range []core.ImportEdge{
			{From: "a", To: "b"}, {From: "a", To: "c"}, {From: "b", To: "c"},
		} {
			if err := w.ImportEdge(e); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	edges, err := s.ImportEdges(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if st.ImportEdges != len(edges) {
		t.Errorf("Stats reports %d import edges, ImportEdges returns %d", st.ImportEdges, len(edges))
	}
	if st.ImportEdges != 3 {
		t.Errorf("Stats reports %d import edges, wrote 3", st.ImportEdges)
	}
}
