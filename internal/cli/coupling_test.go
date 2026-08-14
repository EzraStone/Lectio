package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
)

// The wall-clock trap, a third time.
//
// HiddenCoupling computes co-change inside [Now-ChurnWindow, Now]. A pinned
// checkout read with Now at the wall clock is asked what changed together in
// the last twelve months and correctly answers: nothing. That returned zero
// pairs for every repository in the corpus on the first attempt, and it is the
// same failure that once made all four history baselines score 0.0% — in a
// different function, which is why a test in internal/index did not catch it.
func TestCouplingParamsAnchorOnTheRepositoryNotTheClock(t *testing.T) {
	lastCommit := time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC)
	v := &index.View{
		Now: time.Now(),
		Commits: []core.Commit{
			{Hash: "a", When: lastCommit.Add(-200 * 24 * time.Hour)},
			{Hash: "b", When: lastCommit},
		},
	}

	p := couplingParams(v)
	if !p.Now.Equal(lastCommit) {
		t.Errorf("Now = %s, want the repository's last commit %s", p.Now, lastCommit)
	}

	// Both commits have to fall inside the window, or the check reads a
	// fraction of the evidence it was handed.
	cutoff := p.Now.Add(-p.ChurnWindow)
	for _, c := range v.Commits {
		if c.When.Before(cutoff) {
			t.Errorf("commit %s at %s falls outside the window starting %s", c.Hash, c.When, cutoff)
		}
	}
}

// A repository with no history must not produce a zero time, which would put
// every commit in the future and silently score nothing.
func TestCouplingParamsFallsBackWhenHistoryIsEmpty(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := couplingParams(&index.View{Now: now})
	if !p.Now.Equal(now) {
		t.Errorf("Now = %s, want the view's own reference time %s", p.Now, now)
	}
	if p.Now.IsZero() {
		t.Error("a zero reference time puts all history in the future")
	}
}

// The window the check reads must match the history the index was told to
// load, or half the fix is undone by the other half.
func TestCouplingWindowMatchesTheIndexedHistory(t *testing.T) {
	p := couplingParams(&index.View{Now: time.Now()})
	if p.ChurnWindow != couplingWindow {
		t.Errorf("ChurnWindow = %s, want the coupling window %s", p.ChurnWindow, couplingWindow)
	}
	if p.ChurnWindow < 10*365*24*time.Hour {
		t.Errorf("window of %s is too short to cover a mature repository's history", p.ChurnWindow)
	}
}

// A conclusion that scrolls off the right edge of an eighty-column terminal is
// one nobody reads, and the size readings run to a hundred and fifty
// characters.
func TestWrapKeepsLinesInsideTheWidth(t *testing.T) {
	long := "largest files leads every band, but by 0.3 pp across the 2 bands where size " +
		"is actually controlled — the ranking is not losing on choices, it is adding " +
		"nothing over size"

	got := wrap(long, 68)
	if len(got) < 2 {
		t.Fatalf("a %d-character note produced %d line(s)", len(long), len(got))
	}
	for i, l := range got {
		if len([]rune(l)) > 68 {
			t.Errorf("line %d is %d runes: %q", i, len([]rune(l)), l)
		}
	}
	// No word may be lost or invented.
	if strings.Join(got, " ") != long {
		t.Errorf("wrapping changed the text:\n got %q\nwant %q", strings.Join(got, " "), long)
	}
}

func TestWrapHandlesEmptyInput(t *testing.T) {
	if got := wrap("", 40); len(got) != 1 || got[0] != "" {
		t.Errorf("wrap(\"\") = %q", got)
	}
}
