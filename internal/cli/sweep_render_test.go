package cli

import (
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/backtest"
)

func sweepReport() backtest.Report {
	r := matchedReport()
	iv := func(lo, hi float64) backtest.Interval {
		return backtest.Interval{Lo: lo, Hi: hi, Level: 0.95, Units: 17}
	}
	r.Sweep = []backtest.RatioAggregate{
		// 1.0x: only two strategies reach it at all.
		{Ratio: 1.0, Strategy: "most recently modified", Matched: 0.581, Cases: 9, Pairs: 96, CI: iv(0.532, 0.630)},
		{Ratio: 1.0, Strategy: "lectio", Matched: 0.512, Cases: 9, Pairs: 96, CI: iv(0.462, 0.560)},
		// 1.25x: everyone.
		{Ratio: 1.25, Strategy: "most recently modified", Matched: 0.565, Cases: 41, Pairs: 793, CI: iv(0.520, 0.605)},
		{Ratio: 1.25, Strategy: "churn only", Matched: 0.578, Cases: 41, Pairs: 793, CI: iv(0.521, 0.635)},
		{Ratio: 1.25, Strategy: "lectio", Matched: 0.540, Cases: 41, Pairs: 793, CI: iv(0.488, 0.593)},
		{Ratio: 2.0, Strategy: "most recently modified", Matched: 0.551, Cases: 58, Pairs: 1420, CI: iv(0.518, 0.584)},
		{Ratio: 2.0, Strategy: "churn only", Matched: 0.560, Cases: 58, Pairs: 1420, CI: iv(0.522, 0.598)},
		{Ratio: 2.0, Strategy: "lectio", Matched: 0.534, Cases: 58, Pairs: 1420, CI: iv(0.491, 0.577)},
	}
	return r
}

func TestSweepTableHasAColumnPerRatio(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, sweepReport())
	got := plain(out.String())

	for _, want := range []string{"1.00x", "1.25x", "2.00x"} {
		if !strings.Contains(got, want) {
			t.Errorf("sweep table is missing the %s column:\n%s", want, got)
		}
	}
	if strings.Contains(got, "1.50x") {
		t.Error("a ratio the sweep never ran appeared as a column")
	}
}

// The denominators change by more than an order of magnitude across the
// ladder, and a cell read without them is misleading in both directions.
func TestSweepPrintsItsDenominators(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, sweepReport())
	got := plain(out.String())

	for _, want := range []string{"cases / pairs", "9/96", "41/793", "58/1420"} {
		if !strings.Contains(got, want) {
			t.Errorf("sweep table is missing %q:\n%s", want, got)
		}
	}
}

// sweepSection returns the lines of the sweep table alone. The main table has
// a row per strategy too, and matching on the strategy name across the whole
// report finds that one first.
func sweepSection(out string) []string {
	lines := strings.Split(plain(out), "\n")
	for i, line := range lines {
		if strings.Contains(line, "1.00x") {
			return lines[i:]
		}
	}
	return nil
}

// A strategy that never reached the tightest ratio gets a dash, not a number
// and not a blank that reads as chance.
func TestSweepMarksUnreachedRatios(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, sweepReport())

	for _, line := range sweepSection(out.String()) {
		if !strings.HasPrefix(strings.TrimSpace(line), "churn only") {
			continue
		}
		if !strings.Contains(line, "—") {
			t.Errorf("churn only reached no cell at 1.00x but its row has no dash:\n%q", line)
		}
		if n := strings.Count(line, "%"); n != 2 {
			t.Errorf("churn only has %d cells, want the 2 ratios it reached:\n%q", n, line)
		}
		return
	}
	t.Error("churn only never appeared in the sweep table")
}

// The row that did reach every ratio must have no dash at all, or the dash is
// being printed for something other than an unreached ratio.
func TestSweepFillsEveryReachedRatio(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, sweepReport())

	for _, line := range sweepSection(out.String()) {
		if !strings.HasPrefix(strings.TrimSpace(line), "lectio") {
			continue
		}
		if strings.Contains(line, "—") {
			t.Errorf("lectio reached all three ratios but its row has a dash:\n%q", line)
		}
		if n := strings.Count(line, "%"); n != 3 {
			t.Errorf("lectio has %d cells, want 3:\n%q", n, line)
		}
		return
	}
	t.Error("lectio never appeared in the sweep table")
}

func TestSweepReadingIsSpelledOut(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, sweepReport())
	got := strings.Join(strings.Fields(plain(out.String())), " ")

	for _, want := range []string{
		"Read down a column, not across a row",
		"A result that only appears at the loose end is a result about the pairing bound",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("sweep footnote is missing %q", want)
		}
	}
}

func TestNoSweepTableWithoutASweep(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, matchedReport())
	// Not "1.25x": the matched footnote names the pairing ratio on every run,
	// swept or not. The sweep is identified by its own header.
	if got := sweepSection(out.String()); got != nil {
		t.Errorf("a run with no sweep printed a sweep table:\n%s", strings.Join(got, "\n"))
	}
	if got := plain(out.String()); !strings.Contains(got, "within 1.25x on size") {
		t.Errorf("the matched footnote stopped naming the pairing ratio:\n%s", got)
	}
}
