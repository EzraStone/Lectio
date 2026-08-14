// Package golang implements the LanguageAdapter for Go.
//
// Go is first not because it is the biggest market — TypeScript is, by a wide
// margin, and has far more AI-generated code — but because v1 hinges on
// ground-truth grading, and ground truth means the call graph has to be right.
// Go ships a first-party, maintained call-graph package, has static types and
// explicit imports, and hands over the test-to-function mapping through
// `go test -coverprofile` for free. The alternatives fail on that specific
// axis: Python's dynamic dispatch makes static call graphs unreliable enough
// to undermine the premise, tree-sitter gives syntax without semantics, and
// Java is analyzable right up until Spring.
package golang

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/EzraStone/Lectio/internal/adapter"
	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/vcs"
)

// Adapter analyzes Go repositories.
type Adapter struct {
	opts adapter.Options

	// History is the revision-history provider. It is injectable so tests can
	// run without a git binary, and so the backtest can supply a provider
	// pinned to a historical commit.
	History vcs.History

	mu     sync.Mutex
	loaded map[string]*loadResult // keyed by root, so one run loads once
}

// New returns a Go adapter with default options.
func New() *Adapter {
	return &Adapter{
		opts:   adapter.DefaultOptions(),
		loaded: make(map[string]*loadResult),
	}
}

// Name identifies the adapter.
func (a *Adapter) Name() string { return "go" }

// Configure applies analysis options.
func (a *Adapter) Configure(o adapter.Options) {
	a.opts = o
	a.mu.Lock()
	a.loaded = make(map[string]*loadResult)
	a.mu.Unlock()
}

// Detect reports whether root looks like a Go repository.
//
// A go.mod is near-conclusive. Loose .go files without a module still work but
// score lower, so that a repo which is mostly something else with a stray Go
// script does not get claimed.
func (a *Adapter) Detect(root string) (bool, float64) {
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return true, 0.95
	}
	found := false
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable subtree is not a detection failure
		}
		if d.IsDir() {
			if skipDir(d.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil || !found {
		return false, 0
	}
	return true, 0.6
}

// FileHistory delegates to the shared git provider.
//
// History is not language-specific and pretending otherwise would mean four
// adapters each shelling out to git slightly differently. It stays on the
// interface because a language whose history lives elsewhere — vendored
// monorepo, generated source with an upstream — needs the room to say so.
func (a *Adapter) FileHistory(ctx context.Context, root string, since time.Time) ([]core.Commit, error) {
	h := a.History
	if h == nil {
		h = vcs.NewGit()
	}
	return h.Commits(ctx, root, since)
}

// loadResult caches one packages.Load for a root. Loading is the expensive
// step by an order of magnitude, and Symbols, CallEdges and TestCoverage all
// need the same result.
type loadResult struct {
	pkgs []*packages.Package
	err  error
}

const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo |
	packages.NeedModule

// load type-checks the repository, memoized per root.
func (a *Adapter) load(ctx context.Context, root string) ([]*packages.Package, error) {
	a.mu.Lock()
	cached, ok := a.loaded[root]
	a.mu.Unlock()
	if ok {
		return cached.pkgs, cached.err
	}

	cfg := &packages.Config{
		Mode:    loadMode,
		Dir:     root,
		Context: ctx,
		Tests:   a.opts.IncludeTests,
		// Analysis must never run the repo's generators or build steps.
		Env: append(os.Environ(), "GOFLAGS=-mod=mod"),
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		err = fmt.Errorf("load Go packages in %s: %w", root, err)
	} else {
		pkgs = usable(pkgs, a.opts.MaxPackages)
		if len(pkgs) == 0 {
			err = fmt.Errorf("no type-checkable Go packages found in %s", root)
		}
	}

	a.mu.Lock()
	a.loaded[root] = &loadResult{pkgs: pkgs, err: err}
	a.mu.Unlock()
	return pkgs, err
}

// usable drops packages that cannot contribute ground truth.
//
// Partial failure is the normal case, not the exception: a repo pinned to an
// unavailable dependency, a generated file that is missing, a build tag that
// does not apply here. Indexing eleven of twelve packages is worth far more
// than refusing to index at all, so broken packages are dropped and the rest
// proceed. Callers surface the count.
func usable(pkgs []*packages.Package, max int) []*packages.Package {
	out := make([]*packages.Package, 0, len(pkgs))
	for _, p := range pkgs {
		if p.Types == nil || p.TypesInfo == nil || len(p.Syntax) == 0 {
			continue
		}
		// go/packages synthesizes a main package per test binary; it holds no
		// source anyone wrote.
		if strings.HasSuffix(p.PkgPath, ".test") {
			continue
		}
		out = append(out, p)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

// LoadDiagnostics reports how many packages failed to type-check, for the CLI
// to warn about. A repo where most packages fail will produce a call graph too
// incomplete to grade against, and the user needs to know that before they
// trust an answer.
func (a *Adapter) LoadDiagnostics(ctx context.Context, root string) (loaded, failed int, err error) {
	pkgs, err := a.load(ctx, root)
	if err != nil {
		return 0, 0, err
	}
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			failed++
		}
	}
	return len(pkgs), failed, nil
}

func skipDir(name string) bool {
	switch name {
	case ".git", "vendor", "node_modules", "testdata", ".lectio":
		return true
	}
	return strings.HasPrefix(name, ".")
}
