package rank

import (
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
)

// activeRepo is a repository with a year of history, all of it recent and all
// by one person who is still around: authorship exists, nothing is orphaned,
// nothing is machine-authored.
func activeRepo(now time.Time) *index.View {
	day := 24 * time.Hour
	v := &index.View{
		Now: now,
		Symbols: map[core.SymbolID]core.Symbol{
			"p.A": {ID: "p.A", File: "a.go", Package: "p", StartLine: 1, EndLine: 20},
			"p.B": {ID: "p.B", File: "b.go", Package: "p", StartLine: 1, EndLine: 30},
		},
	}
	for i := 0; i < 8; i++ {
		v.Commits = append(v.Commits, core.Commit{
			Hash:        string(rune('a'+i)) + "0",
			AuthorEmail: "ada@example.com",
			When:        now.Add(-time.Duration(i*7) * day),
			Subject:     "add a thing",
			Files:       []core.FileChange{{Path: "a.go", Added: 10}},
		})
	}
	v.Authorship = []core.Authorship{
		{Path: "a.go", Author: "ada@example.com", Lines: 80, LastActive: now},
		{Path: "b.go", Author: "ada@example.com", Lines: 30, LastActive: now},
	}
	return v
}

func has(sigs []Signal, want Signal) bool {
	for _, s := range sigs {
		if s == want {
			return true
		}
	}
	return false
}

// The bug this file exists for. A repository with authorship where nobody has
// gone quiet was reported as having no data for orphaning; the truth is that
// orphaning ran across the whole repository and found nothing.
func TestOrphaningWithLiveAuthorsIsEmptyNotUnavailable(t *testing.T) {
	now := time.Now().UTC()
	p := DefaultParams()
	p.Now = now

	res := Rank(activeRepo(now), p, DefaultWeights())

	if !has(res.Silent, SignalOrphaning) {
		t.Fatal("orphaning scored something on a repository with no orphaned code")
	}
	if has(res.Unavailable, SignalOrphaning) {
		t.Error("orphaning was reported as missing its input, which the fixture supplies")
	}
	if !has(res.Empty, SignalOrphaning) {
		t.Error("orphaning was not reported as having run and found nothing")
	}
}

// And the case it must still catch: no authorship at all means the signal
// never ran, which is a gap in the index rather than a fact about the code.
func TestOrphaningWithNoAuthorshipIsUnavailable(t *testing.T) {
	now := time.Now().UTC()
	v := activeRepo(now)
	v.Authorship = nil
	p := DefaultParams()
	p.Now = now

	res := Rank(v, p, DefaultWeights())
	if !has(res.Unavailable, SignalOrphaning) {
		t.Error("orphaning with no authorship was not reported as missing its input")
	}
	if has(res.Empty, SignalOrphaning) {
		t.Error("orphaning with no authorship was reported as a finding")
	}
}

// The four history signals share an input, and a repository with no commits
// has a gap rather than four findings.
func TestHistorySignalsAreUnavailableWithoutCommits(t *testing.T) {
	now := time.Now().UTC()
	v := activeRepo(now)
	v.Commits, v.Authorship = nil, nil
	p := DefaultParams()
	p.Now = now

	res := Rank(v, p, DefaultWeights())
	for _, sig := range []Signal{SignalChurn, SignalFixDensity, SignalAIDensity, SignalCoupling} {
		if !has(res.Unavailable, sig) {
			t.Errorf("%s was not reported as missing its input on a repository with no history", sig)
		}
	}
}

// AI density on a repository whose commits carry no machine-authorship markers
// is the example the original comment used, and it is a finding.
func TestAIDensityWithCleanHistoryIsEmpty(t *testing.T) {
	now := time.Now().UTC()
	p := DefaultParams()
	p.Now = now

	res := Rank(activeRepo(now), p, DefaultWeights())
	if !has(res.Empty, SignalAIDensity) {
		t.Error("a history with no AI markers was not reported as a finding")
	}
	if has(res.Unavailable, SignalAIDensity) {
		t.Error("a history with commits was reported as missing AI density's input")
	}
}

// Proximity is optional by design. Nothing was asked of it, so its silence is
// closer to a gap than to a finding.
func TestProximityWithoutATaskIsUnavailable(t *testing.T) {
	now := time.Now().UTC()
	p := DefaultParams()
	p.Now = now

	res := Rank(activeRepo(now), p, DefaultWeights())
	if !has(res.Unavailable, SignalProximity) {
		t.Error("proximity with no task was not reported as missing its input")
	}
}

// Silent stays the union, so a caller that only wants "which signals said
// nothing" does not have to add two lists together.
func TestSilentIsTheUnionOfBoth(t *testing.T) {
	now := time.Now().UTC()
	p := DefaultParams()
	p.Now = now

	res := Rank(activeRepo(now), p, DefaultWeights())
	if len(res.Silent) != len(res.Unavailable)+len(res.Empty) {
		t.Errorf("Silent has %d, Unavailable %d and Empty %d — they do not partition it",
			len(res.Silent), len(res.Unavailable), len(res.Empty))
	}
	for _, sig := range append(append([]Signal{}, res.Unavailable...), res.Empty...) {
		if !has(res.Silent, sig) {
			t.Errorf("%s is classified but not in Silent", sig)
		}
	}
	for _, sig := range res.Active {
		if has(res.Silent, sig) {
			t.Errorf("%s is both active and silent", sig)
		}
	}
}

// A signal that cannot say whether it had input is assumed to have had it. An
// unexplained silence reported as a finding overstates the evidence by less
// than a real finding reported as a gap understates it.
func TestSignalsWithoutAnAvailabilityCheckAreAssumedToHaveInput(t *testing.T) {
	now := time.Now().UTC()
	p := DefaultParams()
	p.Now = now

	unavailable, empty := classifySilence(activeRepo(now), p, []Signal{"invented"})
	if len(unavailable) != 1 || len(empty) != 0 {
		t.Errorf("an unknown signal classified as %v / %v, want it treated as a gap",
			unavailable, empty)
	}
}
