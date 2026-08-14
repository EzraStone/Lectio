package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// indexedRepo builds a fixture repo and indexes it, returning its path.
func indexedRepo(t *testing.T) string {
	t.Helper()
	dir := fixtureRepo(t)
	if code, out, errOut := run(t, "index", dir); code != 0 {
		t.Fatalf("index failed: code=%d\n%s\n%s", code, out, errOut)
	}
	return dir
}

func TestPathRequiresAnIndex(t *testing.T) {
	dir := fixtureRepo(t)
	code, _, errOut := run(t, "path", dir)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "lectio index") {
		t.Errorf("error should tell the user how to fix it, got %q", errOut)
	}
}

func TestPathPrintsAReadingPath(t *testing.T) {
	dir := indexedRepo(t)

	code, out, errOut := run(t, "path", "--top", "5", dir)
	if code != 0 {
		t.Fatalf("exit code = %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "Reading path") {
		t.Errorf("no header:\n%s", out)
	}
	// Tier labels are what make it a sequence rather than a leaderboard.
	if !strings.Contains(strings.ToUpper(out), "START HERE") {
		t.Errorf("no tier labels:\n%s", out)
	}
	if !strings.Contains(out, ".go:") {
		t.Errorf("items should carry a file and line:\n%s", out)
	}
}

func TestPathJSONIsStable(t *testing.T) {
	dir := indexedRepo(t)

	code, out, errOut := run(t, "path", "--json", "--top", "5", dir)
	if code != 0 {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}

	var got jsonOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got.Symbols == 0 {
		t.Error("symbols_indexed missing")
	}
	if len(got.Items) == 0 {
		t.Fatal("path is empty")
	}
	if len(got.Active) == 0 {
		t.Error("active_signals missing")
	}
	for _, it := range got.Items {
		if it.Symbol == "" || it.File == "" {
			t.Errorf("incomplete item: %+v", it)
		}
		if it.Rationale == "" {
			t.Errorf("%s has no rationale", it.Symbol)
		}
		if it.Tier < 1 || it.Tier > 3 {
			t.Errorf("%s has tier %d", it.Symbol, it.Tier)
		}
	}
}

// Advisory output must not contaminate JSON on stdout.
func TestJSONStdoutIsUnpolluted(t *testing.T) {
	dir := indexedRepo(t)
	_, out, _ := run(t, "path", "--json", dir)

	if strings.Contains(out, "signals:") || strings.Contains(out, "warning:") {
		t.Errorf("advisory text leaked into stdout:\n%s", out)
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		t.Errorf("stdout is not parseable as JSON: %v", err)
	}
}

func TestPathTaskScoping(t *testing.T) {
	dir := indexedRepo(t)

	code, out, errOut := run(t, "path", "--task", "billing", "--json", dir)
	if code != 0 {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	var got jsonOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Task == "" {
		t.Error("task not reported in output")
	}

	var proximityActive bool
	for _, s := range got.Active {
		if s == "task_proximity" {
			proximityActive = true
		}
	}
	if !proximityActive {
		t.Errorf("naming a task should activate proximity; active = %v", got.Active)
	}
}

func TestPathRejectsAnUnknownTask(t *testing.T) {
	dir := indexedRepo(t)

	code, _, errOut := run(t, "path", "--task", "nonexistent-area-xyz", dir)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "nothing in this repo matches") {
		t.Errorf("error = %q", errOut)
	}
}

func TestPathExplainShowsContributions(t *testing.T) {
	dir := indexedRepo(t)

	code, out, _ := run(t, "path", "--explain", "--top", "3", dir)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "centrality") {
		t.Errorf("--explain did not show per-signal contributions:\n%s", out)
	}
}

func TestPathTopLimitsResults(t *testing.T) {
	dir := indexedRepo(t)

	_, out, _ := run(t, "path", "--json", "--top", "3", dir)
	var got jsonOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got.Items) > 3 {
		t.Errorf("--top 3 returned %d items", len(got.Items))
	}
}

// Test symbols are indexed for coverage but must never be recommended reading.
func TestPathNeverRecommendsTests(t *testing.T) {
	dir := indexedRepo(t)

	_, out, _ := run(t, "path", "--json", "--top", "50", dir)
	var got jsonOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, it := range got.Items {
		if strings.HasSuffix(it.File, "_test.go") {
			t.Errorf("a test symbol reached the reading path: %s in %s", it.Symbol, it.File)
		}
	}
}
