package cli

import (
	"context"
	"strings"
	"testing"
)

// Go's flag package stops parsing at the first non-flag argument, so a flag
// written after the path is silently ignored — the user asks for three items
// and gets ten with nothing to say why. This is the one CLI mistake that
// produces a wrong answer rather than an error.
func TestFlagsAfterAPathAreRejected(t *testing.T) {
	for _, args := range [][]string{
		{".", "--top", "3"},
		{".", "-top", "3"},
		{".", "--explain"},
		{"some/repo", "--json"},
	} {
		err := checkTrailingFlags(args)
		if err == nil {
			t.Errorf("%v was accepted", args)
			continue
		}
		if !strings.Contains(err.Error(), "came after a positional argument") {
			t.Errorf("%v gave %q, which does not explain the problem", args, err)
		}
		if !strings.Contains(err.Error(), "put flags first") {
			t.Errorf("%v gave %q, which does not say what to do instead", args, err)
		}
	}
}

func TestOrdinaryArgumentsAreAccepted(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"."},
		{"some/repo"},
		{"a/one", "b/two", "c/three"},
		{"-"},                  // a bare dash is stdin by convention, not a flag
		{"./-weird-dir"},       // starts with a dot, not a dash
		{"--", "--not-a-flag"}, // everything after -- is positional
	} {
		if err := checkTrailingFlags(args); err != nil {
			t.Errorf("%v was rejected: %v", args, err)
		}
	}
}

// The error has to reach the user rather than being swallowed, on every
// command that takes a path.
func TestPathRejectsATrailingFlag(t *testing.T) {
	env, _, _ := testEnv()
	err := runPath(context.Background(), env, []string{".", "--top", "3"})
	if err == nil {
		t.Fatal("lectio path . --top 3 was accepted")
	}
	if !strings.Contains(err.Error(), "--top") {
		t.Errorf("the error does not name the flag: %v", err)
	}
}

func TestBacktestRejectsATrailingFlag(t *testing.T) {
	env, _, _ := testEnv()
	err := runBacktest(context.Background(), env, []string{".", "--cases", "3"})
	if err == nil {
		t.Fatal("lectio backtest . --cases 3 was accepted")
	}
	if !strings.Contains(err.Error(), "--cases") {
		t.Errorf("the error does not name the flag: %v", err)
	}
}

// compare reports a trailing flag as a trailing flag rather than as a third
// report file, which is what the argument count alone would have said.
func TestCompareRejectsATrailingFlagByName(t *testing.T) {
	env, _, _ := testEnv()
	err := runCompare(context.Background(), env, []string{"a.json", "b.json", "--json"})
	if err == nil {
		t.Fatal("lectio compare a.json b.json --json was accepted")
	}
	if !strings.Contains(err.Error(), "--json") {
		t.Errorf("the error blames the count rather than the flag: %v", err)
	}
}

// Flags in the right place still work, which is the behaviour this must not
// have broken.
func TestFlagsBeforeThePathStillWork(t *testing.T) {
	env, _, _ := testEnv()
	// A missing repository is the expected failure here; a flag complaint is
	// not.
	err := runPath(context.Background(), env, []string{"--top", "3", t.TempDir()})
	if err != nil && strings.Contains(err.Error(), "came after a positional argument") {
		t.Errorf("a correctly-placed flag was rejected: %v", err)
	}
}
