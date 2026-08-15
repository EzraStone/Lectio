package golang

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// RequiredGo reads the language version a module asks for, from its go.mod.
//
// Returns an empty string when there is no go.mod or no go directive, which is
// a fact about the repository rather than an error — a GOPATH-era tree or a
// bare directory of Go files is still indexable.
func RequiredGo(root string) string {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	defer f.Close()

	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		// "go 1.25.0" or "toolchain go1.25.0"; the go directive is what the
		// type checker is asked to accept, so it is the one that matters.
		if rest, ok := strings.CutPrefix(line, "go "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// TooOld reports whether the running binary's Go release is older than the
// language version a module requires.
//
// This is the failure the README warns about and the warning never named. The
// type checker is compiled into the binary, so a 1.24 build cannot type-check
// a package declaring go 1.25 — it does not fail loudly, it drops every
// package in the module. On go-git that was the difference between 19,005 call
// edges and 36,615, which quietly changes every centrality number in the
// ranking.
//
// Conservative on anything it cannot parse: an unrecognized version returns
// false rather than warning about a problem that may not exist.
func TooOld(required, running string) bool {
	rq, ok := parseGoVersion(required)
	if !ok {
		return false
	}
	rn, ok := parseGoVersion(running)
	if !ok {
		return false
	}
	if rn[0] != rq[0] {
		return rn[0] < rq[0]
	}
	return rn[1] < rq[1]
}

// RunningGo is the Go release this binary was built with, as "1.25.0".
func RunningGo() string { return strings.TrimPrefix(runtime.Version(), "go") }

// parseGoVersion reads the major and minor of a Go version string, ignoring
// the patch and any suffix like "rc1".
func parseGoVersion(s string) ([2]int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "go")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 2 {
		return [2]int{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return [2]int{}, false
	}
	minor, err := strconv.Atoi(trimSuffixNonDigits(parts[1]))
	if err != nil {
		return [2]int{}, false
	}
	return [2]int{major, minor}, true
}

func trimSuffixNonDigits(s string) string {
	for i, r := range s {
		if r < '0' || r > '9' {
			return s[:i]
		}
	}
	return s
}
