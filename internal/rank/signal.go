// Package rank turns an index into an ordered reading path.
//
// This is the part the product lives or dies on. If the top ten items do not
// match what a senior engineer would tell a new hire to read, nothing
// downstream matters — not the probes, not the progress view, not the
// extension. Everything here is therefore built to be argued with: each signal
// is separable, individually inspectable, and carries its own explanation of
// why an item is on the list.
package rank

import (
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
)

// Signal names one input to the score.
type Signal string

const (
	// SignalCentrality is reverse PageRank over the call graph: high fan-in
	// means you cannot avoid it.
	SignalCentrality Signal = "centrality"
	// SignalChurn is commits touching the file in the last twelve months: hot
	// code is code you will be asked to change.
	SignalChurn Signal = "churn"
	// SignalCoupling is co-change minus static dependency — the differentiator.
	SignalCoupling Signal = "hidden_coupling"
	// SignalFixDensity is the share of commits that read as corrective: where
	// the tricky semantics live.
	SignalFixDensity Signal = "fix_density"
	// SignalOrphaning is code whose authors have gone quiet: nobody left to ask.
	SignalOrphaning Signal = "orphaning"
	// SignalAIDensity is machine-authored code that may never have been
	// understood by anyone.
	SignalAIDensity Signal = "ai_density"
	// SignalProximity is graph distance from a user-named area. Optional.
	SignalProximity Signal = "task_proximity"
)

// AllSignals lists the seven, in the spec's order.
var AllSignals = []Signal{
	SignalCentrality,
	SignalChurn,
	SignalCoupling,
	SignalFixDensity,
	SignalOrphaning,
	SignalAIDensity,
	SignalProximity,
}

// Scores holds one signal's value per symbol. Values are raw until
// normalization; afterwards they are in [0,1].
type Scores map[core.SymbolID]float64

// Params carry the inputs a signal may need beyond the index itself.
type Params struct {
	// Task seeds the proximity signal: the area the user says they are working
	// in. Empty means no task scope, and proximity contributes nothing.
	Task []core.SymbolID

	// Now is the reference time for every decay and window. Fixed once per run
	// so results are reproducible, and settable so the backtest can stand at
	// an arbitrary date.
	Now time.Time

	// ChurnWindow bounds the history signals. Twelve months is the spec's.
	ChurnWindow time.Duration
}

// DefaultParams returns the settings a plain ranking run uses.
func DefaultParams() Params {
	return Params{
		Now:         time.Now().UTC(),
		ChurnWindow: 365 * 24 * time.Hour,
	}
}

// Computer produces one signal.
//
// Each is a separate type with a separate test rather than a branch in one
// big function, because the only way to answer "is the ranking any good" is to
// be able to turn one signal off and look at what changed.
type Computer interface {
	Signal() Signal
	// Compute returns raw, unnormalized values. Symbols the signal has nothing
	// to say about are omitted rather than set to zero, so "no evidence" stays
	// distinguishable from "evidence of nothing".
	Compute(v *index.View, p Params) Scores
}

// spreadFileToSymbols assigns a per-file value to every symbol in that file.
//
// Four of the seven signals are file-grained because git is: churn, fix
// density, orphaning and AI density are all facts about a file's history, and
// attributing them to a line range would mean tracking symbol boundaries
// across renames and rewrites. Inheritance is the honest approximation, and it
// costs the ability to say "this function is hot, its neighbor is not" —
// which git, at this granularity, genuinely cannot tell us.
func spreadFileToSymbols(v *index.View, byFile map[string]float64) Scores {
	out := make(Scores, len(v.Symbols))
	for id, sym := range v.Symbols {
		if val, ok := byFile[sym.File]; ok && val != 0 {
			out[id] = val
		}
	}
	return out
}
