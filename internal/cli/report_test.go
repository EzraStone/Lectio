package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/backtest"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func fileReport() backtest.Report {
	rep := backtest.Report{
		Cases: 85, Failed: 29, Degraded: 29, K: 10,
		Collapse: backtest.CollapseMean, Target: backtest.TargetTouched,
		CaseSet: "499ac2622deb",
		Aggregates: []backtest.Aggregate{
			{Strategy: "lectio", PrecisionA: 0.419, RecallA: 0.412, MRR: 0.702},
			{Strategy: "largest files", PrecisionA: 0.481, RecallA: 0.457, MRR: 0.751},
			{Strategy: "most churned, 12mo", PrecisionA: 0.415},
			{Strategy: "most recently modified", PrecisionA: 0.402},
			{Strategy: "most distinct authors", PrecisionA: 0.411},
		},
		Medians: map[string]float64{"lectio": 0.4, "largest files": 0.5},
	}
	for q, p := range [][2]float64{{0.245, 0.300}, {0.273, 0.275}, {0.336, 0.339}, {0.517, 0.531}} {
		spread := 2.0
		if q == 0 {
			spread = 23
		}
		rep.Strata = append(rep.Strata,
			backtest.StratumAggregate{Strategy: "lectio", Stratum: q, Cases: 85, Precision: p[0], Spread: spread},
			backtest.StratumAggregate{Strategy: "largest files", Stratum: q, Cases: 85, Precision: p[1], Spread: spread},
		)
	}
	rep.Verdict = backtest.Verdict{Lost: []string{"largest files"}, Note: "did not beat largest files"}
	return rep
}

// The header carries three facts a number cannot be quoted without: the
// collapse rule, the case set, and how much of the corpus was thrown away.
func TestReportHeaderCarriesItsProvenance(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, fileReport())
	got := plain(out.String())

	for _, want := range []string{
		"85 cases scored, 29 discarded",
		"collapsed to files by mean",
		"case set 499ac2622deb",
		"did not type-check",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("header is missing %q:\n%s", want, got)
		}
	}
}

// Width verbs count bytes and ANSI escapes are bytes, so colouring before
// padding shifts a coloured row left by the length of the escape. Lectio's row
// is the coloured one, which is exactly the row a reader compares against
// every other.
func TestReportColumnsStayAlignedUnderColour(t *testing.T) {
	env, out, _ := testEnv()
	env.Color = true
	renderReport(env, fileReport())

	// Only the main table. The stratified table below it has its own column
	// widths, and comparing across the two would compare different layouts.
	var offsets []int
	for _, line := range strings.Split(plain(out.String()), "\n") {
		if strings.Contains(line, "Precision within") {
			break
		}
		if strings.Contains(line, "%") && (strings.Contains(line, "lectio") || strings.Contains(line, "largest files")) {
			offsets = append(offsets, strings.Index(line, "%"))
		}
	}
	if len(offsets) < 2 {
		t.Fatalf("expected several percentage rows:\n%s", plain(out.String()))
	}
	for i, o := range offsets {
		if o != offsets[0] {
			t.Errorf("row %d puts its first %% at column %d, others at %d — colour shifted the row",
				i, o, offsets[0])
		}
	}
}

// The spread row is the table's own caveat. Without it the table invites the
// conclusion it was built to test.
func TestStratifiedTableAlwaysCarriesTheSpreadRow(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, fileReport())
	got := plain(out.String())

	if !strings.Contains(got, "size spread within band") {
		t.Errorf("no spread row:\n%s", got)
	}
	if !strings.Contains(got, "23x") {
		t.Errorf("the loose band's spread is not shown:\n%s", got)
	}
}

// Quartiles of file size say nothing about a run graded in declarations.
// Rendering it anyway would put a table under a symbolic result that looks
// like it controls for something it does not.
func TestSymbolicReportLabelsBandsAsDeclarations(t *testing.T) {
	rep := fileReport()
	rep.Target = backtest.TargetSymbols
	for i := range rep.Strata {
		if rep.Strata[i].Strategy == "largest files" {
			rep.Strata[i].Strategy = "largest symbols"
		}
	}
	rep.Aggregates = append(rep.Aggregates,
		backtest.Aggregate{Strategy: "largest symbols", PrecisionA: 0.228})

	env, out, _ := testEnv()
	renderReport(env, rep)
	got := plain(out.String())

	if !strings.Contains(got, "declaration-size bands") {
		t.Errorf("bands not labelled as declarations:\n%s", got)
	}
	if strings.Contains(got, "collapsed to files") {
		t.Errorf("a symbolic run reported a collapse rule it did not apply:\n%s", got)
	}
	if !strings.Contains(got, "declarations, not file paths") {
		t.Errorf("the target is not stated:\n%s", got)
	}
}

// A pass a control undercuts must not read as a clean pass.
func TestHollowPassIsAnnotated(t *testing.T) {
	rep := fileReport()
	rep.Verdict = backtest.Verdict{
		Passed:             true,
		Beaten:             []string{"largest files"},
		OutscoredByControl: []string{"largest symbols"},
		Note:               "beat all 4 baselines — but largest symbols scored higher, and it is a control",
	}

	env, out, _ := testEnv()
	renderReport(env, rep)
	got := plain(out.String())

	if !strings.Contains(got, "PASS") {
		t.Fatalf("no verdict line:\n%s", got)
	}
	if !strings.Contains(got, "cannot fail the gate") {
		t.Errorf("the pass was not annotated with what undercuts it:\n%s", got)
	}
}

// Reporting a pass on four cases without saying so would be the most
// flattering possible reading of the evidence.
func TestSmallRunsSayTheyAreSmallRuns(t *testing.T) {
	rep := fileReport()
	rep.Cases = 4

	env, out, _ := testEnv()
	renderReport(env, rep)
	if got := plain(out.String()); !strings.Contains(got, "smoke test") {
		t.Errorf("a four-case run did not warn:\n%s", got)
	}
}

// Nothing in the renderer may reach past the end of an empty report.
func TestReportRendersWithNothingInIt(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, backtest.Report{K: 10, Medians: map[string]float64{}})
	if plain(out.String()) == "" {
		t.Error("an empty report rendered nothing at all")
	}
}

// Chance is 50% and the column is read against it, so the caption has to say
// so — it is the only number in the report interpretable without a baseline
// beside it.
func TestMatchedColumnStatesItsChanceLevel(t *testing.T) {
	rep := fileReport()
	rep.MatchedPairs = 2891
	rep.MatchedCases = 67
	for i := range rep.Aggregates {
		rep.Aggregates[i].MatchedA = 0.495
	}

	env, out, _ := testEnv()
	renderReport(env, rep)
	got := plain(out.String())

	if !strings.Contains(got, "matched") {
		t.Fatalf("no matched column:\n%s", got)
	}
	for _, want := range []string{"2891 size-matched pairs", "67 of 85 cases", "50% is chance"} {
		if !strings.Contains(got, want) {
			t.Errorf("caption is missing %q:\n%s", want, got)
		}
	}
}

// Telling a reader that a file run paired declarations describes a measure it
// did not run.
func TestMatchedCaptionFollowsTheTarget(t *testing.T) {
	rep := fileReport()
	rep.MatchedPairs, rep.MatchedCases = 100, 10

	// The caption wraps, so line breaks fall inside the phrase being checked.
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }

	env, out, _ := testEnv()
	renderReport(env, rep)
	if got := flat(plain(out.String())); !strings.Contains(got, "for each file the contributor touched") {
		t.Errorf("a file target did not describe pairing files:\n%s", got)
	}

	rep.Target = backtest.TargetSymbols
	env2, out2, _ := testEnv()
	renderReport(env2, rep)
	if got := flat(plain(out2.String())); !strings.Contains(got, "for each declaration the contributor touched") {
		t.Errorf("a symbolic target did not describe pairing declarations:\n%s", got)
	}
}

// A run with no pairs must print no column at all rather than a blank one
// implying a control it never applied.
func TestNoMatchedColumnWithoutPairs(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, fileReport()) // MatchedPairs is zero
	if got := plain(out.String()); strings.Contains(got, "size-matched pairs") {
		t.Errorf("a run with no pairs printed the matched caption:\n%s", got)
	}
}
