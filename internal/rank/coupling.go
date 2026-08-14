package rank

import (
	"path"
	"sort"
	"time"

	"github.com/EzraStone/Lectio/internal/index"
)

// HiddenCoupling finds files that change together without depending on each
// other.
//
// This is the differentiator. Centrality and churn are computable by anyone
// and roughly match intuition; this one surfaces what a senior engineer knows
// and cannot articulate — touch the serializer, update the mobile schema. It
// is the output meant to make someone ask how the tool knew.
//
// The subtraction is the whole idea. Files that co-change *and* import each
// other are not surprising: the compiler already tells you, and a reader
// following imports will find them. The interesting pairs are the ones with no
// static path between them, where the only thing linking them is that
// forgetting the second breaks the first.
type HiddenCoupling struct {
	// MaxCommitSize ignores commits touching more than this many files.
	MaxCommitSize int
	// MinSupport is the number of shared commits below which a pair is treated
	// as coincidence.
	MinSupport int
}

// Signal implements Computer.
func (HiddenCoupling) Signal() Signal { return SignalCoupling }

// Defaults chosen for the failure modes each one prevents.
const (
	// A commit touching more than twenty files is a reformat, a license header
	// sweep, a generated-code refresh or a dependency bump. It produces up to
	// 190 pairs, none of them meaningful, and a handful of such commits will
	// otherwise dominate the entire signal — they generate pairs quadratically
	// while ordinary commits generate a few.
	defaultMaxCommitSize = 20

	// Two files that changed together twice changed together by coincidence.
	// Three is the point where the pattern starts being about the code.
	defaultMinSupport = 3
)

// Pair is one co-change relationship, kept for rationale text and for the
// second backtest.
type Pair struct {
	A, B string
	// Together is the number of commits touching both.
	Together int
	// OnlyA and OnlyB are commits touching one and not the other.
	OnlyA, OnlyB int
	// Strength is the Jaccard index: Together / (Together + OnlyA + OnlyB).
	Strength float64
	// Hidden reports whether the pair has no static path between it.
	Hidden bool
}

// Compute scores each file by how strongly it is hidden-coupled to others.
func (h HiddenCoupling) Compute(v *index.View, p Params) Scores {
	byFile := make(map[string]float64)
	for _, pair := range h.Pairs(v, p) {
		if !pair.Hidden {
			continue
		}
		// Sum rather than max: a file coupled to four unrelated things is a
		// worse trap than one coupled to a single thing, and a newcomer needs
		// to know about all four.
		byFile[pair.A] += pair.Strength
		byFile[pair.B] += pair.Strength
	}
	return spreadFileToSymbols(v, byFile)
}

// Pairs returns every co-changing file pair with enough support, marked with
// whether a static dependency explains it.
//
// Partners are not restricted to indexed source. The spec's own example —
// touch the serializer, update the mobile schema — is a pair where one side is
// not code this adapter would ever parse, and restricting to Go files would
// discard exactly the surprises worth surfacing.
func (h HiddenCoupling) Pairs(v *index.View, p Params) []Pair {
	maxCommit := h.MaxCommitSize
	if maxCommit <= 0 {
		maxCommit = defaultMaxCommitSize
	}
	minSupport := h.MinSupport
	if minSupport <= 0 {
		minSupport = defaultMinSupport
	}

	window := p.ChurnWindow
	if window <= 0 {
		window = 365 * 24 * time.Hour
	}
	cutoff := p.Now.Add(-window)

	indexed := indexedFiles(v)
	if len(indexed) == 0 {
		return nil
	}

	type pairKey struct{ a, b string }
	together := make(map[pairKey]int)
	touched := make(map[string]int)

	for _, c := range v.Commits {
		if c.When.Before(cutoff) {
			continue
		}
		paths := distinctPaths(c.Paths())
		if len(paths) < 2 || len(paths) > maxCommit {
			// A single-file commit contributes no pairs; an enormous one
			// contributes only noise.
			for _, f := range paths {
				touched[f]++
			}
			continue
		}
		for _, f := range paths {
			touched[f]++
		}
		for i := 0; i < len(paths); i++ {
			for j := i + 1; j < len(paths); j++ {
				// At least one side must be code we indexed, or the pair
				// cannot be attached to any symbol.
				if !indexed[paths[i]] && !indexed[paths[j]] {
					continue
				}
				together[pairKey{paths[i], paths[j]}]++
			}
		}
	}

	rel := newRelatedness(v)

	out := make([]Pair, 0, len(together))
	for k, n := range together {
		if n < minSupport {
			continue
		}
		onlyA, onlyB := touched[k.a]-n, touched[k.b]-n
		denom := n + onlyA + onlyB
		if denom <= 0 {
			continue
		}
		out = append(out, Pair{
			A: k.a, B: k.b,
			Together: n, OnlyA: onlyA, OnlyB: onlyB,
			Strength: float64(n) / float64(denom),
			Hidden:   !rel.related(k.a, k.b),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Strength != out[j].Strength {
			return out[i].Strength > out[j].Strength
		}
		if out[i].A != out[j].A {
			return out[i].A < out[j].A
		}
		return out[i].B < out[j].B
	})
	return out
}

// HiddenPairsFor returns the strongest hidden couplings involving a file, for
// rationale text.
func (h HiddenCoupling) HiddenPairsFor(v *index.View, p Params, file string, limit int) []Pair {
	var out []Pair
	for _, pair := range h.Pairs(v, p) {
		if !pair.Hidden {
			continue
		}
		if pair.A == file || pair.B == file {
			out = append(out, pair)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out
}

// relatedness answers whether a static path explains a co-change.
type relatedness struct {
	fileEdges map[[2]string]bool
	pkgOfFile map[string]string
	pkgEdges  map[[2]string]bool
}

func newRelatedness(v *index.View) *relatedness {
	r := &relatedness{
		fileEdges: make(map[[2]string]bool),
		pkgOfFile: make(map[string]string, len(v.Symbols)),
		pkgEdges:  make(map[[2]string]bool, len(v.Imports)*2),
	}

	fileOf := make(map[string]string, len(v.Symbols))
	for id, sym := range v.Symbols {
		fileOf[string(id)] = sym.File
		r.pkgOfFile[sym.File] = sym.Package
	}

	// A call edge between symbols in two files is a static path, whether or not
	// their packages import each other — same-package calls have no import.
	for i := 0; i < v.Calls.N(); i++ {
		from := fileOf[v.Calls.ID(i)]
		for _, j := range v.Calls.Out(i) {
			to := fileOf[v.Calls.ID(j)]
			if from != "" && to != "" && from != to {
				r.fileEdges[orderPair(from, to)] = true
			}
		}
	}

	for _, e := range v.Imports {
		r.pkgEdges[orderPair(e.From, e.To)] = true
	}
	return r
}

// related reports whether a static dependency, an import, or shared package
// membership explains why two files change together.
func (r *relatedness) related(a, b string) bool {
	if a == b {
		return true
	}
	if r.fileEdges[orderPair(a, b)] {
		return true
	}

	pa, pb := r.pkgOfFile[a], r.pkgOfFile[b]
	if pa != "" && pa == pb {
		// Same package. Files in one package change together constantly and a
		// reader meets them together anyway; there is no surprise to surface.
		return true
	}
	if pa != "" && pb != "" && r.pkgEdges[orderPair(pa, pb)] {
		return true
	}

	// Neither side is indexed source — a schema next to a template, say. With
	// no static structure to consult, directory adjacency is the only signal
	// available, and files in one directory are found together by anyone
	// looking.
	if pa == "" && pb == "" && path.Dir(a) == path.Dir(b) {
		return true
	}
	return false
}

func orderPair(a, b string) [2]string {
	if a > b {
		a, b = b, a
	}
	return [2]string{a, b}
}

func indexedFiles(v *index.View) map[string]bool {
	out := make(map[string]bool)
	for _, sym := range v.Symbols {
		if !sym.IsTest() {
			out[sym.File] = true
		}
	}
	return out
}

// distinctPaths deduplicates and sorts a commit's paths so pair keys are
// stable regardless of the order git listed them.
func distinctPaths(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	out := sorted[:1]
	for _, p := range sorted[1:] {
		if p != out[len(out)-1] {
			out = append(out, p)
		}
	}
	return out
}
