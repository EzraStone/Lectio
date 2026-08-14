package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tinyManifest writes a valid corpus to a temp path.
func tinyManifest(t *testing.T, rev string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corpus.json")
	body := map[string]any{
		"version": 1,
		"repos": []map[string]string{
			{"name": "fixture/one", "url": "https://example.com/one.git", "rev": rev, "note": "test"},
		},
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCorpusStatusOnAnUncachedManifest(t *testing.T) {
	path := tinyManifest(t, "0123456789abcdef0123456789abcdef01234567")

	code, out, errOut := run(t, "corpus", "--manifest", path, "--cache", t.TempDir())
	if code != 0 {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	for _, want := range []string{"fixture/one", "absent", "0 ready, 0 stale, 0 unpinned", "corpus fetch"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCorpusStatusFlagsUnpinned(t *testing.T) {
	path := tinyManifest(t, "")

	_, out, _ := run(t, "corpus", "status", "--manifest", path, "--cache", t.TempDir())
	if !strings.Contains(out, "unpinned") || !strings.Contains(out, "corpus pin") {
		t.Errorf("an unpinned repo should say how to fix it:\n%s", out)
	}
}

// Fetching an unpinned corpus would produce a number nobody can reproduce.
func TestCorpusFetchRefusesUnpinned(t *testing.T) {
	path := tinyManifest(t, "")

	code, _, errOut := run(t, "corpus", "fetch", "--manifest", path, "--cache", t.TempDir())
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "corpus pin") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestCorpusRejectsUnknownSubcommand(t *testing.T) {
	path := tinyManifest(t, "0123456789abcdef0123456789abcdef01234567")

	code, _, errOut := run(t, "corpus", "frobnicate", "--manifest", path)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "unknown subcommand") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestCorpusReportsAMissingManifest(t *testing.T) {
	code, _, errOut := run(t, "corpus", "--manifest", filepath.Join(t.TempDir(), "nope.json"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "read corpus") {
		t.Errorf("stderr = %q", errOut)
	}
}

// The shipped corpus must load and be fully pinned, or Gate A cannot be run
// reproducibly out of the box.
func TestShippedCorpusIsPinnedAndReadable(t *testing.T) {
	path := filepath.Join("..", "..", "corpus", "gate-a.json")

	code, out, errOut := run(t, "corpus", "status", "--manifest", path, "--cache", t.TempDir())
	if code != 0 {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	if !strings.Contains(out, "30 repositories") {
		t.Errorf("expected thirty repositories:\n%s", out)
	}
	// The summary line always contains the word, so check the per-repo rows.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  ") && strings.Contains(line, "/") && strings.Contains(line, "unpinned") {
			t.Errorf("the shipped corpus has an unpinned entry: %q", line)
		}
	}
	if !strings.Contains(out, "0 unpinned") {
		t.Errorf("summary should report nothing unpinned:\n%s", out)
	}
}

func TestPluralize(t *testing.T) {
	cases := map[int]string{1: "1 repository", 2: "2 repositories"}
	for n, want := range cases {
		if got := pluralize(n, "repository"); got != want {
			t.Errorf("pluralize(%d, repository) = %q, want %q", n, got, want)
		}
	}
	if got := pluralize(3, "case"); got != "3 cases" {
		t.Errorf("pluralize(3, case) = %q", got)
	}
}

func TestCorpusAppearsInHelp(t *testing.T) {
	_, out, _ := run(t, "--help")
	if !strings.Contains(out, "corpus") {
		t.Errorf("corpus missing from help:\n%s", out)
	}
}

// Go's flag package stops at the first positional, so parsing before taking
// the subcommand would make "corpus fetch --manifest x" silently read the
// default corpus instead — the exact failure this command exists to prevent.
func TestCorpusFlagsWorkAfterTheSubcommand(t *testing.T) {
	path := tinyManifest(t, "0123456789abcdef0123456789abcdef01234567")

	code, out, errOut := run(t, "corpus", "status", "--manifest", path, "--cache", t.TempDir())
	if code != 0 {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	if !strings.Contains(out, "fixture/one") {
		t.Errorf("the flag after the subcommand was ignored:\n%s", out)
	}

	// And before it, which is what the flag package handles natively.
	code, out, _ = run(t, "corpus", "--manifest", path, "--cache", t.TempDir(), "status")
	if code != 0 || !strings.Contains(out, "fixture/one") {
		t.Errorf("flags before the subcommand: code=%d\n%s", code, out)
	}
}
