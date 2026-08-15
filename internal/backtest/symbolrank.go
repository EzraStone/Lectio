package backtest

import (
	"sort"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
)

// SymbolStrategy predicts which declarations a newcomer will touch.
//
// Separate from Baseline rather than an extension of it, because the two
// answer different questions and a type that returned either would let a
// caller compare a file list against a symbol list without noticing.
type SymbolStrategy interface {
	Name() string
	RankSymbols(v *index.View, p rank.Params) []core.SymbolID
}

// SymbolBaselines are the four spec baselines, restated in symbols.
//
// This restatement is a judgment call and it was fixed before any symbol-level
// number was seen, because choosing it afterwards is how a gate gets argued
// past. Each baseline keeps its original signal and inherits it to symbols:
// "the biggest files" becomes "the symbols in the biggest files", not "the
// biggest symbols".
//
// That is the faithful translation. The file-level baseline said *read this
// file*, and the symbol-level question is which declarations that recommends —
// which is every declaration in it, in file order. Ranking by symbol length
// instead would be a different heuristic that no earlier run tested, and it
// would quietly replace the baseline that actually beat lectio.
func SymbolBaselines() []SymbolStrategy {
	out := make([]SymbolStrategy, 0, len(Baselines()))
	for _, b := range Baselines() {
		out = append(out, FromFileBaseline{Baseline: b})
	}
	return out
}

// SymbolControls are the non-gating controls, restated the same way.
func SymbolControls() []SymbolStrategy {
	out := make([]SymbolStrategy, 0, len(Controls())+1)
	for _, c := range Controls() {
		out = append(out, FromFileBaseline{Baseline: c})
	}
	// One control only symbol granularity can express: if the biggest
	// declarations predict as well as the ranking, then size followed the
	// measure down from files to symbols and nothing was gained by moving.
	out = append(out, LargestSymbols{})
	return out
}

// FromFileBaseline lifts a file-level strategy to symbols by expanding each
// file into the declarations it contains.
type FromFileBaseline struct{ Baseline Baseline }

// Name implements SymbolStrategy.
func (f FromFileBaseline) Name() string { return f.Baseline.Name() }

// RankSymbols implements SymbolStrategy.
//
// Within a file, declarations come in source order. A file-level baseline
// expresses no preference among them, and source order is the order someone
// opening the file would meet them — the least additional claim available.
func (f FromFileBaseline) RankSymbols(v *index.View, p rank.Params) []core.SymbolID {
	inFile := make(map[string][]core.Symbol, 64)
	for _, sym := range v.Readable() {
		inFile[sym.File] = append(inFile[sym.File], sym)
	}
	for file := range inFile {
		syms := inFile[file]
		sort.Slice(syms, func(i, j int) bool {
			if syms[i].StartLine != syms[j].StartLine {
				return syms[i].StartLine < syms[j].StartLine
			}
			return syms[i].ID < syms[j].ID
		})
	}

	var out []core.SymbolID
	for _, file := range f.Baseline.RankFiles(v, p) {
		for _, sym := range inFile[file] {
			out = append(out, sym.ID)
		}
	}
	return out
}

// LargestSymbols ranks declarations by their own line span.
//
// A control, not a baseline. It asks whether size simply follows the measure
// down from files to symbols: if the longest functions predict as well as
// seven signals do, then changing granularity bought nothing and the size
// prior was never really about files.
type LargestSymbols struct{}

// Name implements SymbolStrategy.
func (LargestSymbols) Name() string { return "largest symbols" }

// RankSymbols implements SymbolStrategy.
func (LargestSymbols) RankSymbols(v *index.View, _ rank.Params) []core.SymbolID {
	syms := v.Readable()
	sort.Slice(syms, func(i, j int) bool {
		if syms[i].Lines() != syms[j].Lines() {
			return syms[i].Lines() > syms[j].Lines()
		}
		return syms[i].ID < syms[j].ID
	})

	out := make([]core.SymbolID, 0, len(syms))
	for _, s := range syms {
		out = append(out, s.ID)
	}
	return out
}

// LectioSymbols is the ranking with no collapse at all.
//
// This is what the ranker produces natively. Every file-level run so far has
// been scoring a projection of this list, and the collapse rule that produced
// the projection was itself worth several points — so this is the first
// measurement of the ranking as it actually is.
type LectioSymbols struct {
	Label   string
	Weights rank.Weights
}

// Name implements SymbolStrategy.
func (l LectioSymbols) Name() string {
	if l.Label != "" {
		return l.Label
	}
	return DefaultVariant
}

// RankSymbols implements SymbolStrategy.
func (l LectioSymbols) RankSymbols(v *index.View, p rank.Params) []core.SymbolID {
	w := l.Weights
	if w == nil {
		w = rank.DefaultWeights()
	}

	res := rank.Rank(v, p, w)
	out := make([]core.SymbolID, 0, len(res.Items))
	for _, item := range res.Items {
		// A zero score means no signal had anything to say. Padding the list
		// with those would fill a top ten with declarations the tool has no
		// reason to name — and at symbol granularity there are thousands of
		// them, so this matters more here than it did for files.
		if item.Score <= 0 {
			break
		}
		out = append(out, item.Symbol.ID)
	}
	return out
}

// symbolIDs converts for the metric functions, which work in strings.
func symbolIDs(ids []core.SymbolID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}
