package cli

import (
	"context"
	"strings"
	"testing"
)

// A count of zero used to be a third, quieter way to do nothing: no output, no
// error, exit zero. The flags that stop early say so and stop.
func TestProbeRejectsANonPositiveCount(t *testing.T) {
	for _, n := range []string{"0", "-2"} {
		env, out, _ := testEnv()
		err := runProbe(context.Background(), env, []string{"-n", n, t.TempDir()})
		if err == nil {
			t.Errorf("-n %s was accepted with output %q", n, out.String())
			continue
		}
		if !strings.Contains(err.Error(), "asks for no probes") {
			t.Errorf("-n %s gave %q, which does not explain the refusal", n, err)
		}
	}
}

// The early-exit flags do their own thing and must not be blocked by a count
// they never read.
func TestProbeCountGuardDoesNotBlockTheStopFlags(t *testing.T) {
	for _, flag := range []string{"--forget", "--health"} {
		env, _, _ := testEnv()
		err := runProbe(context.Background(), env, []string{flag, "-n", "0", t.TempDir()})
		if err != nil && strings.Contains(err.Error(), "asks for no probes") {
			t.Errorf("%s was blocked by the count guard: %v", flag, err)
		}
	}
}

// Zero depth means "follow every hop", which is why this is not the same guard
// the other counts get. Negative means nothing, and was silently producing a
// bounded walk of some unstated depth.
func TestDepsRejectsANegativeDepth(t *testing.T) {
	env, _, _ := testEnv()
	err := runDeps(context.Background(), env, []string{"--depth", "-1", "Anything", t.TempDir()})
	if err == nil {
		t.Fatal("--depth -1 was accepted")
	}
	if !strings.Contains(err.Error(), "use 0 to follow all of them") {
		t.Errorf("the error does not say what zero means: %v", err)
	}
}

func TestDepsAcceptsZeroDepth(t *testing.T) {
	env, _, _ := testEnv()
	err := runDeps(context.Background(), env, []string{"--depth", "0", "Anything", t.TempDir()})
	if err != nil && strings.Contains(err.Error(), "not a number of hops") {
		t.Errorf("--depth 0 was rejected, but zero means follow all: %v", err)
	}
}

// The same shape on path, which is where this class of bug was first found:
// --top 0 reported "the index is thin" about a repository that was fine.
func TestPathRejectsANonPositiveTopAndMonths(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--top", "0"}, "asks for no items"},
		{[]string{"--top", "-3"}, "asks for no items"},
		{[]string{"--months", "0"}, "is not a history window"},
		{[]string{"--months", "-6"}, "is not a history window"},
	} {
		env, _, _ := testEnv()
		err := runPath(context.Background(), env, append(tc.args, t.TempDir()))
		if err == nil {
			t.Errorf("%v was accepted", tc.args)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v gave %q, want it to mention %q", tc.args, err, tc.want)
		}
	}
}

// And the defaults still work, which is what these guards must not have broken.
func TestTheDefaultsAreNotRejected(t *testing.T) {
	env, _, _ := testEnv()
	// A directory with no index is the expected failure; a flag complaint is not.
	err := runPath(context.Background(), env, []string{t.TempDir()})
	for _, phrase := range []string{"asks for no items", "is not a history window"} {
		if err != nil && strings.Contains(err.Error(), phrase) {
			t.Errorf("a default run was rejected: %v", err)
		}
	}
}
