package cli

import (
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/backtest"
)

// matchedReport builds a report whose matched column carries intervals, so the
// rendering can be checked against rows that mean different things.
func matchedReport() backtest.Report {
	iv := func(point, lo, hi float64, units int) backtest.Interval {
		return backtest.Interval{Point: point, Lo: lo, Hi: hi, Level: 0.95, Units: units}
	}
	return backtest.Report{
		Cases: 41, K: 10, Target: backtest.TargetTouched, Collapse: backtest.CollapseMean,
		CaseSet: "09f9c537b9ac", MatchedPairs: 762, MatchedCases: 41,
		Aggregates: []backtest.Aggregate{
			// Clear of chance: the interval does not reach 50%.
			{Strategy: "most recently modified", PrecisionA: 0.391, MatchedA: 0.592,
				MatchedCI: iv(0.592, 0.541, 0.643, 24)},
			// High point estimate, interval straddling chance. This is the row
			// the old fixed margin would have coloured and should not have.
			{Strategy: "churn only", PrecisionA: 0.401, MatchedA: 0.571,
				MatchedCI: iv(0.571, 0.489, 0.652, 24)},
			// Chance, and honestly so.
			{Strategy: "lectio", PrecisionA: 0.371, MatchedA: 0.553,
				MatchedCI: iv(0.553, 0.492, 0.615, 24)},
			// Below chance, clear of it.
			{Strategy: "size-proportional draw", PrecisionA: 0.370, MatchedA: 0.451,
				MatchedCI: iv(0.451, 0.410, 0.489, 24)},
		},
		Medians: map[string]float64{},
	}
}

func TestMatchedColumnPrintsItsInterval(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, matchedReport())
	got := plain(out.String())

	if !strings.Contains(got, "matched 95%") {
		t.Errorf("the matched column is not labelled with its coverage:\n%s", got)
	}
	for _, want := range []string{"59.2% ±5.1", "57.1% ±8.2", "55.3% ±6.2", "45.1% ±4.0"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing point-and-interval cell %q:\n%s", want, got)
		}
	}
}

// The footnote has to say what the interval was resampled over, because a
// bootstrap over pairs and a bootstrap over repositories give very different
// answers from the same run.
func TestMatchedFootnoteNamesTheClusterCount(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, matchedReport())
	got := strings.Join(strings.Fields(plain(out.String())), " ")

	for _, want := range []string{
		"bootstrapped over the 24 repositories that produced pairs",
		"rather than over the pairs themselves",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("footnote is missing %q:\n%s", want, got)
		}
	}
}

// Colour marks a claim. A row whose interval contains chance has not made one,
// however far its point estimate sits from 50%.
func TestOnlyRowsClearOfChanceAreColoured(t *testing.T) {
	env, out, _ := testEnv()
	env.Color = true
	renderReport(env, matchedReport())

	// The strategy label carries its own styling, so the assertions look at
	// the escape wrapping the matched cell rather than at the line as a whole.
	want := map[string]string{
		"most recently modified": ansiGreen, // 59.2%, interval bottoms out at 54.1
		"churn only":             ansiDim,   // 57.1%, but the interval reaches 48.9
		"lectio":                 ansiDim,   // 55.3%, straddles
		"size-proportional draw": ansiRed,   // 45.1%, clear of chance below it
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(out.String(), "\n") {
		bare := plain(line)
		for name, code := range want {
			if !strings.HasPrefix(strings.TrimSpace(bare), name) {
				continue
			}
			seen[name] = true
			// The matched cell is the last styled span on the row. Trim the
			// closing reset first, or LastIndex finds that instead of the code
			// that opened the span.
			cell := line[strings.LastIndex(strings.TrimSuffix(line, ansiReset), "\x1b["):]
			if !strings.HasPrefix(cell, code) {
				t.Errorf("%s: matched cell styled %q, want %q", name, escapeOf(cell), escapeOf(code))
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s never appeared in the table", name)
		}
	}
}

// escapeOf names an ANSI code for a failure message, since the raw bytes are
// invisible in test output.
func escapeOf(s string) string {
	for code, name := range map[string]string{
		ansiGreen: "green", ansiRed: "red", ansiDim: "dim",
		ansiCyan: "cyan", ansiYellow: "yellow", ansiBold: "bold",
	} {
		if strings.HasPrefix(s, code) {
			return name
		}
	}
	return "unstyled"
}

func TestReportsWithoutPairsPrintNoMatchedColumn(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, fileReport())
	got := plain(out.String())

	if strings.Contains(got, "matched") {
		t.Errorf("a run that produced no pairs still printed a matched column:\n%s", got)
	}
}

func TestBootstrapClustersTakesTheLargest(t *testing.T) {
	r := matchedReport()
	r.Aggregates = append(r.Aggregates, backtest.Aggregate{Strategy: "no pairs at all"})
	if got := bootstrapClusters(r); got != 24 {
		t.Errorf("bootstrapClusters = %d, want 24 — a strategy with no pairs pulled it down", got)
	}
	if got := bootstrapClusters(fileReport()); got != 0 {
		t.Errorf("bootstrapClusters on a run with no intervals = %d, want 0", got)
	}
}

// consistencyReport gives three strategies the three readings the table can
// print: broad, concentrated, and consistently below chance.
func consistencyReport() backtest.Report {
	r := matchedReport()
	r.Aggregates[0].MatchedRepos = backtest.Consistency{Above: 20, Below: 4, P: 0.0015439}
	r.Aggregates[1].MatchedRepos = backtest.Consistency{Above: 13, Below: 11, P: 0.8388}
	r.Aggregates[2].MatchedRepos = backtest.Consistency{Above: 12, Below: 12, P: 1}
	r.Aggregates[3].MatchedRepos = backtest.Consistency{Above: 4, Below: 20, P: 0.0015439}
	return r
}

func TestConsistencyTableCountsRepositories(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, consistencyReport())
	got := plain(out.String())

	for _, want := range []string{"repos >50%", "sign p", "20 / 24", "13 / 24", "4 / 24"} {
		if !strings.Contains(got, want) {
			t.Errorf("consistency table is missing %q:\n%s", want, got)
		}
	}
}

// The three readings have to differ, or the column is decoration.
func TestConsistencyReadingsSeparateBroadFromConcentrated(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, consistencyReport())

	want := map[string]string{
		"most recently modified": "above chance in most repositories",
		"churn only":             "a coin, repository by repository",
		"lectio":                 "a coin, repository by repository",
		"size-proportional draw": "below chance in most repositories",
	}
	for _, line := range strings.Split(plain(out.String()), "\n") {
		trimmed := strings.TrimSpace(line)
		for name, reading := range want {
			// Only the consistency table has a sign p, which is what
			// distinguishes these rows from the main table's.
			if !strings.HasPrefix(trimmed, name) || !strings.Contains(trimmed, "/") {
				continue
			}
			if !strings.Contains(trimmed, reading) {
				t.Errorf("%s reads %q, want %q", name, trimmed, reading)
			}
		}
	}
}

// A 13/24 split is a coin. Reporting it as an effect because 13 is more than
// half is the mistake the p-value is there to prevent.
func TestANarrowMajorityIsNotAReading(t *testing.T) {
	c := backtest.Consistency{Above: 13, Below: 11, P: 0.8388}
	if c.Lopsided() {
		t.Errorf("13 of 24 read as lopsided at p=%.4f", c.P)
	}
	if got := c.Share(); got <= 0.5 {
		t.Errorf("Share() = %.3f, want the majority it is not a reading of", got)
	}
}

func TestConsistencyTableIsAbsentWithoutRepositories(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, matchedReport())
	if got := plain(out.String()); strings.Contains(got, "sign p") {
		t.Errorf("a run with no repository counts printed the consistency table:\n%s", got)
	}
}
