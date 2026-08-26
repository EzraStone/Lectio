package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/backtest"
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

// couplingRun builds repositories whose fix rate is a fixed multiple of their
// base rate, so the pooled lift is known before the test runs.
func couplingRun(n int, base, mult float64) []backtest.RepoCoupling {
	out := make([]backtest.RepoCoupling, 0, n)
	for i := 0; i < n; i++ {
		fixes := 100
		hit := int(base * mult * float64(fixes))
		out = append(out, backtest.RepoCoupling{
			Repo: fmt.Sprintf("owner/repo%02d", i),
			CouplingResult: backtest.CouplingResult{
				Pairs: 50, CoupledFiles: 20, NewcomerFixes: fixes, FixesOnCoupled: hit,
				BaseRate: base, FixRate: float64(hit) / float64(fixes), Lift: mult,
				Verdict: "no relationship",
			},
		})
	}
	return out
}

func TestCouplingReportPrintsItsInterval(t *testing.T) {
	env, out, _ := testEnv()
	rs := couplingRun(20, 0.5, 1.02)
	renderCoupling(env, rs, backtest.PooledCoupling(rs))
	got := strings.Join(strings.Fields(plain(out.String())), " ")

	if !strings.Contains(got, "95% interval") {
		t.Errorf("no interval on a 20-repository run:\n%s", got)
	}
	if !strings.Contains(got, "bootstrapped over 20 repositories") {
		t.Errorf("the interval does not say what it resampled:\n%s", got)
	}
}

// The half a null result usually leaves out: what the corpus could have seen.
func TestCouplingReportStatesWhatItCouldHaveDetected(t *testing.T) {
	env, out, _ := testEnv()
	// Repositories that disagree, so the interval is wide and contains 1.0.
	rs := couplingRun(10, 0.3, 2.2)
	for i, r := range couplingRun(10, 0.8, 0.6) {
		r.Repo = fmt.Sprintf("owner/low%02d", i)
		rs = append(rs, r)
	}
	renderCoupling(env, rs, backtest.PooledCoupling(rs))
	got := strings.Join(strings.Fields(plain(out.String())), " ")

	for _, want := range []string{
		"The interval contains 1.0",
		"has not shown a relationship in either direction",
		"would have landed outside it",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// Below the cluster floor there is no interval, and the report says so rather
// than printing a range it cannot support.
func TestCouplingReportSaysWhenItCannotResample(t *testing.T) {
	env, out, _ := testEnv()
	rs := couplingRun(4, 0.5, 1.4)
	renderCoupling(env, rs, backtest.PooledCoupling(rs))
	got := strings.Join(strings.Fields(plain(out.String())), " ")

	if !strings.Contains(got, "No interval: 4 repositories cleared both sample-size guards, fewer than the 8") {
		t.Errorf("a four-repository run did not say why it has no interval:\n%s", got)
	}
	if strings.Contains(got, "95% interval") {
		t.Errorf("a four-repository run printed an interval:\n%s", got)
	}
}

// A pooled lift of 1.6 on an interval running down to 0.8 is not a positive
// result, and colouring it green is how this project would make the same
// mistake a fourth time.
func TestCouplingReadingFollowsTheIntervalNotThePoint(t *testing.T) {
	env, out, _ := testEnv()
	env.Color = true
	// Ten repositories, half at lift 3.5 and half at 0.3. Pooling on counts
	// gives 1.9 — well past the old rule's 1.5 — while resampling ten of them
	// reaches draws whose lift is under 1.0, so the interval contains it.
	rs := couplingRun(5, 0.2, 3.5)
	for i, r := range couplingRun(5, 0.2, 0.3) {
		r.Repo = fmt.Sprintf("owner/low%02d", i)
		rs = append(rs, r)
	}
	pooled := backtest.PooledCoupling(rs)
	if pooled.Lift < 1.5 {
		t.Fatalf("the fixture's pooled lift is %.2f; it needs to clear the old rule's 1.5", pooled.Lift)
	}
	iv := backtest.BootstrapCoupling(rs, backtest.DefaultLevel, backtest.BootstrapIters, couplingSeed)
	if iv.ExcludesNoRelationship() {
		t.Fatalf("the fixture's interval [%.2f, %.2f] does not contain 1.0", iv.Lo, iv.Hi)
	}
	renderCoupling(env, rs, pooled)

	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.Contains(plain(line), "reading:") {
			continue
		}
		if strings.Contains(line, ansiGreen) {
			t.Errorf("a high pooled lift on an interval containing 1.0 was coloured green:\n%q", line)
		}
		return
	}
	t.Error("no reading line in the coupling report")
}
