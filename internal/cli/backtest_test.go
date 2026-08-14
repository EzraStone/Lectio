package cli

import (
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
