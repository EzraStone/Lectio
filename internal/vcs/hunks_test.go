package vcs

import (
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

func TestParseHunkHeader(t *testing.T) {
	for _, tc := range []struct {
		line string
		want core.LineRange
		ok   bool
	}{
		{"@@ -12,3 +14,5 @@ func Parse() {", core.LineRange{Start: 14, Count: 5}, true},
		// Git omits the count for a single line. Reading that as zero would
		// turn every one-line change into a deletion.
		{"@@ -1 +1 @@", core.LineRange{Start: 1, Count: 1}, true},
		{"@@ -1,4 +1 @@", core.LineRange{Start: 1, Count: 1}, true},
		// A pure deletion: zero lines on the new side.
		{"@@ -20,6 +19,0 @@ func Gone() {", core.LineRange{Start: 19, Count: 0}, true},
		{"not a hunk header", core.LineRange{}, false},
		{"@@ garbage @@", core.LineRange{}, false},
	} {
		got, ok := parseHunkHeader(tc.line)
		if ok != tc.ok {
			t.Errorf("parseHunkHeader(%q) ok = %v, want %v", tc.line, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("parseHunkHeader(%q) = %+v, want %+v", tc.line, got, tc.want)
		}
	}
}

func TestParseHunksGroupsByFile(t *testing.T) {
	diff := `diff --git a/rank/score.go b/rank/score.go
index 1111111..2222222 100644
--- a/rank/score.go
+++ b/rank/score.go
@@ -10,2 +10,3 @@ func Rank() {
@@ -40,0 +41,5 @@ func Select() {
diff --git a/probe/grade.go b/probe/grade.go
index 3333333..4444444 100644
--- a/probe/grade.go
+++ b/probe/grade.go
@@ -7,4 +7,0 @@ func Grade() {
`
	got := parseHunks([]byte(diff))
	if len(got) != 2 {
		t.Fatalf("parsed %d files, want 2: %+v", len(got), got)
	}
	if want := []core.LineRange{{Start: 10, Count: 3}, {Start: 41, Count: 5}}; len(got["rank/score.go"]) != 2 ||
		got["rank/score.go"][0] != want[0] || got["rank/score.go"][1] != want[1] {
		t.Errorf("rank/score.go = %+v, want %+v", got["rank/score.go"], want)
	}
	if r := got["probe/grade.go"]; len(r) != 1 || r[0] != (core.LineRange{Start: 7, Count: 0}) {
		t.Errorf("probe/grade.go = %+v, want one zero-count range at 7", r)
	}
}

// A deleted file's new-side path is /dev/null, which names nothing. Its hunks
// must not be filed under an empty key or under the previous file's name.
func TestParseHunksDropsDeletedFiles(t *testing.T) {
	diff := `diff --git a/gone.go b/gone.go
--- a/gone.go
+++ /dev/null
@@ -1,20 +0,0 @@
`
	got := parseHunks([]byte(diff))
	if len(got) != 0 {
		t.Errorf("a deleted file produced ranges: %+v", got)
	}
}

// Git's diff output carries mode changes, binary notices and similarity
// indexes. A parser that rejected a commit on meeting one would throw away
// real evidence to no purpose.
func TestParseHunksSkipsNoiseWithoutFailing(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
old mode 100644
new mode 100755
similarity index 94%
Binary files a/logo.png and b/logo.png differ
--- a/a.go
+++ b/a.go
@@ -3,1 +3,2 @@
\ No newline at end of file
`
	got := parseHunks([]byte(diff))
	if r := got["a.go"]; len(r) != 1 || r[0] != (core.LineRange{Start: 3, Count: 2}) {
		t.Errorf("a.go = %+v, want one range at 3 of length 2", r)
	}
}

func TestTrimDiffPath(t *testing.T) {
	for in, want := range map[string]string{
		"b/internal/rank/score.go": "internal/rank/score.go",
		"/dev/null":                "",
		"b/a b/c.go":               "a b/c.go",
	} {
		if got := trimDiffPath(in); got != want {
			t.Errorf("trimDiffPath(%q) = %q, want %q", in, got, want)
		}
	}
}
