package core

// CoverageEdge links a test to a symbol it exercises. This is half of blast
// radius ground truth: "what breaks" means both transitive callers and the
// tests that would go red.
type CoverageEdge struct {
	Test   SymbolID
	Symbol SymbolID
	// Fraction is the share of the symbol's statements the test covers, in
	// [0,1]. A test that touches one line of a large function is weaker
	// evidence than one that drives it fully.
	Fraction float64
}

// Covers reports whether the edge is strong enough to count as real coverage.
// The threshold exists because Go's coverage profile attributes a statement to
// every test binary that executed it, including incidental initialization.
func (c CoverageEdge) Covers() bool { return c.Fraction >= 0.10 }
