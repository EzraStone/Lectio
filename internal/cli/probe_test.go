package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// runWithInput drives a command with scripted stdin, which is how an
// interactive probe gets tested at all.
func runWithInput(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	env := &Env{Stdout: &out, Stderr: &errBuf, Stdin: strings.NewReader(stdin), Color: false}
	code := Main(context.Background(), env, args)
	return code, out.String(), errBuf.String()
}

func TestProbeAsksAndGrades(t *testing.T) {
	dir := indexedRepo(t)

	// An empty line skips, which exercises the whole loop without depending on
	// which symbol happens to be selected.
	code, out, errOut := runWithInput(t, "\n", "probe", dir)
	if code != 0 {
		t.Fatalf("exit code = %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "skipped") {
		t.Errorf("expected a skip to be acknowledged:\n%s", out)
	}
	// A skip must show the answer — that is the point of being allowed to skip.
	if !strings.Contains(out, "the answer is") {
		t.Errorf("a skip did not reveal the answer:\n%s", out)
	}
}

// The wording around a skip has to carry that it is neutral, or people learn
// to guess rather than skip.
func TestProbeSkipReadsAsNeutral(t *testing.T) {
	dir := indexedRepo(t)
	_, out, _ := runWithInput(t, "\n", "probe", dir)

	for _, forbidden := range []string{"wrong", "incorrect", "failed", "not quite"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Errorf("skip output contains %q, which reads as a failure:\n%s", forbidden, out)
		}
	}
}

func TestProbeHealthWithNoHistory(t *testing.T) {
	dir := indexedRepo(t)

	code, out, _ := runWithInput(t, "", "probe", "--health", dir)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "No probes answered yet") {
		t.Errorf("output = %q", out)
	}
}

func TestProbeHealthAfterAnswering(t *testing.T) {
	dir := indexedRepo(t)

	if code, out, errOut := runWithInput(t, "\n", "probe", dir); code != 0 {
		t.Fatalf("probe failed: %d\n%s\n%s", code, out, errOut)
	}

	code, out, _ := runWithInput(t, "", "probe", "--health", dir)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "Probe health") {
		t.Errorf("output = %q", out)
	}
	if !strings.Contains(out, "skipped") {
		t.Errorf("health should account for the skip:\n%s", out)
	}
}

// The privacy promise is only worth something if erasing is one command.
func TestProbeForgetErasesHistoryButNotTheIndex(t *testing.T) {
	dir := indexedRepo(t)

	runWithInput(t, "\n", "probe", dir)

	code, out, _ := runWithInput(t, "", "probe", "--forget", dir)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "erased") {
		t.Errorf("output = %q", out)
	}

	if _, health, _ := runWithInput(t, "", "probe", "--health", dir); !strings.Contains(health, "No probes answered yet") {
		t.Errorf("history survived --forget:\n%s", health)
	}
	// The index must still be usable.
	if code, _, errOut := runWithInput(t, "", "path", "--top", "3", dir); code != 0 {
		t.Errorf("--forget damaged the index: %d %s", code, errOut)
	}
}

func TestProbeRequiresAnIndex(t *testing.T) {
	dir := fixtureRepo(t)
	code, _, errOut := runWithInput(t, "", "probe", dir)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "lectio index") {
		t.Errorf("error should say how to fix it, got %q", errOut)
	}
}

func TestProbeAppearsInHelp(t *testing.T) {
	_, out, _ := run(t, "--help")
	if !strings.Contains(out, "probe") {
		t.Errorf("probe missing from help:\n%s", out)
	}
}
