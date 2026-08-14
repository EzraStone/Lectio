package probe

import (
	"context"
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/graph"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/store"
)

// specView mirrors the spec's hero figure: parseInterval with three direct
// dependents and five more one hop out.
func specView() *index.View {
	v := &index.View{
		Symbols:     map[core.SymbolID]core.Symbol{},
		CoveredBy:   map[core.SymbolID][]core.SymbolID{},
		Calls:       graph.New(0),
		StaticCalls: graph.New(0),
	}
	add := func(id core.SymbolID, file string) {
		name := string(id)
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		v.Symbols[id] = core.Symbol{ID: id, Name: name, Kind: core.KindFunc, File: file}
		v.Calls.Add(string(id))
		v.StaticCalls.Add(string(id))
	}
	edge := func(from, to core.SymbolID) {
		v.Calls.AddEdge(string(from), string(to))
		v.StaticCalls.AddEdge(string(from), string(to))
	}

	for _, s := range []struct{ id, file string }{
		{"mod.parseInterval", "interval.go"},
		{"mod/scheduler.Next", "scheduler/next.go"},
		{"mod/billing.Cycle", "billing/cycle.go"},
		{"mod/retry.Backoff", "retry/backoff.go"},
		{"mod/cron.Register", "cron/register.go"},
		{"mod/digest.Send", "digest/send.go"},
		{"mod/invoice.Draft", "invoice/draft.go"},
		{"mod/queue.Requeue", "queue/requeue.go"},
		{"mod/webhook.Retry", "webhook/retry.go"},
	} {
		add(core.SymbolID(s.id), s.file)
	}

	edge("mod/scheduler.Next", "mod.parseInterval")
	edge("mod/billing.Cycle", "mod.parseInterval")
	edge("mod/retry.Backoff", "mod.parseInterval")
	edge("mod/cron.Register", "mod/scheduler.Next")
	edge("mod/digest.Send", "mod/scheduler.Next")
	edge("mod/invoice.Draft", "mod/billing.Cycle")
	edge("mod/queue.Requeue", "mod/retry.Backoff")
	edge("mod/webhook.Retry", "mod/retry.Backoff")
	return v
}

func mustGenerate(t *testing.T, g *Context, id core.SymbolID) Probe {
	t.Helper()
	p, ok := BlastRadius{}.Generate(context.Background(), g, g.View.Symbols[id])
	if !ok {
		t.Fatalf("no probe generated for %s", id)
	}
	return p
}

func TestBlastRadiusGroundTruth(t *testing.T) {
	g := NewContext(specView(), nil)
	p := mustGenerate(t, g, "mod.parseInterval")

	if !strings.Contains(p.Stem, "parseInterval") {
		t.Errorf("stem = %q", p.Stem)
	}
	// Depth 2: three direct dependents plus five one hop further.
	if len(p.Expected) != 8 {
		t.Errorf("expected set = %d symbols (%v), want 8", len(p.Expected), p.Expected)
	}
	for _, id := range p.Expected {
		if id == "mod.parseInterval" {
			t.Error("the subject appeared in its own answer set")
		}
	}
	if p.Provenance == "" {
		t.Error("a grader that cannot say why it marked something wrong is one nobody believes twice")
	}
}

func TestBlastRadiusPerfectAnswer(t *testing.T) {
	g := NewContext(specView(), nil)
	p := mustGenerate(t, g, "mod.parseInterval")

	names := make([]string, 0, len(p.Expected))
	for _, id := range p.Expected {
		names = append(names, id.Short())
	}

	got := p.Grade(Answer{Names: names}, g)
	if got.Outcome != store.OutcomeCorrect {
		t.Errorf("outcome = %q, want correct (score %v)", got.Outcome, got.Score)
	}
	if got.Score != 1 {
		t.Errorf("F1 = %v, want 1", got.Score)
	}
	if len(got.Missed) != 0 || len(got.Spurious) != 0 {
		t.Errorf("missed=%v spurious=%v", got.Missed, got.Spurious)
	}
}

// People answer in whatever form is shortest. Demanding a canonical identifier
// would measure typing accuracy rather than understanding.
func TestBlastRadiusAcceptsShortNames(t *testing.T) {
	g := NewContext(specView(), nil)
	p := mustGenerate(t, g, "mod.parseInterval")

	got := p.Grade(Answer{Names: []string{
		"Next", "Cycle", "Backoff", "Register", "Send", "Draft", "Requeue", "Retry",
	}}, g)

	if got.Outcome != store.OutcomeCorrect {
		t.Errorf("bare names scored %v (%q); missed %v", got.Score, got.Outcome, got.Missed)
	}
}

func TestBlastRadiusPartialCredit(t *testing.T) {
	g := NewContext(specView(), nil)
	p := mustGenerate(t, g, "mod.parseInterval")

	// Names the three direct dependents, misses the second hop.
	got := p.Grade(Answer{Names: []string{"scheduler.Next", "billing.Cycle", "retry.Backoff"}}, g)

	if got.Outcome != store.OutcomePartial {
		t.Errorf("outcome = %q, want partial (score %v)", got.Outcome, got.Score)
	}
	if got.Precision != 1 {
		t.Errorf("precision = %v, want 1 — everything named was correct", got.Precision)
	}
	if got.Recall >= 1 {
		t.Errorf("recall = %v, want below 1", got.Recall)
	}
	if len(got.Missed) != 5 {
		t.Errorf("missed = %v, want the five second-hop dependents", got.Missed)
	}
}

func TestBlastRadiusPenalizesSpuriousNames(t *testing.T) {
	g := NewContext(specView(), nil)
	p := mustGenerate(t, g, "mod.parseInterval")

	all := make([]string, 0, len(p.Expected))
	for _, id := range p.Expected {
		all = append(all, id.Short())
	}
	clean := p.Grade(Answer{Names: all}, g)
	noisy := p.Grade(Answer{Names: append(append([]string{}, all...), "NoSuchThing", "AlsoFake")}, g)

	if noisy.Score >= clean.Score {
		t.Errorf("naming things that do not break scored %v, same or better than a clean answer %v",
			noisy.Score, clean.Score)
	}
	if len(noisy.Spurious) != 2 {
		t.Errorf("spurious = %v, want two", noisy.Spurious)
	}
}

func TestBlastRadiusWrongAnswer(t *testing.T) {
	g := NewContext(specView(), nil)
	p := mustGenerate(t, g, "mod.parseInterval")

	got := p.Grade(Answer{Names: []string{"NoSuchThing"}}, g)
	if got.Outcome != store.OutcomeWrong {
		t.Errorf("outcome = %q, want wrong", got.Outcome)
	}
	// The explanation must tell them the answer, not just that they were wrong.
	if !strings.Contains(got.Explanation, "billing.Cycle") {
		t.Errorf("explanation = %q, want the real answer set", got.Explanation)
	}
	// Long answer sets are truncated rather than dumped.
	if !strings.Contains(got.Explanation, "more") {
		t.Errorf("explanation = %q, want a truncated tail for a long answer set", got.Explanation)
	}
}

// A skip is neutral, never a failure.
func TestBlastRadiusSkipIsNeutral(t *testing.T) {
	g := NewContext(specView(), nil)
	p := mustGenerate(t, g, "mod.parseInterval")

	got := p.Grade(Skip(), g)
	if got.Outcome != store.OutcomeSkipped {
		t.Errorf("outcome = %q, want skipped", got.Outcome)
	}
	if got.Score != 0 {
		t.Errorf("score = %v, want 0", got.Score)
	}
	if len(got.Missed) == 0 {
		t.Error("a skip should still show the answer; that is the point of skipping")
	}
}

// The spec's stopping rule: if the median answer time passes thirty seconds,
// the probe design is wrong. Unanswerable questions are declined at generation.
func TestBlastRadiusDeclinesUnfairSubjects(t *testing.T) {
	v := specView()

	// A leaf with no dependents at all.
	if _, ok := (BlastRadius{}).Generate(context.Background(), NewContext(v, nil),
		v.Symbols["mod/webhook.Retry"]); ok {
		t.Error("generated a blast-radius probe for a symbol nothing depends on")
	}

	// A symbol with far too many dependents to name.
	big := specView()
	for i := 0; i < 40; i++ {
		id := core.SymbolID("mod/many.F" + string(rune('a'+i%26)) + string(rune('a'+i/26)))
		big.Symbols[id] = core.Symbol{ID: id, Name: string(id), File: "many.go"}
		big.Calls.AddEdge(string(id), "mod.parseInterval")
		big.StaticCalls.AddEdge(string(id), "mod.parseInterval")
	}
	if _, ok := (BlastRadius{}).Generate(context.Background(), NewContext(big, nil),
		big.Symbols["mod.parseInterval"]); ok {
		t.Error("generated a probe with an answer set nobody could enumerate")
	}
}

// The trust property: over-approximated edges never reach the answer key.
func TestBlastRadiusGradesOnStaticEdgesOnly(t *testing.T) {
	v := specView()
	// A CHA-only edge: present in Calls, absent from StaticCalls.
	v.Symbols["mod/guess.Maybe"] = core.Symbol{ID: "mod/guess.Maybe", Name: "Maybe", File: "guess.go"}
	v.Calls.AddEdge("mod/guess.Maybe", "mod.parseInterval")

	g := NewContext(v, nil)
	p := mustGenerate(t, g, "mod.parseInterval")

	for _, id := range p.Expected {
		if id == "mod/guess.Maybe" {
			t.Fatal("a CHA-guessed dependent reached the answer key; a correct answer would be marked wrong")
		}
	}
}

func TestBlastRadiusIncludesCoveringTests(t *testing.T) {
	v := specView()
	v.CoveredBy["mod.parseInterval"] = []core.SymbolID{"mod.[tests]"}

	g := NewContext(v, nil)
	p := mustGenerate(t, g, "mod.parseInterval")

	var found bool
	for _, id := range p.Expected {
		if id == "mod.[tests]" {
			found = true
		}
	}
	if !found {
		t.Error("covering tests are half of what breaks and belong in the answer set")
	}
	if !strings.Contains(p.Provenance, "tests") {
		t.Errorf("provenance = %q, should mention tests", p.Provenance)
	}
}

func TestTemplateStemsNeedNoModel(t *testing.T) {
	sym := core.Symbol{ID: "mod.parseInterval", Name: "parseInterval"}
	got, err := TemplateStems{}.Stem(context.Background(), KindBlastRadius, sym)
	if err != nil || !strings.Contains(got, "parseInterval") {
		t.Errorf("Stem = %q, err %v", got, err)
	}
}
