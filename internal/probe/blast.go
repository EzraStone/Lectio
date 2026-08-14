package probe

import (
	"context"
	"fmt"
	"sort"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/store"
)

// BlastRadius is the primary probe: you are changing X, what breaks?
//
// It is the hardest to fake, the most useful to have thought about, and the
// reason the whole thing can be trusted — the answer set comes from the call
// graph and the test suite, and no part of it is a matter of opinion.
type BlastRadius struct {
	// Depth bounds how far the answer set reaches. Two hops by default.
	Depth int
}

// Kind implements Generator.
func (BlastRadius) Kind() Kind { return KindBlastRadius }

// Answer-set size bounds.
//
// Both exist because of the spec's own stopping rule: if the median answer
// time passes thirty seconds, the probe design is wrong. A symbol with two
// hundred transitive dependents produces a question nobody can answer, and
// grading someone's partial list against it measures stamina. A symbol with
// one dependent produces a question nobody learns from.
const (
	minAnswerSet = 2
	maxAnswerSet = 12
	defaultDepth = 2
)

// Generate builds a blast-radius probe, or declines when the subject cannot
// produce a fair question.
func (b BlastRadius) Generate(ctx context.Context, g *Context, subject core.Symbol) (Probe, bool) {
	depth := b.Depth
	if depth <= 0 {
		depth = defaultDepth
	}

	// Static edges only. An edge CHA over-approximated is one no execution may
	// take, and marking someone wrong for not naming it would break exactly
	// the property this probe exists to establish.
	dependents := g.View.BlastRadius(subject.ID, depth)
	tests := g.View.CoveringTests(subject.ID)

	expected := make([]core.SymbolID, 0, len(dependents)+len(tests))
	for id := range dependents {
		expected = append(expected, id)
	}
	expected = append(expected, tests...)
	sort.Slice(expected, func(i, j int) bool { return expected[i] < expected[j] })

	if len(expected) < minAnswerSet || len(expected) > maxAnswerSet {
		return Probe{}, false
	}

	stem, err := g.Stems.Stem(ctx, KindBlastRadius, subject)
	if err != nil || stem == "" {
		stem = "You're changing " + subject.ID.Short() + ". What breaks?"
	}

	provenance := fmt.Sprintf("transitive dependents in the call graph, to %d hops", depth)
	if len(tests) > 0 {
		provenance += ", plus covering tests"
	}

	return Probe{
		Kind:       KindBlastRadius,
		Subject:    subject,
		Stem:       stem,
		Expected:   expected,
		Provenance: provenance,
	}, true
}

// gradeBlastRadius scores an answer by F1 against the real dependent set.
func (p Probe) gradeBlastRadius(a Answer, g *Context) Grade {
	if a.Skipped {
		return Grade{Outcome: store.OutcomeSkipped, Missed: p.Expected}
	}

	expected := make(map[core.SymbolID]bool, len(p.Expected))
	for _, id := range p.Expected {
		expected[id] = true
	}

	named := make(map[core.SymbolID]bool)
	var spurious []string
	var resolvedTokens int

	for _, token := range a.Names {
		if normalize(token) == "" {
			continue
		}
		candidates := g.Resolve(token)
		if len(candidates) == 0 {
			// Named something that does not exist in this repo.
			spurious = append(spurious, token)
			resolvedTokens++
			continue
		}

		var hit bool
		for _, id := range candidates {
			if expected[id] {
				named[id] = true
				hit = true
			}
		}
		resolvedTokens++
		if !hit {
			spurious = append(spurious, token)
		}
	}

	var precision, recall, f1 float64
	if resolvedTokens > 0 {
		precision = float64(len(named)) / float64(resolvedTokens)
	}
	if len(expected) > 0 {
		recall = float64(len(named)) / float64(len(expected))
	}
	if precision+recall > 0 {
		f1 = 2 * precision * recall / (precision + recall)
	}

	grade := Grade{
		Score:     f1,
		Precision: precision,
		Recall:    recall,
		Spurious:  spurious,
	}
	for id := range named {
		grade.Named = append(grade.Named, id)
	}
	for _, id := range p.Expected {
		if !named[id] {
			grade.Missed = append(grade.Missed, id)
		}
	}
	sort.Slice(grade.Named, func(i, j int) bool { return grade.Named[i] < grade.Named[j] })

	// Thresholds, not a pass mark. The point of this probe is to make someone
	// think about the blast radius before they change something; a near-miss
	// that named most of it has achieved that, and calling it a failure would
	// teach people to skip rather than guess.
	switch {
	case f1 >= 0.8:
		grade.Outcome = store.OutcomeCorrect
	case f1 >= 0.4:
		grade.Outcome = store.OutcomePartial
	default:
		grade.Outcome = store.OutcomeWrong
	}

	grade.Explanation = explainBlast(grade, p)
	return grade
}

func explainBlast(grade Grade, p Probe) string {
	switch {
	case len(grade.Missed) == 0 && len(grade.Spurious) == 0:
		return "all " + plural(len(p.Expected), "dependent") + " — " + p.Provenance
	case len(grade.Missed) == 0:
		return "everything that breaks, plus " + plural(len(grade.Spurious), "thing") + " that does not"
	case len(grade.Named) == 0:
		return "the answer is " + joinShort(p.Expected) + " — " + p.Provenance
	default:
		return "missed " + joinShort(grade.Missed) + " — " + p.Provenance
	}
}

func joinShort(ids []core.SymbolID) string {
	const maxListed = 6
	out := ""
	for i, id := range ids {
		if i == maxListed {
			out += fmt.Sprintf(", and %d more", len(ids)-maxListed)
			break
		}
		if i > 0 {
			out += ", "
		}
		out += id.Short()
	}
	return out
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
