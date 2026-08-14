package backtest

import (
	"fmt"

	"github.com/EzraStone/Lectio/internal/rank"
)

// Variant is one weighting scored alongside the baselines.
//
// Variants exist so an ablation costs one index per case rather than one per
// weighting. Indexing a rewound revision is seconds to minutes; scoring a
// ranking against an already-built view is microseconds. Re-running the whole
// backtest per variant would multiply the expensive half by eight to learn
// something the cheap half already knows.
type Variant struct {
	Name    string
	Weights rank.Weights
}

// DefaultVariant is the shipped weighting, named so it can be found in a
// report and compared against.
const DefaultVariant = "lectio"

// Variants returns the single default weighting.
func Variants() []Variant {
	return []Variant{{Name: DefaultVariant, Weights: rank.DefaultWeights()}}
}

// Ablations returns the default weighting plus one variant per signal with
// that signal disabled.
//
// This is what makes a FAIL actionable. "Lectio scored 0.31" says nothing you
// can act on; "lectio scored 0.31, and 0.44 with churn disabled" says churn is
// actively hurting and points at a fix. Without it the only response to a
// failing gate is to guess at weights, which is how a proxy gets optimized
// instead of a goal.
//
// Task proximity is excluded. A backtest names no task, so the signal never
// fires and ablating it would add a column identical to the baseline in every
// row — noise in a table whose whole job is making differences visible.
func Ablations() []Variant {
	out := Variants()
	for _, sig := range rank.AllSignals {
		if sig == rank.SignalProximity {
			continue
		}
		w := rank.DefaultWeights()
		w[sig] = 0
		out = append(out, Variant{
			Name:    fmt.Sprintf("lectio −%s", sig),
			Weights: w,
		})
	}
	return out
}

// Contribution is one signal's measured effect, derived from an ablation run.
type Contribution struct {
	Signal rank.Signal
	// Without is mean precision@K with the signal disabled.
	Without float64
	// Delta is the default score minus Without. Positive means the signal
	// helps; negative means removing it would improve the ranking.
	Delta float64
}

// Contributions reads an ablation report back into per-signal effects.
//
// Returns nil when the report holds no ablation variants, so a caller can tell
// "this was a plain run" from "every signal measured zero".
func Contributions(rep Report) []Contribution {
	scores := make(map[string]float64, len(rep.Aggregates))
	for _, a := range rep.Aggregates {
		scores[a.Strategy] = a.PrecisionA
	}
	base, ok := scores[DefaultVariant]
	if !ok {
		return nil
	}

	var out []Contribution
	for _, sig := range rank.AllSignals {
		name := fmt.Sprintf("lectio −%s", sig)
		without, ok := scores[name]
		if !ok {
			continue
		}
		out = append(out, Contribution{Signal: sig, Without: without, Delta: base - without})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Harmful lists signals whose removal improved the score.
//
// The spec's named failure mode is the ranking becoming a churn proxy. A
// signal with a negative delta is direct evidence of that happening, and it is
// worth surfacing separately rather than leaving someone to scan a table of
// near-identical numbers for a minus sign.
func Harmful(cs []Contribution) []Contribution {
	var out []Contribution
	for _, c := range cs {
		if c.Delta < 0 {
			out = append(out, c)
		}
	}
	return out
}
