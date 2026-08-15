package golang

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequiredGoReadsTheDirective(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/x\n\ngo 1.25.0\n\nrequire foo v1.0.0\n"), 0o644)

	if got := RequiredGo(dir); got != "1.25.0" {
		t.Errorf("RequiredGo = %q, want 1.25.0", got)
	}
}

// A GOPATH-era tree or a bare directory of Go files is still indexable, so a
// missing go.mod is a fact rather than an error.
func TestRequiredGoIsSilentWithoutAModule(t *testing.T) {
	if got := RequiredGo(t.TempDir()); got != "" {
		t.Errorf("RequiredGo on a non-module = %q, want empty", got)
	}
}

// This is the failure the README warns about and the warning never named: a
// 1.24 binary drops every package in a go-1.25 module, silently.
func TestTooOldCatchesTheCompiledInTypeChecker(t *testing.T) {
	for _, tc := range []struct {
		required, running string
		want              bool
	}{
		{"1.25", "1.24.7", true},
		{"1.25.0", "1.24", true},
		{"1.24", "1.25.0", false},
		{"1.24", "1.24.7", false},
		{"2.0", "1.25", true},
		{"1.25", "2.0", false},
		// Unparseable input must never produce a warning about a problem that
		// may not exist.
		{"", "1.24", false},
		{"tip", "1.24", false},
		{"1.25", "", false},
		{"1.x", "1.24", false},
	} {
		if got := TooOld(tc.required, tc.running); got != tc.want {
			t.Errorf("TooOld(required %q, running %q) = %v, want %v",
				tc.required, tc.running, got, tc.want)
		}
	}
}

func TestRunningGoHasNoPrefix(t *testing.T) {
	got := RunningGo()
	if got == "" || got[0] < '0' || got[0] > '9' {
		t.Errorf("RunningGo = %q, want a bare version like 1.25.0", got)
	}
}
