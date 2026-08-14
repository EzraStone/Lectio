// Package core holds the language-agnostic domain model that every other
// package speaks. Nothing in here knows about Go, about git, or about SQLite —
// that isolation is what makes the LanguageAdapter seam worth having.
package core

import "strings"

// SymbolKind classifies an indexed symbol. Ranking treats kinds differently:
// a type that everything embeds is a different kind of "central" than a
// function everything calls.
type SymbolKind string

const (
	KindFunc   SymbolKind = "func"
	KindMethod SymbolKind = "method"
	KindType   SymbolKind = "type"
	KindVar    SymbolKind = "var"
	KindConst  SymbolKind = "const"
)

// SymbolID is a stable, repo-unique identifier for a symbol.
//
// Stability matters more than readability here: IDs are the join key between
// the index (rebuilt on every run) and per-user state (which must survive
// re-indexing). The Go adapter uses go/types' fully qualified form, e.g.
// "github.com/x/y/pkg.Parse" or "github.com/x/y/pkg.(*Scheduler).Next".
type SymbolID string

// Package returns the package-path portion of the ID, or "" if the ID has no
// recognizable package qualifier.
func (id SymbolID) Package() string {
	s := string(id)
	// The package path is everything before the final "." that precedes either
	// a bare name or a "(recv)" method qualifier. Package paths contain "/"
	// and may contain "." (e.g. gopkg.in/yaml.v3), so scan from the right.
	if i := strings.LastIndex(s, ".("); i >= 0 {
		return s[:i]
	}
	i := strings.LastIndex(s, ".")
	if i < 0 {
		return ""
	}
	return s[:i]
}

// Short returns a display form: the last path element of the package plus the
// symbol name. "github.com/x/y/scheduler.Next" becomes "scheduler.Next".
func (id SymbolID) Short() string {
	s := string(id)
	pkg := id.Package()
	if pkg == "" {
		return s
	}
	rest := s[len(pkg):]
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	return pkg + rest
}

// Symbol is one indexed declaration.
type Symbol struct {
	ID        SymbolID
	Name      string
	Kind      SymbolKind
	Package   string
	File      string // repo-relative, slash-separated
	StartLine int
	EndLine   int
	Exported  bool
	// Doc is the leading comment, trimmed to a single line. It is used only to
	// render rationales and probe stems; it never influences grading.
	Doc string
}

// Lines returns the source span of the symbol, minimum 1.
func (s Symbol) Lines() int {
	if s.EndLine <= s.StartLine {
		return 1
	}
	return s.EndLine - s.StartLine + 1
}

// IsTest reports whether the symbol is itself test scaffolding. Test symbols
// are indexed — they are how coverage maps back to production code — but they
// are never ranked into a reading path.
func (s Symbol) IsTest() bool { return IsTestFile(s.File) }

// IsTestFile reports whether a repo-relative path holds tests.
func IsTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go") ||
		strings.Contains(path, "/testdata/") ||
		strings.HasPrefix(path, "testdata/")
}
