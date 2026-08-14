// Package adapter defines the seam between language-specific analysis and
// everything above it.
//
// Ranking, probes, and state are language-agnostic and sit above this
// interface. Going Go-first therefore costs one adapter, not the product — if
// the Go bet is wrong, a second implementation of these four methods is the
// entire port.
package adapter

import (
	"context"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
)

// LanguageAdapter extracts ground truth from a repository.
//
// The method set is deliberately small and deliberately fixed at phase 0. Every
// method returns facts, not judgments: nothing in an adapter decides what is
// worth reading, and nothing in an adapter grades an answer. That keeps the
// question "is our ground truth correct?" answerable per-language, in isolation
// from the question "is our ranking any good?".
type LanguageAdapter interface {
	// Name identifies the adapter in output and in the index metadata.
	Name() string

	// Detect reports whether this adapter can analyze the repository at root,
	// and a rough confidence in [0,1] used to pick between adapters when more
	// than one claims a polyglot repo.
	Detect(root string) (ok bool, confidence float64)

	// Symbols enumerates every declaration worth indexing.
	Symbols(ctx context.Context, root string) ([]core.Symbol, error)

	// CallEdges returns the dependency graph, oriented caller -> callee, plus
	// package-level import edges.
	CallEdges(ctx context.Context, root string) ([]core.CallEdge, []core.ImportEdge, error)

	// TestCoverage maps tests to the symbols they exercise. Implementations may
	// return an empty slice when the repo's tests do not run; a missing
	// coverage map degrades ranking but must never fail indexing.
	TestCoverage(ctx context.Context, root string) ([]core.CoverageEdge, error)

	// FileHistory returns per-file revision history within the window.
	//
	// History is not language-specific, and the Go adapter satisfies this by
	// delegating to the shared git provider. It stays on the interface because
	// a language whose history lives elsewhere — vendored monorepo, generated
	// source with an upstream — needs the room to say so.
	FileHistory(ctx context.Context, root string, since time.Time) ([]core.Commit, error)
}

// Options tune analysis cost. Indexing a large repo with full dynamic-dispatch
// resolution is minutes; the defaults trade some recall for a run that finishes
// while someone is still watching.
type Options struct {
	// IncludeTests indexes test files as symbols. Required for coverage
	// mapping; test symbols are still excluded from reading paths.
	IncludeTests bool

	// ResolveDynamic enables over-approximating resolution of interface and
	// func-value calls. Off means only type-checker-exact edges are recorded.
	ResolveDynamic bool

	// RunTests executes the suite to collect a coverage profile. Off by
	// default: running arbitrary tests from a repo you just cloned is a
	// decision the user makes, not one the tool makes for them.
	RunTests bool

	// HistoryWindow bounds how far back history is read. Twelve months is the
	// churn window the ranking signals assume.
	HistoryWindow time.Duration

	// MaxPackages caps analysis breadth; 0 means unlimited.
	MaxPackages int
}

// DefaultOptions returns the settings a plain `lectio index` uses.
func DefaultOptions() Options {
	return Options{
		IncludeTests:   true,
		ResolveDynamic: true,
		RunTests:       false,
		HistoryWindow:  365 * 24 * time.Hour,
	}
}

// Configurable is implemented by adapters that accept Options. Adapters that
// ignore tuning simply do not implement it.
type Configurable interface {
	Configure(Options)
}
