package probe

import (
	"context"
	"fmt"

	"github.com/EzraStone/Lectio/internal/core"
)

// Direction asks which way a dependency points.
//
// Trivially gradable, and the failure it catches is the expensive one: a wrong
// answer reveals an inverted mental model, which is what produces bad
// architecture decisions months later. Someone who believes the queue calls
// the scheduler will eventually put retry logic in the wrong place, and by
// then it is a refactor rather than a correction.
type Direction struct{}

// Kind implements Generator.
func (Direction) Kind() Kind { return KindDirection }

// Generate builds a two-choice direction probe about the subject and one of
// its dependencies.
func (Direction) Generate(ctx context.Context, g *Context, subject core.Symbol) (Probe, bool) {
	callee, ok := pickCallee(g, subject)
	if !ok {
		return Probe{}, false
	}

	a, b := subject.ID.Short(), callee.ID.Short()

	// Both options are stated in full so neither reads as the default. An
	// asymmetric pair ("does A call B, or the reverse?") lets someone pick the
	// first clause when unsure, which turns the probe into a coin flip with a
	// bias rather than a question.
	choices := []Choice{
		{Label: fmt.Sprintf("%s calls %s", a, b), Correct: true},
		{Label: fmt.Sprintf("%s calls %s", b, a)},
	}
	shuffle(choices, string(subject.ID)+string(callee.ID))

	return Probe{
		Kind:       KindDirection,
		Subject:    subject,
		Stem:       fmt.Sprintf("Which way does this dependency go, between %s and %s?", a, b),
		Choices:    choices,
		Expected:   []core.SymbolID{callee.ID},
		Provenance: "the call graph",
	}, true
}

// pickCallee returns a dependency of the subject that is unambiguous.
//
// The pair must not be mutually recursive: when A calls B and B calls A, both
// options are true and there is no answer to be wrong about. Only static edges
// count, for the same reason grading uses them — a CHA guess is not a fact to
// examine anyone on.
func pickCallee(g *Context, subject core.Symbol) (core.Symbol, bool) {
	i, ok := g.View.StaticCalls.Index(string(subject.ID))
	if !ok {
		return core.Symbol{}, false
	}

	for _, j := range g.View.StaticCalls.Out(i) {
		calleeID := core.SymbolID(g.View.StaticCalls.ID(j))
		callee, known := g.View.Symbols[calleeID]
		if !known || callee.IsTest() || calleeID == subject.ID {
			continue
		}
		if callsBack(g, j, i) {
			continue
		}
		return callee, true
	}
	return core.Symbol{}, false
}

// callsBack reports whether there is a direct edge from j back to i.
func callsBack(g *Context, j, i int) bool {
	for _, back := range g.View.StaticCalls.Out(j) {
		if back == i {
			return true
		}
	}
	return false
}
