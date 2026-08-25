package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/backtest"
)

func writeReport(t *testing.T, dir, name string, r backtest.Report) string {
	t.Helper()
	path := filepath.Join(dir, name)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func comparableReport(caseSet string, lectio, matched float64) backtest.Report {
	return backtest.Report{
		Schema: backtest.ReportSchema, CaseSet: caseSet, Cases: 80, K: 10,
		Collapse: backtest.CollapseMean, Target: backtest.TargetTouched,
		Aggregates: []backtest.Aggregate{
			{Strategy: "lectio", PrecisionA: lectio, MatchedA: matched},
			{Strategy: "largest files", PrecisionA: 0.48, MatchedA: 0.49},
		},
		Medians: map[string]float64{},
	}
}

func TestCompareShowsDeltasForComparableRuns(t *testing.T) {
	dir := t.TempDir()
	a := writeReport(t, dir, "a.json", comparableReport("same", 0.40, 0.52))
	b := writeReport(t, dir, "b.json", comparableReport("same", 0.44, 0.55))

	env, out, _ := testEnv()
	if code := Main(t.Context(), env, []string{"compare", a, b}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := plain(out.String())

	if !strings.Contains(got, "lectio") {
		t.Errorf("no strategy rows:\n%s", got)
	}
	if !strings.Contains(got, "+4.0") {
		t.Errorf("the precision delta is missing:\n%s", got)
	}
}

// Refusing has to be an error exit, not a warning buried in output. A script
// comparing two runs must not treat "these are not comparable" as success.
func TestCompareExitsNonZeroWhenNotComparable(t *testing.T) {
	dir := t.TempDir()
	a := writeReport(t, dir, "a.json", comparableReport("aaaa", 0.40, 0.52))
	b := writeReport(t, dir, "b.json", comparableReport("bbbb", 0.44, 0.55))

	env, out, _ := testEnv()
	code := Main(t.Context(), env, []string{"compare", a, b})
	if code == 0 {
		t.Error("comparing two different case sets exited zero")
	}
	if got := plain(out.String()); !strings.Contains(got, "not comparable") {
		t.Errorf("no refusal in the output:\n%s", got)
	}
}

// A file that is not a report should say what it expected rather than
// producing a JSON parse error nobody can act on.
func TestCompareExplainsABadFile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "notes.txt")
	os.WriteFile(bad, []byte("this is not a report"), 0o644)
	good := writeReport(t, dir, "a.json", comparableReport("same", 0.40, 0.52))

	env, _, errOut := testEnv()
	if code := Main(t.Context(), env, []string{"compare", bad, good}); code == 0 {
		t.Error("comparing a non-report exited zero")
	}
	if got := errOut.String(); !strings.Contains(got, "backtest --json") {
		t.Errorf("the error does not say what the file should be:\n%s", got)
	}
}

func TestCompareNeedsTwoArguments(t *testing.T) {
	dir := t.TempDir()
	a := writeReport(t, dir, "a.json", comparableReport("same", 0.40, 0.52))

	env, _, _ := testEnv()
	if code := Main(t.Context(), env, []string{"compare", a}); code == 0 {
		t.Error("compare with one argument exited zero")
	}
}

// Crossing chance is a change of claim. The output has to name it, because a
// delta of four points looks the same on either side of 50%.
func TestCompareCallsOutCrossingChance(t *testing.T) {
	dir := t.TempDir()
	a := writeReport(t, dir, "a.json", comparableReport("same", 0.40, 0.52))
	b := writeReport(t, dir, "b.json", comparableReport("same", 0.40, 0.48))

	env, out, _ := testEnv()
	Main(t.Context(), env, []string{"compare", a, b})
	flat := strings.Join(strings.Fields(plain(out.String())), " ")

	if !strings.Contains(flat, "crossed chance") {
		t.Errorf("crossing chance was not called out:\n%s", flat)
	}
}
