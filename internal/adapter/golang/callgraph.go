package golang

import (
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"sort"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/EzraStone/Lectio/internal/core"
)

// CallEdges returns the repository's dependency graph and its import graph.
//
// Two passes, and the split is the point:
//
// The static pass resolves calls through go/types. Every edge it produces is
// one the type checker proved, and those are the only edges a probe is ever
// graded against.
//
// The dynamic pass resolves interface dispatch with class hierarchy analysis.
// CHA conservatively assumes every concrete type reaches every interface, so
// its edges are sound but imprecise — an interface with four implementations
// yields four edges where execution takes one. Those edges are marked
// EdgeDynamic, feed ranking, and are excluded from grading. Marking someone
// wrong for failing to name a call that no execution reaches would break the
// trust property the whole product rests on.
func (a *Adapter) CallEdges(ctx context.Context, root string) ([]core.CallEdge, []core.ImportEdge, error) {
	pkgs, err := a.load(ctx, root)
	if err != nil {
		return nil, nil, err
	}

	local := a.localSymbols(ctx, root)
	if len(local) == 0 {
		return nil, nil, fmt.Errorf("no symbols indexed for %s", root)
	}

	edges := staticEdges(pkgs, root, local)

	if a.opts.ResolveDynamic {
		seen := make(map[[2]core.SymbolID]bool, len(edges))
		for _, e := range edges {
			seen[[2]core.SymbolID{e.Caller, e.Callee}] = true
		}
		edges = append(edges, dynamicEdges(pkgs, local, seen)...)
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Caller != edges[j].Caller {
			return edges[i].Caller < edges[j].Caller
		}
		if edges[i].Callee != edges[j].Callee {
			return edges[i].Callee < edges[j].Callee
		}
		return edges[i].Line < edges[j].Line
	})
	return edges, importEdges(pkgs), nil
}

// localSymbols is the set of symbols defined in this repository. Edges leaving
// it are dropped: a reading path should not send someone to strconv.Atoi, and
// stdlib nodes would swamp centrality if left in the graph.
func (a *Adapter) localSymbols(ctx context.Context, root string) map[core.SymbolID]bool {
	syms, err := a.Symbols(ctx, root)
	if err != nil {
		return nil
	}
	set := make(map[core.SymbolID]bool, len(syms))
	for _, s := range syms {
		set[s.ID] = true
	}
	return set
}

// ---------------------------------------------------------------- static ---

func staticEdges(pkgs []*packages.Package, root string, local map[core.SymbolID]bool) []core.CallEdge {
	var out []core.CallEdge

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			rel, ok := relPath(root, pkg.Fset, file)
			if !ok {
				continue
			}
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				obj, ok := pkg.TypesInfo.Defs[fd.Name].(*types.Func)
				if !ok || obj == nil {
					continue
				}
				caller := symbolID(obj)
				if !local[caller] {
					continue
				}
				out = append(out, callsIn(pkg, fd.Body, caller, rel, local)...)
			}
		}
	}
	return out
}

// callsIn walks a function body and records every statically resolved call.
//
// Function literals are attributed to the enclosing named function rather than
// getting their own node. From a reading standpoint a closure is part of the
// function that defines it — you cannot open one on its own — and giving each
// an anonymous node would fragment both the graph and anyone's probe history.
func callsIn(pkg *packages.Package, body ast.Node, caller core.SymbolID, file string, local map[core.SymbolID]bool) []core.CallEdge {
	var out []core.CallEdge

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		obj := resolveCallee(pkg.TypesInfo, call)
		if obj == nil {
			return true
		}
		callee := symbolID(obj)
		if !local[callee] || callee == caller {
			return true // external, or direct recursion, which teaches nothing
		}
		out = append(out, core.CallEdge{
			Caller: caller,
			Callee: callee,
			Kind:   core.EdgeStatic,
			File:   file,
			Line:   line(pkg.Fset, call.Lparen),
		})
		return true
	})
	return out
}

// resolveCallee returns the function a call site provably targets, or nil when
// the target is not statically knowable — an interface method, a call through
// a func-typed variable or struct field, a builtin, or a type conversion.
func resolveCallee(info *types.Info, call *ast.CallExpr) *types.Func {
	fun := ast.Unparen(call.Fun)

	// Strip generic instantiation: Map[string, int](xs) targets Map.
	switch idx := fun.(type) {
	case *ast.IndexExpr:
		fun = ast.Unparen(idx.X)
	case *ast.IndexListExpr:
		fun = ast.Unparen(idx.X)
	}

	// A conversion, T(x), is not a call to anything.
	if tv, ok := info.Types[fun]; ok && tv.IsType() {
		return nil
	}

	switch f := fun.(type) {
	case *ast.Ident:
		fn, _ := info.Uses[f].(*types.Func)
		return concrete(fn)

	case *ast.SelectorExpr:
		if sel, ok := info.Selections[f]; ok {
			switch sel.Kind() {
			case types.MethodVal, types.MethodExpr:
				fn, _ := sel.Obj().(*types.Func)
				// A method on an interface value is dispatched at runtime; the
				// dynamic pass owns it.
				if isInterface(sel.Recv()) {
					return nil
				}
				return concrete(fn)
			default:
				return nil // a func-typed struct field
			}
		}
		// A qualified identifier: pkg.Func.
		fn, _ := info.Uses[f.Sel].(*types.Func)
		return concrete(fn)
	}
	return nil
}

// concrete filters out functions with no package: builtins and universe-scope
// objects, which are not part of anyone's codebase.
func concrete(fn *types.Func) *types.Func {
	if fn == nil || fn.Pkg() == nil {
		return nil
	}
	return fn
}

func isInterface(t types.Type) bool {
	_, ok := types.Unalias(t).Underlying().(*types.Interface)
	return ok
}

// --------------------------------------------------------------- dynamic ---

// dynamicEdges resolves interface dispatch with class hierarchy analysis.
//
// SSA is built for the repository's own packages only. Dependencies are
// created from type information without bodies, which keeps memory bounded and
// costs nothing that matters here: the implementations worth resolving are the
// ones in this repo, and edges into a dependency's internals are not something
// a reading path would ever surface.
func dynamicEdges(pkgs []*packages.Package, local map[core.SymbolID]bool, seen map[[2]core.SymbolID]bool) (out []core.CallEdge) {
	// SSA construction can panic on input that type-checked but is malformed
	// in ways the builder does not expect. A failed dynamic pass costs
	// precision on a signal that never grades anything; killing the whole
	// index over it would cost everything.
	defer func() {
		if r := recover(); r != nil {
			out = nil
		}
	}()

	prog, _ := ssautil.Packages(pkgs, ssa.InstantiateGenerics)
	if prog == nil {
		return nil
	}
	prog.Build()

	cg := cha.CallGraph(prog)
	if cg == nil {
		return nil
	}
	cg.DeleteSyntheticNodes()

	err := callgraph.GraphVisitEdges(cg, func(e *callgraph.Edge) error {
		// Only interface dispatch. Everything else the static pass already
		// resolved, with better provenance.
		if e.Site == nil || !e.Site.Common().IsInvoke() {
			return nil
		}
		caller, callee := ssaSymbol(e.Caller.Func), ssaSymbol(e.Callee.Func)
		if caller == "" || callee == "" || caller == callee {
			return nil
		}
		if !local[caller] || !local[callee] {
			return nil
		}
		key := [2]core.SymbolID{caller, callee}
		if seen[key] {
			return nil
		}
		seen[key] = true

		pos := prog.Fset.Position(e.Site.Pos())
		out = append(out, core.CallEdge{
			Caller: caller, Callee: callee, Kind: core.EdgeDynamic, Line: pos.Line,
		})
		return nil
	})
	if err != nil {
		return nil
	}
	return out
}

// ssaSymbol maps an SSA function to its symbol, walking out of closures to the
// named function that encloses them. Wrappers and thunks have no object and no
// parent, and correctly resolve to nothing.
func ssaSymbol(f *ssa.Function) core.SymbolID {
	for f != nil {
		if obj := f.Object(); obj != nil {
			return symbolID(obj)
		}
		f = f.Parent()
	}
	return ""
}

// --------------------------------------------------------------- imports ---

// importEdges returns package-level dependencies within the repository. These
// are the static-dependency side of the hidden-coupling signal: two files that
// change together and have no import path between them are the interesting
// case.
func importEdges(pkgs []*packages.Package) []core.ImportEdge {
	known := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		known[pkgPath(p)] = true
	}

	seen := make(map[core.ImportEdge]bool)
	var out []core.ImportEdge
	for _, p := range pkgs {
		from := pkgPath(p)
		for path := range p.Imports {
			if !known[path] || path == from {
				continue
			}
			e := core.ImportEdge{From: from, To: path}
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}
