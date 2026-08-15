package core

import "testing"

// The existing TestIsTestFile covers paths that should match. These are the
// near misses — production code whose name merely resembles a test path.
// Excluding one of these would silently drop real code from every reading path.
func TestIsTestFileDoesNotOverreach(t *testing.T) {
	for _, path := range []string{
		"internal/testing/harness.go",
		"protest.go",
		"internal/latest.go",
		"testdata.go",
		"src/contest/main.go",
		"",
	} {
		if IsTestFile(path) {
			t.Errorf("IsTestFile(%q) = true — production code excluded from reading paths", path)
		}
	}
}

func TestSymbolIsTestFollowsItsFile(t *testing.T) {
	if !(Symbol{File: "a_test.go"}).IsTest() {
		t.Error("a symbol in a _test.go file is not marked as a test symbol")
	}
	if (Symbol{File: "a.go"}).IsTest() {
		t.Error("a production symbol was marked as a test symbol")
	}
}

// Grading against CHA-resolved edges marks correct answers wrong: an interface
// with four implementations yields four edges where execution takes one. The
// split between the two graphs rests on this predicate.
func TestOnlyStaticEdgesAreConfident(t *testing.T) {
	if !(CallEdge{Kind: EdgeStatic}).Confident() {
		t.Error("a type-checker-proven edge is not confident")
	}
	if (CallEdge{Kind: EdgeDynamic}).Confident() {
		t.Error("a CHA-resolved edge reported as confident — grading against it marks correct answers wrong")
	}
}
