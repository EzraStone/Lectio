package golang

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/EzraStone/Lectio/internal/core"
)

// Symbols enumerates every declaration in the repository.
func (a *Adapter) Symbols(ctx context.Context, root string) ([]core.Symbol, error) {
	pkgs, err := a.load(ctx, root)
	if err != nil {
		return nil, err
	}

	// go/packages loads a package twice when tests are included — once plain,
	// once as the test variant carrying the _test.go files. Both report the
	// same PkgPath, so IDs collide by design and the map keeps one copy while
	// still picking up symbols that only exist in the test variant.
	seen := make(map[core.SymbolID]core.Symbol, 4096)

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			rel, ok := relPath(root, pkg.Fset, file)
			if !ok {
				continue // generated or outside the repo; not something anyone reads
			}
			if !a.opts.IncludeTests && core.IsTestFile(rel) {
				continue
			}
			// ast.IsGenerated implements the convention from the Go docs: a
			// "Code generated … DO NOT EDIT." line before the package clause.
			// Using the standard library's reading of it rather than a regex
			// of our own keeps this agreeing with every other Go tool.
			generated := ast.IsGenerated(file)
			for _, sym := range fileSymbols(pkg, file, rel) {
				sym.Generated = generated
				if _, dup := seen[sym.ID]; !dup {
					seen[sym.ID] = sym
				}
			}
		}
	}

	out := make([]core.Symbol, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// fileSymbols extracts the declarations in one file.
func fileSymbols(pkg *packages.Package, file *ast.File, rel string) []core.Symbol {
	var out []core.Symbol
	fset := pkg.Fset

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			obj, ok := pkg.TypesInfo.Defs[d.Name].(*types.Func)
			if !ok || obj == nil {
				continue
			}
			kind := core.KindFunc
			if d.Recv != nil {
				kind = core.KindMethod
			}
			out = append(out, core.Symbol{
				ID:        symbolID(obj),
				Name:      obj.Name(),
				Kind:      kind,
				Package:   pkgPath(pkg),
				File:      rel,
				StartLine: line(fset, d.Pos()),
				EndLine:   line(fset, d.End()),
				Exported:  obj.Exported(),
				Doc:       firstLine(d.Doc),
			})

		case *ast.GenDecl:
			out = append(out, genDeclSymbols(pkg, d, rel)...)
		}
	}
	return out
}

func genDeclSymbols(pkg *packages.Package, d *ast.GenDecl, rel string) []core.Symbol {
	var kind core.SymbolKind
	switch d.Tok {
	case token.TYPE:
		kind = core.KindType
	case token.VAR:
		kind = core.KindVar
	case token.CONST:
		kind = core.KindConst
	default:
		return nil // imports carry no symbol of their own
	}

	fset := pkg.Fset
	var out []core.Symbol

	for _, spec := range d.Specs {
		var names []*ast.Ident
		var doc *ast.CommentGroup
		var start, end token.Pos

		switch s := spec.(type) {
		case *ast.TypeSpec:
			names, doc, start, end = []*ast.Ident{s.Name}, s.Doc, s.Pos(), s.End()
		case *ast.ValueSpec:
			names, doc, start, end = s.Names, s.Doc, s.Pos(), s.End()
		default:
			continue
		}
		if doc == nil {
			// A single-spec declaration carries its comment on the GenDecl.
			doc = d.Doc
		}

		for _, name := range names {
			obj := pkg.TypesInfo.Defs[name]
			if obj == nil || name.Name == "_" {
				continue
			}
			out = append(out, core.Symbol{
				ID:        symbolID(obj),
				Name:      obj.Name(),
				Kind:      kind,
				Package:   pkgPath(pkg),
				File:      rel,
				StartLine: line(fset, start),
				EndLine:   line(fset, end),
				Exported:  obj.Exported(),
				Doc:       firstLine(doc),
			})
		}
	}
	return out
}

// symbolID builds the stable identifier for an object.
//
// The format is documented on core.SymbolID and parsed by it, so it must stay
// in sync: "<pkgpath>.<Name>" for plain declarations,
// "<pkgpath>.(<recv>).<Name>" for methods.
//
// Generic functions and methods normalize to their origin. Without that, every
// instantiation of a generic helper becomes a separate symbol with its own
// ranking and its own probe history, which is both wrong and unstable — the
// set of instantiations changes whenever a caller changes.
func symbolID(obj types.Object) core.SymbolID {
	if obj == nil {
		return ""
	}
	pkg := obj.Pkg()
	if pkg == nil {
		return core.SymbolID(obj.Name()) // universe scope: error, builtins
	}
	path := pkg.Path()

	if fn, ok := obj.(*types.Func); ok {
		if origin := fn.Origin(); origin != nil {
			fn = origin
		}
		if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
			return core.SymbolID(path + ".(" + receiverName(sig.Recv().Type()) + ")." + fn.Name())
		}
		return core.SymbolID(path + "." + fn.Name())
	}
	return core.SymbolID(path + "." + obj.Name())
}

// receiverName renders a method receiver as "T" or "*T", dropping type
// arguments so Stack[int] and Stack[string] name the same method.
func receiverName(t types.Type) string {
	star := ""
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
		star = "*"
	}
	if named, ok := t.(*types.Named); ok {
		if origin := named.Origin(); origin != nil {
			named = origin
		}
		return star + named.Obj().Name()
	}
	return star + types.TypeString(t, func(*types.Package) string { return "" })
}

// pkgPath returns the import path, stripping the "_test" suffix that
// go/packages gives external test packages so their symbols land under the
// package they exercise.
func pkgPath(pkg *packages.Package) string {
	return strings.TrimSuffix(pkg.PkgPath, "_test")
}

// relPath converts a file's absolute path to a repo-relative one, reporting
// false for files outside the repo (dependencies) or with no position.
func relPath(root string, fset *token.FileSet, file *ast.File) (string, bool) {
	pos := fset.Position(file.Pos())
	if !pos.IsValid() || pos.Filename == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, pos.Filename)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func line(fset *token.FileSet, p token.Pos) int {
	if !p.IsValid() {
		return 0
	}
	return fset.Position(p).Line
}

// firstLine reduces a doc comment to one line, for rationales and probe stems.
// Doc text never influences grading, only presentation.
func firstLine(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	text := strings.TrimSpace(g.Text())
	if text == "" {
		return ""
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	const maxDoc = 160
	if len(text) > maxDoc {
		text = strings.TrimSpace(text[:maxDoc]) + "…"
	}
	return text
}
