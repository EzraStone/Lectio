package cli

import (
	"strings"
	"testing"
)

// The Go release is the single most useful diagnostic this tool has: the type
// checker is compiled in, so a binary older than the repository it analyzes
// produces a call graph missing whole packages. It must be printed without
// being asked for.
func TestVersionReportsTheGoRelease(t *testing.T) {
	env, out, _ := testEnv()
	if code := Main(t.Context(), env, []string{"version"}); code != 0 {
		t.Fatalf("exit %d", code)
	}

	got := out.String()
	if !strings.Contains(got, "lectio ") {
		t.Errorf("no version line:\n%s", got)
	}
	if !strings.Contains(got, "built with go1.") {
		t.Errorf("the Go release is missing, which is the line that matters:\n%s", got)
	}
	if !strings.Contains(got, "type checker is compiled in") {
		t.Errorf("no explanation of why the Go release matters:\n%s", got)
	}
}

// The flag spellings used to be a second implementation that printed only the
// version number. They are aliases now, so they cannot drift apart again.
func TestVersionFlagsAreAliasesNotACopy(t *testing.T) {
	for _, spelling := range []string{"--version", "-version"} {
		env, out, _ := testEnv()
		if code := Main(t.Context(), env, []string{spelling}); code != 0 {
			t.Fatalf("%s: exit %d", spelling, code)
		}
		if !strings.Contains(out.String(), "built with go1.") {
			t.Errorf("%s took a different path from `version`:\n%s", spelling, out.String())
		}
	}
}
