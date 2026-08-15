package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/EzraStone/Lectio/internal/core"
)

// ResolveSpans implements adapter.SpanResolver.
//
// Syntax only — no type checking, no package loading, no module resolution.
// That is what makes symbol-level grading affordable: the backtest resolves
// every file of every commit a newcomer made across ninety days, and each of
// those is one parse of one file measured in microseconds. Type-checking them
// would mean loading a package graph per commit and would cost more than the
// rest of Gate A put together.
//
// It also means this works on revisions that do not build, which is most of
// what a historical corpus contains.
func (a *Adapter) ResolveSpans(src []byte, ranges []core.LineRange) ([]string, error) {
	if len(ranges) == 0 || len(src) == 0 {
		return nil, nil
	}

	fset := token.NewFileSet()
	// ParseComments so doc comments are attached and counted as part of the
	// declaration: editing a function's doc comment is editing that function,
	// and attributing it to whatever precedes it would be worse than useless.
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if file == nil {
		return nil, err
	}
	// A parse error is not fatal. Go's parser recovers and returns a partial
	// AST, and a partial answer about a historical revision beats none —
	// especially since the revisions that fail to parse are disproportionately
	// the old ones a backtest most wants to reach.

	seen := make(map[string]bool, 8)
	var out []string
	add := func(name string, start, end int) {
		if name == "" || seen[name] {
			return
		}
		for _, r := range ranges {
			if r.Touches(start, end) {
				seen[name] = true
				out = append(out, name)
				return
			}
		}
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			start, end := declBounds(fset, d, d.Doc)
			add(funcLocalName(d), start, end)

		case *ast.GenDecl:
			// A grouped declaration is not one symbol. `var ( a = 1; b = 2 )`
			// declares two, and a change to one must not attribute to both —
			// the whole point of moving to symbol granularity is losing the
			// coarseness that file-level grading had.
			for _, spec := range d.Specs {
				for _, name := range specNames(spec) {
					start, end := declBounds(fset, spec, d.Doc)
					add(name, start, end)
				}
			}
		}
	}
	return out, err
}

// declBounds returns the line span of a node, extended upward over its doc
// comment when the comment sits above this node rather than a previous one.
func declBounds(fset *token.FileSet, n ast.Node, doc *ast.CommentGroup) (int, int) {
	start := fset.Position(n.Pos()).Line
	end := fset.Position(n.End()).Line
	if doc != nil {
		if d := fset.Position(doc.Pos()).Line; d > 0 && d < start {
			start = d
		}
	}
	return start, end
}

// funcLocalName renders a function or method the way core.SymbolID does after
// the package path: "Parse", or "(*Scheduler).Next".
func funcLocalName(d *ast.FuncDecl) string {
	if d.Name == nil {
		return ""
	}
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	recv := receiverExprName(d.Recv.List[0].Type)
	if recv == "" {
		return d.Name.Name
	}
	return "(" + recv + ")." + d.Name.Name
}

// receiverExprName renders a receiver type as "T" or "*T", dropping type
// parameters so Stack[T] and Stack[int] name the same method — matching how
// symbolID normalizes generics to their origin.
func receiverExprName(e ast.Expr) string {
	star := ""
	if s, ok := e.(*ast.StarExpr); ok {
		star = "*"
		e = s.X
	}
	switch t := e.(type) {
	case *ast.Ident:
		return star + t.Name
	case *ast.IndexExpr: // Stack[T]
		if id, ok := t.X.(*ast.Ident); ok {
			return star + id.Name
		}
	case *ast.IndexListExpr: // Pair[K, V]
		if id, ok := t.X.(*ast.Ident); ok {
			return star + id.Name
		}
	case *ast.SelectorExpr:
		return star + t.Sel.Name
	}
	return ""
}

// specNames returns the identifiers a spec declares, skipping blanks and
// imports.
func specNames(spec ast.Spec) []string {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		if s.Name != nil && s.Name.Name != "_" {
			return []string{s.Name.Name}
		}
	case *ast.ValueSpec:
		out := make([]string, 0, len(s.Names))
		for _, n := range s.Names {
			if n.Name != "_" {
				out = append(out, n.Name)
			}
		}
		return out
	}
	return nil
}

// LocalName strips the package path from a symbol ID, giving the form
// ResolveSpans returns.
//
// Lives here rather than on core.Symbol because the "<pkg>.<local>" layout is
// this adapter's convention, and core is meant not to know how any language
// names things.
func LocalName(sym core.Symbol) string {
	id := string(sym.ID)
	if sym.Package != "" {
		if trimmed := strings.TrimPrefix(id, sym.Package+"."); trimmed != id {
			return trimmed
		}
	}
	// No package prefix: a universe-scope symbol, or an ID built before the
	// package was known. The whole ID is the local name.
	return id
}
