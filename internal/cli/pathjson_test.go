package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// The JSON is what another tool reads, and it should not carry a conflation
// the human output no longer has.
func TestPathJSONSplitsTheTwoSilences(t *testing.T) {
	env, out, _ := testEnv()
	if err := runPath(t.Context(), env, []string{"--json", "--top", "5", repoRoot(t)}); err != nil {
		t.Fatalf("path --json: %v", err)
	}

	var got jsonOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}

	if len(got.Silent) != len(got.Unavailable)+len(got.Empty) {
		t.Errorf("silent has %d, unavailable %d, empty %d — they do not partition it",
			len(got.Silent), len(got.Unavailable), len(got.Empty))
	}
	// Every classified signal is in the union, so a consumer written against
	// the older shape still reads the same thing.
	in := func(hay []string, needle string) bool {
		for _, s := range hay {
			if s == needle {
				return true
			}
		}
		return false
	}
	for _, s := range append(append([]string{}, got.Unavailable...), got.Empty...) {
		if !in(got.Silent, s) {
			t.Errorf("%s is classified but missing from silent_signals", s)
		}
	}
	for _, s := range got.Active {
		if in(got.Silent, s) {
			t.Errorf("%s is both active and silent", s)
		}
	}
}

// A path of ten across one file and one across seven are different objects,
// and nothing else in the output tells them apart.
func TestPathJSONReportsHowManyFilesItSpans(t *testing.T) {
	env, out, _ := testEnv()
	if err := runPath(t.Context(), env, []string{"--json", "--top", "10", repoRoot(t)}); err != nil {
		t.Fatalf("path --json: %v", err)
	}

	var got jsonOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if len(got.Items) == 0 {
		t.Skip("this repository produced no reading path")
	}

	files := map[string]bool{}
	for _, it := range got.Items {
		files[it.File] = true
	}
	if got.Files != len(files) {
		t.Errorf("files_spanned is %d, but the items span %d", got.Files, len(files))
	}
	if got.Files < 2 && len(got.Items) >= 4 {
		t.Errorf("a path of %d items spans %d files — the spread rule is not applying",
			len(got.Items), got.Files)
	}
}

// repoRoot finds this repository, which has an index built by the suite's
// other tests or by a developer running the tool. Skips when there is none
// rather than building one, since indexing takes tens of seconds.
func repoRoot(t *testing.T) string {
	t.Helper()
	root := "../.."
	env, out, _ := testEnv()
	if err := runPath(t.Context(), env, []string{"--json", "--top", "1", root}); err != nil {
		if strings.Contains(err.Error(), "no index") {
			t.Skip("no index for this repository; run: lectio index .")
		}
		t.Fatalf("path: %v", err)
	}
	_ = out
	return root
}
