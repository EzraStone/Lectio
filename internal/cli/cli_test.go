package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testEnv() (*Env, *bytes.Buffer, *bytes.Buffer) {
	var out, errBuf bytes.Buffer
	return &Env{Stdout: &out, Stderr: &errBuf, Stdin: strings.NewReader(""), Color: false}, &out, &errBuf
}

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	env, out, errBuf := testEnv()
	code = Main(context.Background(), env, args)
	return code, out.String(), errBuf.String()
}

func TestHelpListsCommands(t *testing.T) {
	code, out, _ := run(t, "--help")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "index") {
		t.Errorf("help does not list the index command:\n%s", out)
	}
	// The privacy promise belongs where someone will actually see it.
	if !strings.Contains(out, "Nothing leaves your machine") {
		t.Errorf("help omits the privacy statement:\n%s", out)
	}
}

func TestNoArgsShowsHelp(t *testing.T) {
	if code, out, _ := run(t); code != 0 || !strings.Contains(out, "usage:") {
		t.Errorf("bare invocation: code=%d out=%q", code, out)
	}
}

func TestUnknownCommandExitsTwo(t *testing.T) {
	code, _, errOut := run(t, "frobnicate")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != 0 || !strings.Contains(out, "lectio") {
		t.Errorf("version: code=%d out=%q", code, out)
	}
}

func TestIndexRejectsAMissingDirectory(t *testing.T) {
	code, _, errOut := run(t, "index", filepath.Join(t.TempDir(), "nope"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if errOut == "" {
		t.Error("no error reported")
	}
}

func TestIndexRejectsANonGoDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "index", dir)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	// The error should point at the seam rather than just failing.
	if !strings.Contains(errOut, "LanguageAdapter") {
		t.Errorf("stderr = %q, want an explanation of the language limitation", errOut)
	}
}

// fixtureRepo copies the Go fixture module somewhere writable and gives it its
// own git history, so `git log` cannot walk up into this repository's.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	for _, bin := range []string{"go", "git"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	src, err := filepath.Abs(filepath.Join("..", "adapter", "golang", "testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if out, err := exec.Command("cp", "-r", src+"/.", dst).CombinedOutput(); err != nil {
		t.Fatalf("copy fixture: %v\n%s", err, out)
	}

	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "Fixture"},
		{"config", "user.email", "fixture@example.com"},
		{"add", "."},
		{"commit", "-q", "-m", "initial import"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dst
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dst
}

func TestIndexEndToEnd(t *testing.T) {
	dir := fixtureRepo(t)

	code, out, errOut := run(t, "index", dir)
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	for _, want := range []string{"index built", "symbols", "call edges", "next:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".lectio", "index.db")); err != nil {
		t.Errorf("index database not written: %v", err)
	}
}

// -quiet must not be a way to hide a broken index.
func TestQuietStillReportsWarnings(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	src, _ := filepath.Abs(filepath.Join("..", "adapter", "golang", "testdata", "sample"))
	dst := t.TempDir()
	if out, err := exec.Command("cp", "-r", src+"/.", dst).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v\n%s", err, out)
	}

	// No git repo here, so history is unavailable and a warning is due.
	code, out, errOut := run(t, "index", "-quiet", dst)
	if code != 0 {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	if strings.Contains(out, "index built") {
		t.Errorf("-quiet still printed the summary:\n%s", out)
	}
	if !strings.Contains(errOut, "warning:") {
		t.Errorf("-quiet suppressed a warning about a degraded index:\n%s", errOut)
	}
}

func TestStyleIsInertWithoutColor(t *testing.T) {
	env := &Env{Color: false}
	if got := env.bold("x"); got != "x" {
		t.Errorf("styling applied with Color=false: %q", got)
	}
	env.Color = true
	if got := env.bold("x"); !strings.Contains(got, "\x1b[1m") {
		t.Errorf("styling not applied with Color=true: %q", got)
	}
}
