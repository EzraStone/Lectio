package rank

import (
	"sort"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
)

// Weights control how much each signal contributes.
//
// These numbers are a hypothesis, not a result. Nothing here has been fit to
// anything — Gate A has not been run — and the honest description is "a
// starting point chosen so the differentiator is not drowned out by the two
// signals anyone could have computed". Whatever the backtest says should
// replace them.
type Weights map[Signal]float64

// DefaultWeights returns the starting point.
//
// The shape of the guess:
//
// Centrality and churn are the floor. They are obvious, they are right, and
// weighting them too heavily produces a list of your biggest files — the exact
// failure the spec names as most likely to kill this. Together they are under
// half the score.
//
// Hidden coupling carries the most weight of any single signal because it is
// the only one that produces the reaction the product needs: someone stopping
// to say they would not have thought of that.
//
// AI density is last. Its evidence is opt-in markers, so its recall is capped
// by a repo's commit conventions, and a signal that measures team habits as
// much as code deserves the smallest vote.
func DefaultWeights() Weights {
	return Weights{
		SignalCentrality: 0.20,
		SignalChurn:      0.15,
		SignalCoupling:   0.25,
		SignalFixDensity: 0.15,
		SignalOrphaning:  0.10,
		SignalAIDensity:  0.05,
		SignalProximity:  0.10,
	}
}

// Computers returns the seven signal computers in spec order.
func Computers() []Computer {
	return []Computer{
		Centrality{},
		Churn{},
		HiddenCoupling{},
		FixDensity{},
		Orphaning{},
		AIDensity{},
		Proximity{},
	}
}

// Normalizer is implemented by signals whose raw output is already in [0,1]
// on an absolute scale and must not be percentile-ranked.
type Normalizer interface {
	Normalized() bool
}

// Item is one scored symbol.
type Item struct {
	Symbol core.Symbol
	Score  float64
	// Contributions holds each signal's normalized value, so any ranking can
	// be taken apart and argued with. This is the whole reason signals are
	// separate types.
	Contributions map[Signal]float64
	// Tier is the relevance bucket, 1 being most relevant.
	Tier int
	// Depth is the symbol's dependency depth, used for ordering within a tier.
	Depth int
	// Rationale is the one-line explanation shown next to the item.
	Rationale string
}

// Top returns the largest contributing signal and its normalized value.
func (i Item) Top() (Signal, float64) {
	var best Signal
	var bestVal float64
	for _, s := range AllSignals { // fixed order, so ties resolve the same way twice
		if v := i.Contributions[s]; v > bestVal {
			best, bestVal = s, v
		}
	}
	return best, bestVal
}

// Result is a complete ranking.
type Result struct {
	Items []Item
	// Active lists the signals that had anything to say about this repo. A
	// signal with no data is reported rather than silently counted as zero,
	// because "we found no AI markers" and "this code is not AI-written" are
	// different claims.
	Active []Signal
	// Silent lists signals with no data.
	Silent []Signal
	// TaskMatch describes what a task query resolved to, if anything.
	TaskMatch string
}

// Rank scores every readable symbol.
func Rank(v *index.View, p Params, w Weights) *Result {
	if w == nil {
		w = DefaultWeights()
	}

	normalized := make(map[Signal]Scores, len(AllSignals))
	res := &Result{}

	for _, c := range Computers() {
		sig := c.Signal()
		if w[sig] <= 0 {
			continue
		}
		raw := c.Compute(v, p)
		if len(raw) == 0 {
			res.Silent = append(res.Silent, sig)
			continue
		}
		if n, ok := c.(Normalizer); ok && n.Normalized() {
			normalized[sig] = raw
		} else {
			normalized[sig] = Normalize(raw)
		}
		res.Active = append(res.Active, sig)
	}

	// Weights are renormalized over the signals that actually fired. Without
	// this, a repo with no git history scores every symbol near zero and the
	// ordering survives but the numbers become meaningless — and they are shown
	// to people.
	var totalWeight float64
	for _, sig := range res.Active {
		totalWeight += w[sig]
	}
	if totalWeight == 0 {
		return res
	}

	readable := v.Readable()
	res.Items = make([]Item, 0, len(readable))

	for _, sym := range readable {
		item := Item{Symbol: sym, Contributions: make(map[Signal]float64, len(res.Active))}
		var score float64
		for _, sig := range res.Active {
			val := normalized[sig][sym.ID]
			if val == 0 {
				continue
			}
			item.Contributions[sig] = val
			score += w[sig] * val
		}
		item.Score = score / totalWeight

		// Understanding already demonstrated reduces the reason to read
		// something again. This is the only place the comprehension score
		// touches output, and it moves the order rather than being displayed.
		if f, ok := p.Familiarity[sym.ID]; ok && f > 0 {
			item.Score *= 1 - clamp01(f)
		}

		res.Items = append(res.Items, item)
	}

	depth := Depth(v)
	for i := range res.Items {
		res.Items[i].Depth = depth[res.Items[i].Symbol.ID]
	}

	sort.Slice(res.Items, func(a, b int) bool {
		x, y := res.Items[a], res.Items[b]
		if x.Score != y.Score {
			return x.Score > y.Score
		}
		return x.Symbol.ID < y.Symbol.ID // deterministic ties
	})
	return res
}

// Select returns the n highest-scoring items, dropping anything that scored
// nothing at all.
//
// A symbol with a zero score is one no signal had anything to say about.
// Padding a top-ten list with those to reach ten would be filling a
// recommendation with items the tool has no reason to recommend.
func (r *Result) Select(n int) []Item {
	out := make([]Item, 0, n)
	for _, item := range r.Items {
		if item.Score <= 0 {
			break
		}
		out = append(out, item)
		if n > 0 && len(out) >= n {
			break
		}
	}
	return out
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
