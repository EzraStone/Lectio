package rank

import (
	"math"
	"time"

	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/vcs"
)

// Orphaning scores code whose authors have gone quiet.
//
// Source: lines whose author has not committed anywhere in the repository for
// ninety days. Why it matters: nobody left to ask. Every other signal points
// at code that is hard; this one points at code where the usual escape hatch —
// finding whoever wrote it and asking — is closed.
//
// It is also the signal most likely to be read as an accusation, so it is
// worth being precise about what it measures. It says nothing about the
// quality of the code or of its author. It says the institutional memory
// around a file has left the building, which is a fact about the team, and it
// is a reason for a newcomer to read carefully rather than skim.
type Orphaning struct{}

// Signal implements Computer.
func (Orphaning) Signal() Signal { return SignalOrphaning }

// Compute returns the orphaned share of each file, scaled by volume.
//
// Share alone makes a twelve-line file written entirely by someone who left
// score as high as a two-thousand-line one. Volume alone makes every large
// file score. The product asks for both: a meaningful proportion of a
// meaningful amount of code.
func (Orphaning) Compute(v *index.View, p Params) Scores {
	if len(v.Authorship) == 0 {
		return Scores{}
	}

	type tally struct{ orphaned, total int }
	byFile := make(map[string]*tally, len(v.Authorship))

	for _, a := range v.Authorship {
		t := byFile[a.Path]
		if t == nil {
			t = &tally{}
			byFile[a.Path] = t
		}
		t.total += a.Lines
		if vcs.Orphaned(a.LastActive, p.Now) {
			t.orphaned += a.Lines
		}
	}

	scores := make(map[string]float64, len(byFile))
	for path, t := range byFile {
		if t.total == 0 || t.orphaned == 0 {
			continue
		}
		share := float64(t.orphaned) / float64(t.total)
		scores[path] = share * math.Log1p(float64(t.orphaned))
	}
	return spreadFileToSymbols(v, scores)
}

// OrphanedShare returns the orphaned fraction per file, unscaled, for
// rationale text where a percentage reads better than a score.
func OrphanedShare(v *index.View, now time.Time) map[string]float64 {
	type tally struct{ orphaned, total int }
	byFile := make(map[string]*tally, len(v.Authorship))
	for _, a := range v.Authorship {
		t := byFile[a.Path]
		if t == nil {
			t = &tally{}
			byFile[a.Path] = t
		}
		t.total += a.Lines
		if vcs.Orphaned(a.LastActive, now) {
			t.orphaned += a.Lines
		}
	}

	out := make(map[string]float64, len(byFile))
	for path, t := range byFile {
		if t.total > 0 {
			out[path] = float64(t.orphaned) / float64(t.total)
		}
	}
	return out
}
