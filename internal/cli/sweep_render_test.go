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

// leakingReport is run 10's file-granularity shape: a size control clear of
// chance at 1.25x, and a sweep whose 1.10x column is clean.
func leakingReport() backtest.Report {
	r := matchedReport()
	r.SizeRatio = 1.25
	iv := func(lo, hi float64) backtest.Interval {
		return backtest.Interval{Lo: lo, Hi: hi, Level: 0.95, Units: 17}
	}
	r.Aggregates = append(r.Aggregates, backtest.Aggregate{
		Strategy: "largest files", PrecisionA: 0.481, MatchedA: 0.529,
		MatchedCI: backtest.Interval{Point: 0.529, Lo: 0.502, Hi: 0.562, Level: 0.95, Units: 17},
	})
	r.Sweep = []backtest.RatioAggregate{
		{Ratio: 1.10, Strategy: "largest files", Matched: 0.505, Cases: 36, Pairs: 696, CI: iv(0.462, 0.548)},
		{Ratio: 1.25, Strategy: "largest files", Matched: 0.529, Cases: 41, Pairs: 793, CI: iv(0.502, 0.562)},
		{Ratio: 2.00, Strategy: "largest files", Matched: 0.587, Cases: 47, Pairs: 916, CI: iv(0.551, 0.622)},
	}
	return r
}

func TestReportWarnsWhenThePairingLeaks(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, leakingReport())
	got := strings.Join(strings.Fields(plain(out.String())), " ")

	for _, want := range []string{
		"largest files at 52.9%",
		"cannot tell two candidates of the same size apart",
		"pairing at 1.25x is still letting size through",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the leak warning is missing %q:\n%s", want, got)
		}
	}
}

// Warning is the minimum. When a sweep ran, the report can name a bound that
// works instead of only condemning the one that was used.
func TestReportNamesACleanRatioWhenTheSweepFoundOne(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, leakingReport())
	got := strings.Join(strings.Fields(plain(out.String())), " ")

	for _, want := range []string{
		"clean at 1.10x",
		"--size-ratio 1.10",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report did not name the clean ratio %q:\n%s", want, got)
		}
	}
}

func TestReportIsSilentWhenThePairingHolds(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, sweepReport())
	if got := plain(out.String()); strings.Contains(got, "letting size through") {
		t.Errorf("a clean run printed the leak warning:\n%s", got)
	}
}

// thinSweepReport has a 1.00x column resting on four repositories — the shape
// that reported a size strategy at 60.3% as clear of chance.
func thinSweepReport() backtest.Report {
	r := matchedReport()
	r.SizeRatio = 1.25
	r.Sweep = []backtest.RatioAggregate{
		{Ratio: 1.00, Strategy: "largest files", Matched: 0.603, Cases: 12, Pairs: 171,
			CI: backtest.Interval{Point: 0.603, Lo: 0, Hi: 1, Level: 0.95, Units: 4}},
		{Ratio: 1.25, Strategy: "largest files", Matched: 0.529, Cases: 41, Pairs: 793,
			CI: backtest.Interval{Point: 0.529, Lo: 0.502, Hi: 0.562, Level: 0.95, Units: 17}},
	}
	return r
}

// A column with no interval and a column whose interval straddles chance are
// different states, and printing them identically is how four repositories
// read like a measurement.
func TestSweepMarksColumnsWithNoInterval(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, thinSweepReport())
	lines := sweepSection(out.String())

	var row string
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "largest files") {
			row = l
			break
		}
	}
	if row == "" {
		t.Fatalf("no largest files row in:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(row, "60.3%?") {
		t.Errorf("the four-repository cell is not marked:\n%q", row)
	}
	if strings.Contains(row, "52.9%?") {
		t.Errorf("the seventeen-repository cell was marked as thin:\n%q", row)
	}
}

func TestSweepPrintsItsRepositoryCounts(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, thinSweepReport())
	got := strings.Join(strings.Fields(plain(out.String())), " ")

	if !strings.Contains(got, "repositories 4? 17") {
		t.Errorf("the repository row is missing or unmarked:\n%s", got)
	}
	if !strings.Contains(got, "? marks a column resampled over fewer than 8 repositories") {
		t.Errorf("the marker is not explained:\n%s", got)
	}
}

// A sweep where every column clears the cluster floor must not carry the
// explanation, or the note becomes furniture.
func TestSweepOmitsTheMarkerWhenEveryColumnIsThickEnough(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, sweepReport())
	got := plain(out.String())

	if strings.Contains(got, "? marks a column") {
		t.Errorf("a sweep with no thin columns printed the marker note:\n%s", got)
	}
	if !strings.Contains(strings.Join(strings.Fields(got), " "), "repositories 17 17 17") {
		t.Errorf("the repository row is missing:\n%s", got)
	}
}
