package cli

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/backtest"
)

func TestBacktestRejectsANonRepo(t *testing.T) {
	code, _, errOut := run(t, "backtest", t.TempDir())
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "not a git repository") {
		t.Errorf("stderr = %q", errOut)
	}
}

// The fixture repo has a handful of commits and one author, which is exactly
// the shape Gate A must decline rather than score.
func TestBacktestDeclinesThinHistory(t *testing.T) {
	dir := fixtureRepo(t)

	code, _, errOut := run(t, "backtest", dir)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "skipping") {
		t.Errorf("stderr should explain why the repo was skipped, got %q", errOut)
	}
	if !strings.Contains(errOut, "commits") && !strings.Contains(errOut, "contributors") {
		t.Errorf("stderr should name the reason, got %q", errOut)
	}
}

// Reporting a pass on four cases without saying so would be the most
// flattering possible reading of the evidence, which is what a go/no-go must
// not do.
func TestReportWarnsBelowThirtyCases(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, backtest.Report{
		Cases: 4, K: 10,
		Aggregates: []backtest.Aggregate{
			{Strategy: "lectio", PrecisionA: 0.5},
			{Strategy: "largest files", PrecisionA: 0.1},
		},
		Medians: map[string]float64{"lectio": 0.5, "largest files": 0.1},
		Verdict: backtest.Verdict{Passed: true, Note: "beat all 1 baselines"},
	})

	got := out.String()
	if !strings.Contains(got, "PASS") {
		t.Errorf("verdict missing:\n%s", got)
	}
	if !strings.Contains(got, "smoke test") {
		t.Errorf("a 4-case pass was reported without qualification:\n%s", got)
	}
}

func TestReportOmitsTheWarningAtScale(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, backtest.Report{
		Cases: 40, K: 10,
		Aggregates: []backtest.Aggregate{{Strategy: "lectio", PrecisionA: 0.5}},
		Medians:    map[string]float64{"lectio": 0.5},
		Verdict:    backtest.Verdict{Passed: true, Note: "beat all 4 baselines"},
	})
	if strings.Contains(out.String(), "smoke test") {
		t.Errorf("a 40-case run should not carry the small-sample warning:\n%s", out.String())
	}
}

func TestReportShowsAFailureClearly(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, backtest.Report{
		Cases: 40, K: 10,
		Aggregates: []backtest.Aggregate{
			{Strategy: "lectio", PrecisionA: 0.20},
			{Strategy: "most churned, 12mo", PrecisionA: 0.35},
		},
		Medians: map[string]float64{"lectio": 0.2, "most churned, 12mo": 0.35},
		Verdict: backtest.Verdict{Passed: false, Note: "did not beat [most churned, 12mo]"},
	})

	got := out.String()
	if !strings.Contains(got, "FAIL") {
		t.Errorf("a failing gate must say so plainly:\n%s", got)
	}
	if !strings.Contains(got, "most churned") {
		t.Errorf("the failure should name what beat it:\n%s", got)
	}
}

func TestBacktestAppearsInHelp(t *testing.T) {
	_, out, _ := run(t, "--help")
	if !strings.Contains(out, "backtest") {
		t.Errorf("backtest missing from help:\n%s", out)
	}
	if !strings.Contains(out, "Gate A") {
		t.Errorf("help should name what backtest is for:\n%s", out)
	}
}

// Width verbs count bytes and ANSI escapes are bytes, so colouring a cell
// before padding it silently shifts that row left. The lectio row is the one
// that is coloured, which made it the one that looked wrong.
func TestReportColumnsAlignWithColorOn(t *testing.T) {
	var out, errBuf bytes.Buffer
	env := &Env{Stdout: &out, Stderr: &errBuf, Color: true}

	renderReport(env, backtest.Report{
		Cases: 30, K: 10,
		Aggregates: []backtest.Aggregate{
			{Strategy: "lectio", PrecisionA: 0.5},
			{Strategy: "most churned, 12mo", PrecisionA: 0.4},
		},
		Medians: map[string]float64{"lectio": 0.5, "most churned, 12mo": 0.4},
		Verdict: backtest.Verdict{Passed: true, Note: "beat all baselines"},
	})

	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	var widths []int
	for _, line := range strings.Split(out.String(), "\n") {
		plain := ansi.ReplaceAllString(line, "")
		if strings.Contains(plain, "%") && strings.HasPrefix(plain, "  ") {
			widths = append(widths, strings.Index(plain, "%"))
		}
	}
	if len(widths) < 2 {
		t.Fatalf("expected two data rows, got %d:\n%s", len(widths), out.String())
	}
	for i := 1; i < len(widths); i++ {
		if widths[i] != widths[0] {
			t.Errorf("columns misaligned once colour is stripped: %v\n%s", widths, out.String())
		}
	}
}

// %v on a slice runs the names together with only spaces, and these names
// contain spaces themselves.
func TestFailureVerdictSeparatesBaselineNames(t *testing.T) {
	rep := backtest.Summarize([]backtest.CaseResult{{
		Scores: []backtest.Score{
			{Strategy: "lectio", Precision: 0.1},
			{Strategy: "most churned, 12mo", Precision: 0.5},
			{Strategy: "most distinct authors", Precision: 0.5},
		},
	}}, 10)

	if rep.Verdict.Passed {
		t.Fatal("expected a failing verdict")
	}
	if !strings.Contains(rep.Verdict.Note, "most churned, 12mo; most distinct authors") {
		t.Errorf("baseline names are not readably separated: %q", rep.Verdict.Note)
	}
}

// A run that threw away most of its corpus is not measuring ranking quality,
// whatever number it prints.
func TestReportFlagsDegradedDiscards(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, backtest.Report{
		Cases: 12, Failed: 18, Degraded: 17, K: 10,
		Aggregates: []backtest.Aggregate{{Strategy: "lectio", PrecisionA: 0.5}},
		Medians:    map[string]float64{"lectio": 0.5},
		Verdict:    backtest.Verdict{Passed: true, Note: "beat all 4 baselines"},
	})

	got := out.String()
	if !strings.Contains(got, "17") || !strings.Contains(got, "thin index") {
		t.Errorf("discarded cases were not surfaced:\n%s", got)
	}
	if !strings.Contains(got, "12 cases scored") {
		t.Errorf("headline should say how many were actually scored:\n%s", got)
	}
}

func TestReportOmitsDegradedLineWhenClean(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, backtest.Report{
		Cases: 30, Failed: 0, Degraded: 0, K: 10,
		Aggregates: []backtest.Aggregate{{Strategy: "lectio", PrecisionA: 0.5}},
		Medians:    map[string]float64{"lectio": 0.5},
		Verdict:    backtest.Verdict{Passed: true, Note: "beat all 4 baselines"},
	})
	if strings.Contains(out.String(), "thin index") {
		t.Errorf("a clean run should not mention discards:\n%s", out.String())
	}
}

func TestBacktestCorpusRequiresAFetchedCache(t *testing.T) {
	path := tinyManifest(t, "0123456789abcdef0123456789abcdef01234567")

	code, _, errOut := run(t, "backtest", "--corpus", path, "--cache", t.TempDir())
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "corpus fetch") {
		t.Errorf("error should say how to fix it, got %q", errOut)
	}
}

func TestBacktestCorpusReportsAMissingManifest(t *testing.T) {
	code, _, errOut := run(t, "backtest", "--corpus", filepath.Join(t.TempDir(), "nope.json"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "read corpus") {
		t.Errorf("stderr = %q", errOut)
	}
}

// The table already holds these numbers, but reading them means subtracting
// each ablation row from the baseline by eye across seven near-identical rows.
func TestReportRendersSignalContributions(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, backtest.Report{
		Cases: 30, K: 10,
		Aggregates: []backtest.Aggregate{
			{Strategy: "lectio", PrecisionA: 0.40},
			{Strategy: "lectio −centrality", PrecisionA: 0.25},
			{Strategy: "lectio −churn", PrecisionA: 0.46},
			{Strategy: "most churned, 12mo", PrecisionA: 0.30},
		},
		Medians: map[string]float64{},
		Verdict: backtest.Verdict{Passed: true, Note: "beat all baselines"},
	})

	got := out.String()
	if !strings.Contains(got, "What each signal is worth") {
		t.Fatalf("no contribution section:\n%s", got)
	}
	if !strings.Contains(got, "+15.0 pp") {
		t.Errorf("centrality's contribution was not computed:\n%s", got)
	}
	if !strings.Contains(got, "-6.0 pp") {
		t.Errorf("churn's negative contribution was not computed:\n%s", got)
	}
	// A signal that hurts is the spec's named failure mode and must be called
	// out rather than left as a minus sign in a table.
	if !strings.Contains(got, "look here") || !strings.Contains(got, "churn") {
		t.Errorf("a harmful signal was not surfaced:\n%s", got)
	}
}

func TestReportOmitsContributionsOnAPlainRun(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, backtest.Report{
		Cases: 30, K: 10,
		Aggregates: []backtest.Aggregate{
			{Strategy: "lectio", PrecisionA: 0.40},
			{Strategy: "largest files", PrecisionA: 0.10},
		},
		Medians: map[string]float64{},
		Verdict: backtest.Verdict{Passed: true, Note: "beat all baselines"},
	})
	if strings.Contains(out.String(), "What each signal is worth") {
		t.Errorf("a plain run should not show a contribution table:\n%s", out.String())
	}
}

func TestReportStaysQuietWhenNoSignalHurts(t *testing.T) {
	env, out, _ := testEnv()
	renderReport(env, backtest.Report{
		Cases: 30, K: 10,
		Aggregates: []backtest.Aggregate{
			{Strategy: "lectio", PrecisionA: 0.40},
			{Strategy: "lectio −centrality", PrecisionA: 0.25},
			{Strategy: "lectio −churn", PrecisionA: 0.31},
		},
		Medians: map[string]float64{},
		Verdict: backtest.Verdict{Passed: true, Note: "beat all baselines"},
	})
	if strings.Contains(out.String(), "look here") {
		t.Errorf("no signal hurt, so nothing should be flagged:\n%s", out.String())
	}
}
