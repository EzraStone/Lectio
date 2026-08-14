package core

// EdgeKind records how confident the adapter is about a call edge. The
// distinction is load-bearing: blast radius is graded against ground truth, so
// an edge the adapter only guessed at must be distinguishable from one the
// type checker resolved.
type EdgeKind string

const (
	// EdgeStatic is a call whose callee the type checker resolved exactly.
	EdgeStatic EdgeKind = "static"
	// EdgeDynamic is a call through an interface or function value, resolved by
	// over-approximating analysis (CHA). Sound but imprecise: it may include
	// callees that no execution reaches.
	EdgeDynamic EdgeKind = "dynamic"
)

// CallEdge is a directed dependency: Caller depends on Callee.
//
// Orientation is fixed and everything downstream relies on it. Reversing this
// graph yields "what depends on X", which is the blast-radius question.
type CallEdge struct {
	Caller SymbolID
	Callee SymbolID
	Kind   EdgeKind
	File   string // where the call site lives, repo-relative
	Line   int
}

// ImportEdge is a package-level dependency, used for the static-dependency
// side of the hidden-coupling signal.
type ImportEdge struct {
	From string // package path
	To   string // package path
}

// Confident reports whether the edge is precise enough to grade against.
func (e CallEdge) Confident() bool { return e.Kind == EdgeStatic }
