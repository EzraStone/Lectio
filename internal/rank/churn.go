package rank

import (
	"math"
	"time"

	"github.com/EzraStone/Lectio/internal/index"
)

// Churn scores a file by how much it has been changed recently.
//
// Source: commits touching the file in the last twelve months. Why it matters:
// hot code is code you will be asked to change, and being asked to change
// something you have never read is the situation this whole tool exists to
// prevent.
type Churn struct{}

// Signal implements Computer.
func (Churn) Signal() Signal { return SignalChurn }

// Compute counts windowed commits per file, weighting recent ones more.
//
// A flat count within the window has a cliff at the boundary: a file with
// eleven commits eleven months ago outranks one with ten commits last week,
// and then next month the first drops to zero. Exponential recency weighting
// with a six-month half-life removes the cliff and matches what the signal is
// actually claiming — that recent activity predicts the work you are about to
// be handed.
func (Churn) Compute(v *index.View, p Params) Scores {
	window := p.ChurnWindow
	if window <= 0 {
		window = 365 * 24 * time.Hour
	}
	cutoff := p.Now.Add(-window)
	const halfLife = 182 * 24 * time.Hour

	byFile := make(map[string]float64)
	for _, c := range v.Commits {
		if c.When.Before(cutoff) {
			continue
		}
		age := p.Now.Sub(c.When)
		if age < 0 {
			age = 0
		}
		weight := math.Exp2(-age.Hours() / halfLife.Hours())

		for _, f := range c.Files {
			byFile[f.Path] += weight
			// A rename carries the file's history forward. Without this, moving
			// a file resets its churn to zero and the most-reorganized code in
			// the repo — which is usually the code under active work — looks
			// untouched.
			if f.Renamed != "" {
				byFile[f.Renamed] += weight
			}
		}
	}
	return spreadFileToSymbols(v, byFile)
}

// FixDensity scores a file by the share of its recent commits that read as
// corrective.
//
// Source: commits matching fix / bug / revert. Why it matters: this is where
// the tricky semantics live. Code that needed fixing repeatedly is code whose
// behavior surprised someone who thought they understood it, which makes it
// exactly what a newcomer should read before they are the next one surprised.
type FixDensity struct{}

// Signal implements Computer.
func (FixDensity) Signal() Signal { return SignalFixDensity }

// Compute returns fix commits per file, scaled by the share they represent.
//
// Density rather than raw count, because a file with four fixes out of five
// commits is a different object from one with four fixes out of two hundred.
// The count still matters — one fix out of one commit is not evidence of
// anything — so the two are multiplied: a file needs both a meaningful number
// of fixes and a meaningful proportion to score.
func (FixDensity) Compute(v *index.View, p Params) Scores {
	window := p.ChurnWindow
	if window <= 0 {
		window = 365 * 24 * time.Hour
	}
	cutoff := p.Now.Add(-window)

	type tally struct{ fixes, total int }
	byFile := make(map[string]*tally)

	for _, c := range v.Commits {
		if c.When.Before(cutoff) {
			continue
		}
		isFix := c.IsFix() || c.IsRevert()
		for _, f := range c.Files {
			t := byFile[f.Path]
			if t == nil {
				t = &tally{}
				byFile[f.Path] = t
			}
			t.total++
			if isFix {
				t.fixes++
			}
		}
	}

	scores := make(map[string]float64, len(byFile))
	for path, t := range byFile {
		if t.fixes == 0 || t.total == 0 {
			continue
		}
		share := float64(t.fixes) / float64(t.total)
		// log1p on the count so a file with forty fixes does not outrank one
		// with twelve by the same factor it outranks one with two.
		scores[path] = share * math.Log1p(float64(t.fixes))
	}
	return spreadFileToSymbols(v, scores)
}
