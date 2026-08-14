package golang

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/tools/cover"
	"golang.org/x/tools/go/packages"

	"github.com/EzraStone/Lectio/internal/core"
)

// TestBinaryMarker distinguishes a test-binary pseudo-symbol from a real one.
//
// Coverage is attributed per test binary — one Go test package — rather than
// per test function. Go's coverage profile is written once per test binary, so
// per-function attribution would mean re-running the suite once per test:
// hundreds of compilations and full-suite runtimes to sharpen ground truth
// that is already good enough to answer "which tests go red if you change
// this". The granularity is a deliberate cost decision and shows up in the
// answer text as "the tests in package X".
const TestBinaryMarker = ".[tests]"

// TestBinaryID names the pseudo-symbol for a package's test binary.
func TestBinaryID(pkgPath string) core.SymbolID {
	return core.SymbolID(pkgPath + TestBinaryMarker)
}

// IsTestBinary reports whether an id names a test binary rather than a symbol.
func IsTestBinary(id core.SymbolID) bool {
	return strings.HasSuffix(string(id), TestBinaryMarker)
}

// coverageTimeout bounds one package's test run. A suite that hangs must not
// hang indexing.
const coverageTimeout = 90 * time.Second

// TestCoverage maps test binaries to the symbols they exercise.
//
// Off unless Options.RunTests is set. Running an arbitrary repo's test suite
// is a decision the person who cloned it makes, not one an indexer makes on
// their behalf — tests start databases, bind ports, and occasionally talk to
// the network.
//
// A package whose tests fail is still useful: the profile written before the
// failure covers whatever ran. Failures are therefore ignored rather than
// propagated, and a package that produces no profile at all contributes
// nothing without stopping the others.
func (a *Adapter) TestCoverage(ctx context.Context, root string) ([]core.CoverageEdge, error) {
	if !a.opts.RunTests {
		return nil, nil
	}

	pkgs, err := a.load(ctx, root)
	if err != nil {
		return nil, err
	}
	syms, err := a.Symbols(ctx, root)
	if err != nil {
		return nil, err
	}

	spans := newSpanIndex(syms)
	fileOf := profileFileMap(pkgs, root)

	tmp, err := os.MkdirTemp("", "lectio-cover-")
	if err != nil {
		return nil, fmt.Errorf("coverage workspace: %w", err)
	}
	defer os.RemoveAll(tmp)

	var out []core.CoverageEdge
	for i, pkgPath := range testPackages(pkgs) {
		profile := filepath.Join(tmp, fmt.Sprintf("cover-%d.out", i))
		if !runCoverage(ctx, root, pkgPath, profile) {
			continue
		}
		edges, err := attribute(profile, TestBinaryID(pkgPath), spans, fileOf)
		if err != nil {
			continue
		}
		out = append(out, edges...)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Test != out[j].Test {
			return out[i].Test < out[j].Test
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out, nil
}

// runCoverage runs one package's tests and reports whether a profile landed.
//
// -coverpkg=./... is what makes this worth doing: without it a package's tests
// only report coverage of that same package, and the interesting fact — that
// scheduler's tests exercise the interval parser three packages away — is
// exactly what gets lost.
func runCoverage(ctx context.Context, root, pkgPath, profile string) bool {
	runCtx, cancel := context.WithTimeout(ctx, coverageTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "go", "test",
		"-covermode=set",
		"-coverpkg=./...",
		"-coverprofile="+profile,
		"-count=1",
		pkgPath,
	)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	_ = cmd.Run() // a failing suite still leaves a usable profile

	info, err := os.Stat(profile)
	return err == nil && info.Size() > 0
}

// attribute maps profile blocks onto symbols.
func attribute(profile string, test core.SymbolID, spans *spanIndex, fileOf map[string]string) ([]core.CoverageEdge, error) {
	profiles, err := cover.ParseProfiles(profile)
	if err != nil {
		return nil, err
	}

	type tally struct{ covered, total int }
	counts := make(map[core.SymbolID]*tally)

	for _, p := range profiles {
		rel, ok := fileOf[p.FileName]
		if !ok {
			continue // a dependency outside the repo
		}
		for _, b := range p.Blocks {
			sym, ok := spans.at(rel, b.StartLine)
			if !ok {
				continue // package-level declaration, or a file we did not index
			}
			t := counts[sym]
			if t == nil {
				t = &tally{}
				counts[sym] = t
			}
			t.total += b.NumStmt
			if b.Count > 0 {
				t.covered += b.NumStmt
			}
		}
	}

	out := make([]core.CoverageEdge, 0, len(counts))
	for sym, t := range counts {
		if t.total == 0 || t.covered == 0 {
			continue
		}
		out = append(out, core.CoverageEdge{
			Test:     test,
			Symbol:   sym,
			Fraction: float64(t.covered) / float64(t.total),
		})
	}
	return out, nil
}

// testPackages lists import paths that have test files.
func testPackages(pkgs []*packages.Package) []string {
	seen := make(map[string]bool)
	var out []string
	for _, pkg := range pkgs {
		for _, f := range pkg.CompiledGoFiles {
			if !strings.HasSuffix(f, "_test.go") {
				continue
			}
			p := pkgPath(pkg)
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
			break
		}
	}
	sort.Strings(out)
	return out
}

// profileFileMap maps the names Go writes into a coverage profile
// ("<import path>/<file>") to repo-relative paths.
func profileFileMap(pkgs []*packages.Package, root string) map[string]string {
	out := make(map[string]string, len(pkgs)*4)
	for _, pkg := range pkgs {
		for _, abs := range pkg.CompiledGoFiles {
			rel, err := filepath.Rel(root, abs)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			out[path.Join(pkgPath(pkg), filepath.Base(abs))] = filepath.ToSlash(rel)
		}
	}
	return out
}

// spanIndex answers "which symbol contains this line", per file.
type spanIndex struct {
	byFile map[string][]core.Symbol
}

func newSpanIndex(syms []core.Symbol) *spanIndex {
	idx := &spanIndex{byFile: make(map[string][]core.Symbol)}
	for _, s := range syms {
		if s.StartLine == 0 {
			continue
		}
		idx.byFile[s.File] = append(idx.byFile[s.File], s)
	}
	for _, list := range idx.byFile {
		sort.Slice(list, func(i, j int) bool { return list[i].StartLine < list[j].StartLine })
	}
	return idx
}

// at returns the innermost symbol whose span contains line.
//
// Binary search finds the last symbol starting at or before the line, then the
// scan backwards handles nesting: a method declared inside the span of nothing
// is the common case, but a type and its methods can interleave in ways that
// make the first hit the wrong one.
func (idx *spanIndex) at(file string, line int) (core.SymbolID, bool) {
	list := idx.byFile[file]
	if len(list) == 0 {
		return "", false
	}
	i := sort.Search(len(list), func(i int) bool { return list[i].StartLine > line }) - 1
	for ; i >= 0; i-- {
		if list[i].EndLine >= line {
			return list[i].ID, true
		}
		// Symbols are sorted by start line; once one ends before the target and
		// no enclosing span reaches it, earlier ones are unlikely to. Keep
		// scanning a bounded distance to tolerate nesting.
		if list[i].EndLine < line && line-list[i].EndLine > 2000 {
			break
		}
	}
	return "", false
}
