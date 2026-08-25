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

// VariantKind names which experiment a run performed.
//
// Recorded rather than inferred. The two sets overlap by name — a candidate is
// called "lectio −orphaning" and so is an ablation row — and guessing from
// names produced an ablation table for a run that ablated nothing.
type VariantKind string

const (
	// VariantsAblation is the leave-one-out set: what is each signal worth
	// inside the current weighting?
	VariantsAblation VariantKind = "ablation"
	// VariantsCandidates is the named hypotheses: which weighting to keep?
	VariantsCandidates VariantKind = "candidates"
)

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
	// MatchedWithout and MatchedDelta are the same two numbers on size-matched
	// pairs.
	//
	// Worth reading before the precision pair, on any run that has them. A
	// signal can lift precision purely by preferring larger candidates, and the
	// first four runs are what that looks like; on matched pairs it cannot, so
	// a positive delta there is information the pairing did not already
	// remove.
	MatchedWithout float64
	MatchedDelta   float64
}

// Contributions reads an ablation report back into per-signal effects.
//
// Returns nil when the report was not an ablation run, which the report states
// rather than this function guessing from variant names. The candidate
// weightings include one called "lectio −orphaning" — indistinguishable from
// an ablation row by name alone, and reading it as one produced a table headed
// "what each signal is worth" for a run that ablated nothing.
func Contributions(rep Report) []Contribution {
	if rep.Variants != "" && rep.Variants != VariantsAblation {
		return nil
	}

	scores := make(map[string]float64, len(rep.Aggregates))
	matched := make(map[string]float64, len(rep.Aggregates))
	for _, a := range rep.Aggregates {
		scores[a.Strategy] = a.PrecisionA
		matched[a.Strategy] = a.MatchedA
	}
	base, ok := scores[DefaultVariant]
	if !ok {
		return nil
	}
	matchedBase := matched[DefaultVariant]

	var out []Contribution
	for _, sig := range rank.AllSignals {
		name := fmt.Sprintf("lectio −%s", sig)
		without, ok := scores[name]
		if !ok {
			continue
		}
		out = append(out, Contribution{
			Signal:         sig,
			Without:        without,
			Delta:          base - without,
			MatchedWithout: matched[name],
			MatchedDelta:   matchedBase - matched[name],
		})
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
