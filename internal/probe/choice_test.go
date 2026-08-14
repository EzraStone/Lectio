package probe

import (
	"context"
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/store"
)

func documentedView() *Context {
	v := specView()
	for id, sym := range v.Symbols {
		sym.Doc = "handles " + sym.Name
		sym.Package = string(id.Package())
		v.Symbols[id] = sym
	}
	return NewContext(v, nil)
}

func correctIndex(p Probe) int {
	for i, c := range p.Choices {
		if c.Correct {
			return i
		}
	}
	return -1
}

// ------------------------------------------------------------------ locate --

func TestLocateGeneratesAChoiceQuestion(t *testing.T) {
	g := documentedView()
	p, ok := Locate{}.Generate(context.Background(), g, g.View.Symbols["mod/billing.Cycle"])
	if !ok {
		t.Fatal("no locate probe generated")
	}

	if len(p.Choices) != distractorCount+1 {
		t.Errorf("choices = %d, want %d", len(p.Choices), distractorCount+1)
	}
	if correctIndex(p) < 0 {
		t.Fatal("no correct option")
	}
	if !strings.Contains(p.Stem, "handles Cycle") {
		t.Errorf("stem = %q, should quote the doc comment", p.Stem)
	}
	if !strings.Contains(p.Provenance, "billing/cycle.go") {
		t.Errorf("provenance = %q, should name the file", p.Provenance)
	}
}

// Without a doc comment there is nothing to describe the behavior with, and
// inventing one would mean a model writing text the grader then treats as
// truth.
func TestLocateDeclinesUndocumentedSymbols(t *testing.T) {
	g := NewContext(specView(), nil) // no docs anywhere
	if _, ok := (Locate{}).Generate(context.Background(), g, g.View.Symbols["mod/billing.Cycle"]); ok {
		t.Error("generated a locate probe for a symbol with no doc comment")
	}
}

func TestLocateGrading(t *testing.T) {
	g := documentedView()
	p, _ := Locate{}.Generate(context.Background(), g, g.View.Symbols["mod/billing.Cycle"])

	right := p.Grade(Answer{Choice: correctIndex(p)}, g)
	if right.Outcome != store.OutcomeCorrect || right.Score != 1 {
		t.Errorf("correct answer graded %q / %v", right.Outcome, right.Score)
	}

	wrongIdx := (correctIndex(p) + 1) % len(p.Choices)
	wrong := p.Grade(Answer{Choice: wrongIdx}, g)
	if wrong.Outcome != store.OutcomeWrong {
		t.Errorf("wrong answer graded %q", wrong.Outcome)
	}
	// Being told you are wrong without being told the answer teaches nothing.
	if !strings.Contains(wrong.Explanation, p.Choices[correctIndex(p)].Label) {
		t.Errorf("explanation = %q, should name the right answer", wrong.Explanation)
	}

	if skipped := p.Grade(Skip(), g); skipped.Outcome != store.OutcomeSkipped {
		t.Errorf("skip graded %q", skipped.Outcome)
	}
	if bad := p.Grade(Answer{Choice: 99}, g); bad.Outcome != store.OutcomeWrong {
		t.Errorf("out-of-range choice graded %q", bad.Outcome)
	}
}

// A distractor from across the repo is eliminated by elimination rather than
// by knowledge, and then the probe has measured nothing.
func TestLocateDistractorsComeFromTheSamePackage(t *testing.T) {
	v := specView()
	for i := 0; i < 6; i++ {
		id := core.SymbolID("mod/billing.Helper" + string(rune('A'+i)))
		v.Symbols[id] = core.Symbol{
			ID: id, Name: "Helper" + string(rune('A'+i)), Kind: core.KindFunc,
			File: "billing/helpers.go", Package: "mod/billing", Doc: "helps",
		}
		v.Calls.Add(string(id))
		v.StaticCalls.Add(string(id))
	}
	subject := v.Symbols["mod/billing.Cycle"]
	subject.Doc = "computes a billing cycle"
	subject.Package = "mod/billing"
	v.Symbols["mod/billing.Cycle"] = subject

	g := NewContext(v, nil)
	p, ok := Locate{}.Generate(context.Background(), g, subject)
	if !ok {
		t.Fatal("no probe generated")
	}
	for _, c := range p.Choices {
		if c.Correct {
			continue
		}
		if !strings.HasPrefix(c.Label, "billing.") {
			t.Errorf("distractor %q is not from the subject's package", c.Label)
		}
	}
}

// The same probe shown twice must look the same.
func TestChoiceOrderIsDeterministic(t *testing.T) {
	g := documentedView()
	first, _ := Locate{}.Generate(context.Background(), g, g.View.Symbols["mod/billing.Cycle"])

	for i := 0; i < 10; i++ {
		got, _ := Locate{}.Generate(context.Background(), g, g.View.Symbols["mod/billing.Cycle"])
		for j := range got.Choices {
			if got.Choices[j] != first.Choices[j] {
				t.Fatalf("run %d differs at choice %d: %+v vs %+v", i, j, got.Choices[j], first.Choices[j])
			}
		}
	}
}

// --------------------------------------------------------------- direction --

func TestDirectionGeneratesATwoWayQuestion(t *testing.T) {
	g := documentedView()
	p, ok := Direction{}.Generate(context.Background(), g, g.View.Symbols["mod/scheduler.Next"])
	if !ok {
		t.Fatal("no direction probe generated")
	}

	if len(p.Choices) != 2 {
		t.Fatalf("choices = %d, want 2", len(p.Choices))
	}
	// Both options must be full statements, so neither reads as the default.
	for _, c := range p.Choices {
		if !strings.Contains(c.Label, " calls ") {
			t.Errorf("choice %q is not a complete statement", c.Label)
		}
	}
	right := p.Choices[correctIndex(p)].Label
	if right != "scheduler.Next calls mod.parseInterval" {
		t.Errorf("correct option = %q", right)
	}
}

func TestDirectionGrading(t *testing.T) {
	g := documentedView()
	p, _ := Direction{}.Generate(context.Background(), g, g.View.Symbols["mod/scheduler.Next"])

	if got := p.Grade(Answer{Choice: correctIndex(p)}, g); got.Outcome != store.OutcomeCorrect {
		t.Errorf("correct answer graded %q", got.Outcome)
	}
	wrong := p.Grade(Answer{Choice: 1 - correctIndex(p)}, g)
	if wrong.Outcome != store.OutcomeWrong {
		t.Errorf("inverted answer graded %q", wrong.Outcome)
	}
	if !strings.Contains(wrong.Explanation, "call graph") {
		t.Errorf("explanation = %q, should cite its source", wrong.Explanation)
	}
}

func TestDirectionDeclinesLeaves(t *testing.T) {
	g := documentedView()
	// parseInterval calls nothing indexed, so there is no pair to ask about.
	if _, ok := (Direction{}).Generate(context.Background(), g, g.View.Symbols["mod.parseInterval"]); ok {
		t.Error("generated a direction probe for a symbol that calls nothing")
	}
}

// When A calls B and B calls A, both options are true and there is no answer
// to be wrong about.
func TestDirectionDeclinesMutualRecursion(t *testing.T) {
	v := specView()
	v.StaticCalls.AddEdge("mod.parseInterval", "mod/scheduler.Next")
	v.Calls.AddEdge("mod.parseInterval", "mod/scheduler.Next")

	g := NewContext(v, nil)
	p, ok := Direction{}.Generate(context.Background(), g, v.Symbols["mod/scheduler.Next"])
	if ok {
		t.Errorf("generated an unanswerable probe about a mutually recursive pair: %q", p.Stem)
	}
}

// A CHA guess is not a fact to examine anyone on.
func TestDirectionIgnoresDynamicEdges(t *testing.T) {
	v := specView()
	v.Symbols["mod/guess.Maybe"] = core.Symbol{ID: "mod/guess.Maybe", Name: "Maybe", File: "guess.go"}
	v.Calls.Add("mod/guess.Maybe")
	// Present only in the over-approximated graph.
	v.Calls.AddEdge("mod/webhook.Retry", "mod/guess.Maybe")

	g := NewContext(v, nil)
	if p, ok := (Direction{}).Generate(context.Background(), g, v.Symbols["mod/webhook.Retry"]); ok {
		for _, id := range p.Expected {
			if id == "mod/guess.Maybe" {
				t.Error("a CHA-guessed edge became a direction question")
			}
		}
	}
}
