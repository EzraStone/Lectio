package probe

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"

	"github.com/EzraStone/Lectio/internal/core"
)

// Locate asks which function handles a described behavior.
//
// Ten seconds to answer. Cheap to generate, cheap to grade, and a fast read on
// whether someone has a map in their head at all — which is a different
// question from whether they understand any particular function, and worth
// asking separately.
type Locate struct{}

// Kind implements Generator.
func (Locate) Kind() Kind { return KindLocate }

// distractorCount is how many wrong options accompany the right one.
const distractorCount = 3

// Generate builds a multiple-choice locate probe.
//
// The subject needs a doc comment: without one there is nothing to describe
// the behavior with, and inventing a description would mean a model writing
// something the grader then treats as truth. Declining is the honest move.
func (Locate) Generate(ctx context.Context, g *Context, subject core.Symbol) (Probe, bool) {
	if subject.Doc == "" || subject.Kind == core.KindConst || subject.Kind == core.KindVar {
		return Probe{}, false
	}

	distractors := pickDistractors(g, subject, distractorCount)
	if len(distractors) < distractorCount {
		return Probe{}, false
	}

	choices := make([]Choice, 0, distractorCount+1)
	choices = append(choices, Choice{Label: symbolLabel(subject), Correct: true})
	for _, d := range distractors {
		choices = append(choices, Choice{Label: symbolLabel(d)})
	}
	shuffle(choices, string(subject.ID))

	return Probe{
		Kind:       KindLocate,
		Subject:    subject,
		Stem:       fmt.Sprintf("Which one does this: %q", subject.Doc),
		Choices:    choices,
		Expected:   []core.SymbolID{subject.ID},
		Provenance: "the code — " + subject.File,
	}, true
}

// pickDistractors chooses plausible wrong answers.
//
// Same package where possible. A distractor drawn from across the repo is
// eliminated by elimination rather than by knowledge — someone who has never
// read either function can rule out "ui.Render" for a question about billing,
// and then the probe has measured nothing.
func pickDistractors(g *Context, subject core.Symbol, n int) []core.Symbol {
	var sameKind, samePackage, others []core.Symbol

	for _, sym := range g.ReadableSymbols() {
		if sym.ID == subject.ID {
			continue
		}
		switch {
		case sym.Package == subject.Package && sym.Kind == subject.Kind:
			sameKind = append(sameKind, sym)
		case sym.Package == subject.Package:
			samePackage = append(samePackage, sym)
		default:
			others = append(others, sym)
		}
	}

	pool := append(append(sameKind, samePackage...), others...)
	if len(pool) > n {
		pool = pool[:n]
	}
	return pool
}

// shuffle permutes choices deterministically from a seed.
//
// Deterministic because the same probe must look the same if it is shown
// twice, and because a test that cannot predict option order cannot assert
// anything about it. Seeded by the subject so different probes differ.
func shuffle(choices []Choice, seed string) {
	h := fnv.New32a()
	h.Write([]byte(seed))
	key := h.Sum32()

	sort.SliceStable(choices, func(i, j int) bool {
		return hashOrder(choices[i].Label, key) < hashOrder(choices[j].Label, key)
	})
}

func hashOrder(label string, key uint32) uint32 {
	h := fnv.New32a()
	h.Write([]byte(label))
	return h.Sum32() ^ key
}
