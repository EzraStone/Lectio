package backtest

import "github.com/EzraStone/Lectio/internal/rank"

// Candidates are the weightings worth testing after seven runs, written down
// before the corpus that will judge them has been fetched.
//
// The order matters and so does the timing. Every number this project has is
// from one corpus, and the run that produced these hypotheses is the same run
// that would judge them — which is how a result gets fitted to its own test
// set. Naming the candidates first, in code, with the reasoning attached, is
// what makes a later confirmation mean anything.
//
// What the seventh run found, on the only measure size cannot win:
//
//	most churned, 12mo        55.8%
//	lectio −orphaning         55.2%
//	lectio (all seven)        52.2%
//	largest files             49.9%
//
// So: churn carries a modest real effect, orphaning cancels most of it, and
// the other five are indistinguishable from nothing. These candidates are the
// three readings of that which differ in what they predict.
func Candidates() []Variant {
	return []Variant{
		{Name: DefaultVariant, Weights: rank.DefaultWeights()},

		// If churn is the whole effect, this matches lectio −orphaning and
		// beats the full ranking. If the other signals contribute anything at
		// all, this is worse than both.
		{Name: "churn only", Weights: rank.Weights{
			rank.SignalChurn: 1.0,
		}},

		// The observed best. Predicts that removing one signal recovers what
		// the ranking was losing, and nothing further is gained by removing
		// more.
		{Name: "lectio −orphaning", Weights: withoutSignals(rank.SignalOrphaning)},

		// If orphaning is not uniquely harmful but merely one of several dead
		// weights, dropping every signal that measured nothing should beat
		// dropping orphaning alone.
		{Name: "churn + centrality", Weights: rank.Weights{
			rank.SignalChurn:      0.6,
			rank.SignalCentrality: 0.4,
		}},

		// The whole history side against the whole structure side. Three of
		// the four strategies above chance on matched pairs were
		// history-derived, and none of the structural ones were.
		{Name: "history only", Weights: rank.Weights{
			rank.SignalChurn:      0.4,
			rank.SignalFixDensity: 0.3,
			rank.SignalOrphaning:  0.3,
		}},
	}
}

// withoutSignals returns the default weighting with some signals zeroed.
func withoutSignals(drop ...rank.Signal) rank.Weights {
	w := rank.DefaultWeights()
	for _, sig := range drop {
		w[sig] = 0
	}
	return w
}
