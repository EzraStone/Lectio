package backtest

import "github.com/EzraStone/Lectio/internal/index"

// SymbolSizes measures every readable declaration, in lines spanned.
//
// The symbol-level counterpart of FileSizes, and it exists for the same
// reason: once a size heuristic wins, every argument about the result becomes
// an argument about one definition of size, so there had better be exactly one.
//
// Stratification itself is shared with the file targets. scoreStrata does not
// care whether it is banding files or declarations — the bands, the halved
// cutoff, the skip on absent ground truth and the spread caveat all keep their
// meaning — so this supplies the sizes and the arithmetic stays in one place.
func SymbolSizes(v *index.View) map[string]int {
	out := make(map[string]int, len(v.Symbols))
	for _, sym := range v.Readable() {
		out[string(sym.ID)] = sym.Lines()
	}
	return out
}
