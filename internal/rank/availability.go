package rank

import "github.com/EzraStone/Lectio/internal/index"

// Computer's own contract says that symbols a signal has nothing to say about
// are omitted rather than zeroed, "so 'no evidence' stays distinguishable from
// 'evidence of nothing'". Result then collapses that distinction: a signal is
// Silent when it produced no scores, whether it had no input or ran across the
// whole repository and found nothing.
//
// The two are different claims and the reporting said so out loud —
// "'we found no AI markers' and 'this code is not AI-written' are different
// claims" — while printing "no data for: orphaning" at a repository with 286
// commits of authorship where the honest answer is that nothing is orphaned.
//
// This file supplies the missing half.

// Available is implemented by signals that can say whether the index carried
// what they need.
//
// Optional. A signal that does not implement it is assumed to have had its
// input, which is the conservative reading: an unexplained silence reported as
// a finding overstates the evidence by less than a real finding reported as a
// gap understates it.
type Available interface {
	// HasInput reports whether the index carries the data this signal reads.
	HasInput(v *index.View, p Params) bool
}

// HasInput reports whether the call graph has any edges to walk.
func (Centrality) HasInput(v *index.View, _ Params) bool { return v.Calls.N() > 0 }

// HasInput reports whether any history was read. All four history signals
// share this: churn, fix density, AI density and hidden coupling are facts
// about commits, and with none they cannot speak rather than having nothing
// to say.
func (Churn) HasInput(v *index.View, _ Params) bool { return len(v.Commits) > 0 }

// HasInput implements Available.
func (FixDensity) HasInput(v *index.View, _ Params) bool { return len(v.Commits) > 0 }

// HasInput implements Available.
func (AIDensity) HasInput(v *index.View, _ Params) bool { return len(v.Commits) > 0 }

// HasInput implements Available.
func (HiddenCoupling) HasInput(v *index.View, _ Params) bool { return len(v.Commits) > 0 }

// HasInput reports whether blame-derived ownership was recorded. Orphaning
// reads Authorship rather than Commits, and the difference is the whole point
// of this file: a repository with commits and no orphaned code produces an
// empty result from a signal that ran.
func (Orphaning) HasInput(v *index.View, _ Params) bool { return len(v.Authorship) > 0 }

// HasInput reports whether the user named a task. Proximity is optional by
// design, so its silence on a plain run is neither a gap nor a finding — but
// it is closer to a gap, since nothing was asked of it.
func (Proximity) HasInput(_ *index.View, p Params) bool { return len(p.Task) > 0 }

// classifySilence splits the signals that produced nothing into the ones that
// had no input and the ones that looked and found nothing.
func classifySilence(v *index.View, p Params, silent []Signal) (unavailable, empty []Signal) {
	byName := make(map[Signal]Computer, len(AllSignals))
	for _, c := range Computers() {
		byName[c.Signal()] = c
	}
	for _, sig := range silent {
		c, ok := byName[sig]
		if !ok {
			unavailable = append(unavailable, sig)
			continue
		}
		a, ok := c.(Available)
		if !ok || a.HasInput(v, p) {
			empty = append(empty, sig)
			continue
		}
		unavailable = append(unavailable, sig)
	}
	return unavailable, empty
}
