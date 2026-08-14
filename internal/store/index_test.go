package store

import (
	"context"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
)

func TestWriteAndReadIndex(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	when := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	err := s.WriteIndex(ctx, func(w *IndexWriter) error {
		if err := w.Symbol(core.Symbol{
			ID: "pkg.parseInterval", Name: "parseInterval", Kind: core.KindFunc,
			Package: "pkg", File: "pkg/interval.go", StartLine: 10, EndLine: 40,
			Exported: false, Doc: "parseInterval parses a duration spec.",
		}); err != nil {
			return err
		}
		if err := w.Symbol(core.Symbol{
			ID: "pkg.Scheduler", Name: "Scheduler", Kind: core.KindType,
			Package: "pkg", File: "pkg/sched.go", StartLine: 1, EndLine: 5, Exported: true,
		}); err != nil {
			return err
		}
		if err := w.CallEdge(core.CallEdge{
			Caller: "pkg.Scheduler", Callee: "pkg.parseInterval",
			Kind: core.EdgeStatic, File: "pkg/sched.go", Line: 3,
		}); err != nil {
			return err
		}
		if err := w.CallEdge(core.CallEdge{
			Caller: "pkg.Scheduler", Callee: "pkg.mystery",
			Kind: core.EdgeDynamic, File: "pkg/sched.go", Line: 4,
		}); err != nil {
			return err
		}
		if err := w.ImportEdge(core.ImportEdge{From: "pkg", To: "time"}); err != nil {
			return err
		}
		if err := w.Coverage(core.CoverageEdge{
			Test: "pkg.TestParse", Symbol: "pkg.parseInterval", Fraction: 0.8,
		}); err != nil {
			return err
		}
		if err := w.Commit(core.Commit{
			Hash: "abc123", AuthorName: "A Dev", AuthorEmail: "dev@example.com",
			When: when, Subject: "fix: off-by-one in parseInterval",
			Files: []core.FileChange{{Path: "pkg/interval.go", Added: 3, Deleted: 1}},
		}); err != nil {
			return err
		}
		return w.Authorship(core.Authorship{
			Path: "pkg/interval.go", Author: "dev@example.com", Lines: 30, LastActive: when,
		})
	})
	if err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	syms, err := s.Symbols(ctx)
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("symbols = %d, want 2", len(syms))
	}
	if syms[0].ID != "pkg.Scheduler" || !syms[0].Exported {
		t.Errorf("first symbol = %+v", syms[0])
	}
	if syms[1].File != "pkg/interval.go" || syms[1].EndLine != 40 {
		t.Errorf("second symbol = %+v", syms[1])
	}

	sym, ok, err := s.SymbolByID(ctx, "pkg.parseInterval")
	if err != nil || !ok {
		t.Fatalf("SymbolByID: ok=%v err=%v", ok, err)
	}
	if sym.Doc == "" {
		t.Error("doc was not round-tripped")
	}
	if _, ok, err := s.SymbolByID(ctx, "pkg.nope"); err != nil || ok {
		t.Errorf("missing symbol: ok=%v err=%v, want false, nil", ok, err)
	}

	all, err := s.CallEdges(ctx, false)
	if err != nil || len(all) != 2 {
		t.Fatalf("CallEdges(all) = %d edges, err %v; want 2", len(all), err)
	}
	static, err := s.CallEdges(ctx, true)
	if err != nil || len(static) != 1 {
		t.Fatalf("CallEdges(staticOnly) = %d edges, err %v; want 1", len(static), err)
	}
	if static[0].Callee != "pkg.parseInterval" {
		t.Errorf("static edge = %+v", static[0])
	}

	commits, err := s.Commits(ctx, time.Time{})
	if err != nil || len(commits) != 1 {
		t.Fatalf("Commits = %d, err %v; want 1", len(commits), err)
	}
	if !commits[0].IsFix() {
		t.Error("commit should classify as a fix")
	}
	if len(commits[0].Files) != 1 || commits[0].Files[0].Added != 3 {
		t.Errorf("commit files = %+v", commits[0].Files)
	}
	if !commits[0].When.Equal(when) {
		t.Errorf("commit time = %v, want %v", commits[0].When, when)
	}

	cov, err := s.Coverage(ctx)
	if err != nil || len(cov) != 1 || !cov[0].Covers() {
		t.Errorf("Coverage = %+v, err %v", cov, err)
	}

	auth, err := s.Authorship(ctx)
	if err != nil || len(auth) != 1 || auth[0].Lines != 30 {
		t.Errorf("Authorship = %+v, err %v", auth, err)
	}

	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Symbols != 2 || st.CallEdges != 2 || st.Files != 2 || st.Commits != 1 {
		t.Errorf("Stats = %+v", st)
	}
}

func TestCommitsFiltersBySince(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	err := s.WriteIndex(ctx, func(w *IndexWriter) error {
		if err := w.Commit(core.Commit{Hash: "old", When: old, Subject: "ancient",
			Files: []core.FileChange{{Path: "a.go"}}}); err != nil {
			return err
		}
		return w.Commit(core.Commit{Hash: "new", When: recent, Subject: "recent",
			Files: []core.FileChange{{Path: "b.go"}}})
	})
	if err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	got, err := s.Commits(ctx, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Commits: %v", err)
	}
	if len(got) != 1 || got[0].Hash != "new" {
		t.Fatalf("windowed commits = %+v, want just \"new\"", got)
	}
	// File changes must be windowed with their commit, not fetched wholesale.
	if len(got[0].Files) != 1 || got[0].Files[0].Path != "b.go" {
		t.Errorf("windowed commit files = %+v", got[0].Files)
	}
}

func TestFileIDsAreMemoized(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	err := s.WriteIndex(ctx, func(w *IndexWriter) error {
		for i := 0; i < 3; i++ {
			if err := w.Symbol(core.Symbol{
				ID: core.SymbolID("pkg.S" + string(rune('A'+i))), Name: "S", Kind: core.KindFunc,
				Package: "pkg", File: "pkg/same.go",
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}
	if got := count(t, s.DB(), "files"); got != 1 {
		t.Errorf("files = %d, want 1 — three symbols in one file must share a row", got)
	}
}

func TestTestFilesAreFlagged(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	err := s.WriteIndex(ctx, func(w *IndexWriter) error {
		if _, err := w.File("pkg/thing.go", 100); err != nil {
			return err
		}
		_, err := w.File("pkg/thing_test.go", 50)
		return err
	})
	if err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	var isTest int
	if err := s.DB().QueryRow(`SELECT is_test FROM files WHERE path='pkg/thing_test.go'`).Scan(&isTest); err != nil {
		t.Fatalf("query: %v", err)
	}
	if isTest != 1 {
		t.Error("_test.go file was not flagged")
	}
}
