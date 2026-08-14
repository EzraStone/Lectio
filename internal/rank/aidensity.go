package rank

import (
	"math"
	"time"

	"github.com/EzraStone/Lectio/internal/index"
)

// AIDensity scores code that may never have been understood by anyone.
//
// Source: git-ai notes and machine co-author trailers, when present. Why it
// matters: code a person wrote was, at some point, code a person understood.
// Code a model wrote and a person approved may never have crossed that line —
// and that is the comprehension debt the whole product is aimed at.
//
// This signal has the weakest evidence base of the seven, by construction.
// Both of its sources are opt-in markers, so absence means "unknown", never
// "human", and a repository that records nothing scores zero everywhere rather
// than scoring low. It is weighted last for that reason and it is the first
// signal to disable when its input looks unreliable — a repo where one team
// uses a marked assistant and another does not is measuring team habits, not
// comprehension risk.
type AIDensity struct{}

// Signal implements Computer.
func (AIDensity) Signal() Signal { return SignalAIDensity }

// Compute returns the machine-authored share of each file's recent line
// additions, scaled by volume on the same reasoning as orphaning.
func (AIDensity) Compute(v *index.View, p Params) Scores {
	window := p.ChurnWindow
	if window <= 0 {
		window = 365 * 24 * time.Hour
	}
	cutoff := p.Now.Add(-window)

	type tally struct{ ai, total int }
	byFile := make(map[string]*tally)

	var anyMarked bool
	for _, c := range v.Commits {
		if c.When.Before(cutoff) {
			continue
		}
		if c.AIAssisted {
			anyMarked = true
		}
		for _, f := range c.Files {
			t := byFile[f.Path]
			if t == nil {
				t = &tally{}
				byFile[f.Path] = t
			}
			// Line counts rather than commit counts: one commit adding four
			// hundred generated lines is a different object from one adding
			// four, and the commit count cannot tell them apart.
			t.total += f.Added
			if c.AIAssisted {
				t.ai += f.Added
			}
		}
	}

	// No markers anywhere means the repository does not record this, not that
	// the repository is all hand-written. Returning nothing keeps the signal
	// out of the score instead of asserting a fact we do not have.
	if !anyMarked {
		return Scores{}
	}

	scores := make(map[string]float64, len(byFile))
	for path, t := range byFile {
		if t.total == 0 || t.ai == 0 {
			continue
		}
		share := float64(t.ai) / float64(t.total)
		scores[path] = share * math.Log1p(float64(t.ai))
	}
	return spreadFileToSymbols(v, scores)
}

// HasAIMarkers reports whether the history records machine authorship at all,
// so the CLI can say "not recorded in this repo" rather than showing a column
// of zeros that reads like a clean bill of health.
func HasAIMarkers(v *index.View) bool {
	for _, c := range v.Commits {
		if c.AIAssisted {
			return true
		}
	}
	return false
}
