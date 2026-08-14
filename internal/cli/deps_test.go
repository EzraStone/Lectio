package cli

import (
	"strings"
	"testing"
)

func TestDepsAnswersTheBlastRadiusQuestion(t *testing.T) {
	dir := indexedRepo(t)

	code, out, errOut := run(t, "deps", "parseInterval", dir)
	if code != 0 {
		t.Fatalf("exit code = %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "what breaks") {
		t.Errorf("missing heading:\n%s", out)
	}
	// Parse calls parseInterval directly; the three packages are one hop past.
	if !strings.Contains(out, "sample.Parse") {
		t.Errorf("direct caller missing:\n%s", out)
	}
	if !strings.Contains(out, "1 hop away") {
		t.Errorf("results should be grouped by distance:\n%s", out)
	}
	// The grader's provenance belongs in the output, on stderr so it does not
	// contaminate anything parsing the list.
	if !strings.Contains(errOut, "static call edges only") {
		t.Errorf("provenance missing from stderr: %q", errOut)
	}
}

// Tests belong in the answer — the spec puts covering tests in the ground
// truth — but a symbol with one production caller and nine test functions
// reads as nine unrelated things unless they are split out.
func TestDepsSeparatesTestsFromProductionCallers(t *testing.T) {
	dir := indexedRepo(t)

	_, out, _ := run(t, "deps", "Parse", dir)
	if !strings.Contains(out, "tests that exercise it") {
		t.Errorf("tests were not given their own section:\n%s", out)
	}

	hopSection := out
	if i := strings.Index(out, "tests that exercise it"); i >= 0 {
		hopSection = out[:i]
	}
	if strings.Contains(hopSection, "_test.go") {
		t.Errorf("a test symbol appeared among the production callers:\n%s", hopSection)
	}
}

func TestDepsUsesFlagReversesDirection(t *testing.T) {
	dir := indexedRepo(t)

	code, out, _ := run(t, "deps", "--uses", "Parse", dir)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "depends on") {
		t.Errorf("heading = %q", out)
	}
	if !strings.Contains(out, "parseInterval") {
		t.Errorf("Parse calls parseInterval; that should show:\n%s", out)
	}
}

func TestDepsRespectsDepth(t *testing.T) {
	dir := indexedRepo(t)

	_, shallow, _ := run(t, "deps", "--depth", "1", "parseInterval", dir)
	_, deep, _ := run(t, "deps", "--depth", "3", "parseInterval", dir)

	if strings.Contains(shallow, "2 hops away") {
		t.Errorf("--depth 1 returned two-hop results:\n%s", shallow)
	}
	if !strings.Contains(deep, "2 hops away") {
		t.Errorf("--depth 3 should reach two hops:\n%s", deep)
	}
}

func TestDepsSuggestsNearMisses(t *testing.T) {
	dir := indexedRepo(t)

	code, _, errOut := run(t, "deps", "parseInterv", dir)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "did you mean") {
		t.Errorf("a half-remembered name should get a suggestion, got %q", errOut)
	}
}

func TestDepsUnknownSymbol(t *testing.T) {
	dir := indexedRepo(t)

	code, _, errOut := run(t, "deps", "zzzznotasymbol", dir)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "no symbol named") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestDepsRequiresASymbol(t *testing.T) {
	code, _, errOut := run(t, "deps")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "name a symbol") {
		t.Errorf("stderr = %q", errOut)
	}
}

// The default answer must not include edges CHA merely guessed at.
func TestDepsDefaultsToStaticEdges(t *testing.T) {
	dir := indexedRepo(t)

	_, _, staticOnly := run(t, "deps", "Next", dir)
	if !strings.Contains(staticOnly, "static call edges only") {
		t.Errorf("default provenance = %q", staticOnly)
	}

	_, _, withDynamic := run(t, "deps", "--include-dynamic", "Next", dir)
	if !strings.Contains(withDynamic, "over-approximates") {
		t.Errorf("--include-dynamic should say the answer is over-approximated, got %q", withDynamic)
	}
}
