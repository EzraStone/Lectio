// Package probe generates questions about a codebase and grades the answers
// against ground truth.
//
// The rule the whole package is built around: nothing here grades by judgment.
// Every correctness decision comes from the call graph, the test suite, or git
// history. That is the property separating this from a chatbot quizzing you,
// and it is enforced structurally rather than by discipline — see StemWriter.
package probe

import (
	"context"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/store"
)

// Kind identifies a probe type.
type Kind string

const (
	// KindBlastRadius is the primary probe: you are changing X, what breaks?
	// Hardest to fake, most useful to have thought about, and the reason the
	// whole thing can be trusted.
	KindBlastRadius Kind = "blast_radius"
	// KindLocate asks which symbol handles a described behavior.
	KindLocate Kind = "locate"
	// KindDirection asks which way a dependency points.
	KindDirection Kind = "direction"
)

// Probe is a generated question together with its ground truth.
type Probe struct {
	Kind Kind
	// Subject is the symbol the question is about.
	Subject core.Symbol
	// Stem is the question as shown.
	Stem string
	// Choices is non-empty for multiple-choice probes.
	Choices []Choice
	// Expected is the ground-truth answer set. Populated only by the
	// generator, only from the index, and never by anything that phrases text.
	Expected []core.SymbolID
	// Provenance names where the truth came from, and is shown after answering.
	// A grader that cannot say why it marked something wrong is one nobody will
	// believe twice.
	Provenance string
}

// Choice is one option in a multiple-choice probe.
type Choice struct {
	Label   string
	Correct bool
}

// Grade is the outcome of one answer.
type Grade struct {
	Outcome store.Outcome
	// Score is in [0,1]: F1 for blast radius, 0 or 1 for the binary probes.
	Score float64

	Precision, Recall float64
	// Named, Missed, and Spurious break the score down so the person can see
	// what they got right rather than just a number.
	Named    []core.SymbolID
	Missed   []core.SymbolID
	Spurious []string

	Explanation string
}

// StemWriter phrases a question.
//
// This is the only seam where a language model may ever be wired in, and its
// signature is the reason the trust property holds. It receives a symbol and
// returns a string. It cannot see the expected answer set, cannot modify it,
// and is called before grading exists — so no phrasing, however creative, can
// influence whether an answer is judged correct. The constraint lives in the
// type, not in a comment asking people to be careful.
type StemWriter interface {
	Stem(ctx context.Context, kind Kind, subject core.Symbol) (string, error)
}

// TemplateStems is the default StemWriter: fixed phrasings, no model, no
// network. It is what ships, and it exists to make the point that the LLM in
// this design is a nicety rather than a dependency.
type TemplateStems struct{}

// Stem returns the standard phrasing for a probe kind.
func (TemplateStems) Stem(_ context.Context, kind Kind, subject core.Symbol) (string, error) {
	switch kind {
	case KindBlastRadius:
		return "You're changing " + subject.ID.Short() + ". What breaks?", nil
	case KindLocate:
		return "Which function handles this?", nil
	case KindDirection:
		return "Which way does this dependency point?", nil
	}
	return "", nil
}

// Generator produces probes of one kind.
type Generator interface {
	Kind() Kind
	// Generate returns a probe for the subject, or ok=false when the subject
	// cannot produce a fair question of this kind.
	Generate(ctx context.Context, g *Context, subject core.Symbol) (Probe, bool)
}

// Grade scores an answer against the probe's ground truth.
func (p Probe) Grade(answer Answer, g *Context) Grade {
	switch p.Kind {
	case KindBlastRadius:
		return p.gradeBlastRadius(answer, g)
	case KindLocate, KindDirection:
		return p.gradeChoice(answer)
	}
	return Grade{Outcome: store.OutcomeSkipped}
}

// Answer is what the person said.
type Answer struct {
	// Names holds free-text tokens for open probes.
	Names []string
	// Choice is the selected index for multiple-choice probes, or -1.
	Choice int
	// Skipped marks a declined probe. A skip is neutral, never a failure.
	Skipped bool
}

// Skip returns a skipped answer.
func Skip() Answer { return Answer{Choice: -1, Skipped: true} }

// gradeChoice grades a multiple-choice probe.
func (p Probe) gradeChoice(a Answer) Grade {
	if a.Skipped {
		return Grade{Outcome: store.OutcomeSkipped}
	}
	if a.Choice < 0 || a.Choice >= len(p.Choices) {
		return Grade{Outcome: store.OutcomeWrong, Explanation: "no option was chosen"}
	}
	if p.Choices[a.Choice].Correct {
		return Grade{Outcome: store.OutcomeCorrect, Score: 1, Explanation: p.Provenance}
	}

	var right string
	for _, c := range p.Choices {
		if c.Correct {
			right = c.Label
			break
		}
	}
	return Grade{
		Outcome:     store.OutcomeWrong,
		Explanation: "the answer is " + right + " — " + p.Provenance,
	}
}
