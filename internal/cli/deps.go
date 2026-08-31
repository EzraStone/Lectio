package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/store"
)

func depsCmd() *Command {
	return &Command{
		Name:    "deps",
		Summary: "what breaks if you change this symbol",
		Run:     runDeps,
	}
}

// runDeps answers the blast-radius question directly.
//
// This is phase 0's done-when condition made visible: "what depends on X" is
// correct on a real repo. It is also the honest way to check the tool's own
// ground truth — before trusting a probe that grades you on this, you should
// be able to look at the same answer yourself.
func runDeps(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "deps", "deps [flags] <symbol> [repo]")
	var (
		depth   = fs.Int("depth", 2, "how many hops of dependents to follow; 0 for all")
		reverse = fs.Bool("uses", false, "show what this symbol depends on instead")
		all     = fs.Bool("include-dynamic", false, "include interface calls resolved by over-approximation")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Zero means "follow every hop", which is why this is not the same guard
	// the other counts get. Negative means nothing at all, and was silently
	// producing a bounded walk of some unstated depth.
	if *depth < 0 {
		return fmt.Errorf("--depth %d is not a number of hops; use 0 to follow all of them", *depth)
	}

	rest := fs.Args()
	if len(rest) == 0 {
		fs.Usage()
		return fmt.Errorf("name a symbol")
	}
	query := rest[0]

	root, err := repoArg(rest[1:])
	if err != nil {
		return err
	}

	s, err := store.OpenRepo(ctx, root)
	if err != nil {
		return err
	}
	defer s.Close()

	v, err := index.Load(ctx, s)
	if err != nil {
		return err
	}
	if len(v.Symbols) == 0 {
		return fmt.Errorf("no index for %s yet — run: lectio index %s", root, root)
	}

	target, err := resolveSymbol(v, query)
	if err != nil {
		return err
	}

	g := v.StaticCalls
	label := "static call edges only"
	if *all {
		g = v.Calls
		label = "including interface calls resolved by CHA, which over-approximates"
	}

	var found map[string]int
	var heading string
	if *reverse {
		found = dependenciesOf(g, target, *depth)
		heading = fmt.Sprintf("%s depends on", target.ID.Short())
	} else {
		found = dependentsOf(g, target, *depth)
		heading = fmt.Sprintf("Change %s and this is what breaks", target.ID.Short())
	}

	env.out("")
	env.out("%s", env.bold(heading))
	env.out("%s", env.dim(target.File+location(target.StartLine)))
	env.out("")

	// Production and test callers are separated rather than interleaved. Tests
	// genuinely are part of what breaks — the spec puts covering tests in the
	// ground truth — but a symbol with one production caller and nine test
	// functions reads as nine unrelated things unless they are split, and the
	// production caller is the one that changes what you do next.
	production, tests := partitionByTest(v, found)

	if len(production) == 0 && len(tests) == 0 {
		env.out("  nothing — no indexed symbol reaches it")
	}
	printHops(env, v, production)

	printTestFiles(env, v, tests)

	if covering := v.CoveringTests(target.ID); len(covering) > 0 && !*reverse {
		env.out("%s", env.dim("test binaries that would go red"))
		for _, t := range covering {
			env.out("  %s", strings.TrimSuffix(string(t), ".[tests]"))
		}
		env.out("")
	}

	env.note("%s %s", env.dim("ground truth:"), label)
	return nil
}

// resolveSymbol turns a user's query into exactly one symbol, or explains why
// it could not.
func resolveSymbol(v *index.View, query string) (core.Symbol, error) {
	if sym, ok := v.Symbols[core.SymbolID(query)]; ok {
		return sym, nil
	}

	// Case-sensitive first. Go identifiers are case-sensitive, so normalize and
	// Normalize are different symbols, and folding case would report a false
	// ambiguity between two things the language considers unrelated — and
	// between one the caller can reach and one they cannot.
	matches := matchSymbols(v, query, false)
	if len(matches) != 1 {
		if folded := matchSymbols(v, query, true); len(folded) > 0 {
			matches = folded
		}
	}

	lower := strings.ToLower(query)
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		// Fall through to a substring search, so a half-remembered name still
		// gets somewhere useful.
		var near []string
		for _, sym := range v.Symbols {
			if !sym.IsTest() && strings.Contains(strings.ToLower(sym.Name), lower) {
				near = append(near, sym.ID.Short())
			}
		}
		sort.Strings(near)
		if len(near) > 0 {
			if len(near) > 8 {
				near = near[:8]
			}
			return core.Symbol{}, fmt.Errorf("no symbol named %q — did you mean: %s", query, strings.Join(near, ", "))
		}
		return core.Symbol{}, fmt.Errorf("no symbol named %q in this index", query)
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, string(m.ID))
		}
		if len(names) > 8 {
			names = names[:8]
		}
		return core.Symbol{}, fmt.Errorf("%q is ambiguous — %s", query, strings.Join(names, ", "))
	}
}

// printTestFiles summarizes the test side by file rather than enumerating
// every test function.
//
// On a real repository the function-level list is overwhelming and useless:
// go-git's OpenFileIndex has one production dependent and twenty-eight test
// functions, so the thing a reader came for is buried under a wall of names
// that all say the same thing. What breaks, from a test's point of view, is a
// suite going red — which is the granularity a person would answer in, and the
// same reasoning that keeps individual test functions out of a graded probe.
func printTestFiles(env *Env, v *index.View, tests map[string]int) {
	if len(tests) == 0 {
		return
	}

	counts := map[string]int{}
	for id := range tests {
		counts[v.Symbols[core.SymbolID(id)].File]++
	}
	files := make([]string, 0, len(counts))
	for f := range counts {
		files = append(files, f)
	}
	sort.Strings(files)

	env.out("%s", env.dim(fmt.Sprintf("%s exercise it, across %s",
		plural(len(tests), "test"), plural(len(files), "file"))))
	for _, f := range files {
		env.out("  %-58s %s", f, env.dim(plural(counts[f], "test")))
	}
	env.out("")
}

// plural renders a count with its noun.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// partitionByTest splits a result set into production and test symbols.
func partitionByTest(v *index.View, found map[string]int) (production, tests map[string]int) {
	production = make(map[string]int, len(found))
	tests = make(map[string]int)
	for id, hop := range found {
		if v.Symbols[core.SymbolID(id)].IsTest() {
			tests[id] = hop
			continue
		}
		production[id] = hop
	}
	return production, tests
}

// printHops renders a result set grouped by distance.
func printHops(env *Env, v *index.View, found map[string]int) {
	byHop := map[int][]string{}
	for id, hop := range found {
		byHop[hop] = append(byHop[hop], id)
	}
	hops := make([]int, 0, len(byHop))
	for hop := range byHop {
		hops = append(hops, hop)
	}
	sort.Ints(hops)

	// One column width for the whole listing, derived from the longest name
	// present. A fixed pad breaks the moment a symbol exceeds it, and real
	// repositories are full of names that do.
	width := 0
	for id := range found {
		if n := len(core.SymbolID(id).Short()); n > width {
			width = n
		}
	}
	if width > 64 {
		width = 64
	}

	for _, hop := range hops {
		names := byHop[hop]
		sort.Strings(names)
		env.out("%s", env.dim(fmt.Sprintf("%s away", pluralHop(hop))))
		for _, id := range names {
			sym := v.Symbols[core.SymbolID(id)]
			// Pad before colouring: width verbs count the escape bytes.
			label := fmt.Sprintf("%-*s", width, core.SymbolID(id).Short())
			env.out("  %s  %s", env.accent(label), env.dim(sym.File))
		}
		env.out("")
	}
}

// matchSymbols returns production symbols whose short id or name equals query.
func matchSymbols(v *index.View, query string, fold bool) []core.Symbol {
	eq := func(a, b string) bool { return a == b }
	if fold {
		eq = strings.EqualFold
	}

	var out []core.Symbol
	for _, sym := range v.Symbols {
		if sym.IsTest() {
			continue
		}
		if eq(sym.ID.Short(), query) || eq(sym.Name, query) {
			out = append(out, sym)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func pluralHop(n int) string {
	if n == 1 {
		return "1 hop"
	}
	return fmt.Sprintf("%d hops", n)
}

// dependentsOf and dependenciesOf adapt the graph package to symbol ids.
func dependentsOf(g graphLike, target core.Symbol, depth int) map[string]int {
	return walk(g, target, depth, true)
}

func dependenciesOf(g graphLike, target core.Symbol, depth int) map[string]int {
	return walk(g, target, depth, false)
}

// graphLike is the narrow slice of *graph.Graph this file needs, which keeps
// the CLI from importing graph internals it has no business touching.
type graphLike interface {
	Index(string) (int, bool)
	ID(int) string
	Out(int) []int
	In(int) []int
	N() int
}

func walk(g graphLike, target core.Symbol, depth int, dependents bool) map[string]int {
	start, ok := g.Index(string(target.ID))
	if !ok {
		return nil
	}

	dist := map[int]int{start: 0}
	frontier := []int{start}
	for d := 1; len(frontier) > 0; d++ {
		if depth > 0 && d > depth {
			break
		}
		var next []int
		for _, v := range frontier {
			neighbors := g.Out(v)
			if dependents {
				neighbors = g.In(v)
			}
			for _, w := range neighbors {
				if _, seen := dist[w]; seen {
					continue
				}
				dist[w] = d
				next = append(next, w)
			}
		}
		frontier = next
	}

	out := make(map[string]int, len(dist))
	for i, d := range dist {
		if i != start {
			out[g.ID(i)] = d
		}
	}
	return out
}
