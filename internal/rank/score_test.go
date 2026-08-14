package rank

import (
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

// A view with enough structure and history for every signal to have an
// opinion, so scoring can be tested end to end.
func fullView() *builder {
	b := newView()

	add := func(id core.SymbolID, file, pkg string) {
		b.v.Symbols[id] = core.Symbol{ID: id, Name: string(id), Kind: core.KindFunc, File: file, Package: pkg}
		b.v.Calls.Add(string(id))
	}
	add("mod/core.parseInterval", "core/interval.go", "mod/core")
	add("mod/api.Handler", "api/handler.go", "mod/api")
	add("mod/api.Serialize", "api/serialize.go", "mod/api")
	add("mod/util.Trivial", "util/trivial.go", "mod/util")

	b.calls("mod/api.Handler", "mod/core.parseInterval")
	b.calls("mod/api.Serialize", "mod/core.parseInterval")

	// parseInterval is central, hot, and repeatedly fixed.
	for i := 0; i < 8; i++ {
		b.commit(refNow.AddDate(0, 0, -i*4), "fix: interval edge case", "gone@example.com", "core/interval.go")
	}
	// Serialize is hidden-coupled to a schema it does not import.
	for i := 0; i < 6; i++ {
		b.commit(refNow.AddDate(0, 0, -i*5), "update payload", "here@example.com",
			"api/serialize.go", "mobile/schema.json")
	}
	// Trivial has no history at all.
	b.authorship("core/interval.go", "gone@example.com", 500, refNow.AddDate(0, 0, -300))
	b.authorship("api/handler.go", "here@example.com", 200, refNow.AddDate(0, 0, -2))
	return b
}

func TestRankProducesAnOrdering(t *testing.T) {
	res := Rank(fullView().build(), params(), DefaultWeights())

	if len(res.Items) != 4 {
		t.Fatalf("items = %d, want 4", len(res.Items))
	}
	for i := 1; i < len(res.Items); i++ {
		if res.Items[i-1].Score < res.Items[i].Score {
			t.Fatalf("items not sorted by score: %v then %v", res.Items[i-1].Score, res.Items[i].Score)
		}
	}
	// A symbol nothing has anything to say about must not outrank one that
	// every signal flags.
	byID := map[core.SymbolID]Item{}
	for _, it := range res.Items {
		byID[it.Symbol.ID] = it
	}
	if byID["mod/util.Trivial"].Score >= byID["mod/core.parseInterval"].Score {
		t.Errorf("an inert symbol (%v) outranked the central, hot, orphaned one (%v)",
			byID["mod/util.Trivial"].Score, byID["mod/core.parseInterval"].Score)
	}
}

func TestScoresStayInRange(t *testing.T) {
	for _, it := range Rank(fullView().build(), params(), DefaultWeights()).Items {
		if it.Score < 0 || it.Score > 1 {
			t.Errorf("%s scored %v, outside [0,1]", it.Symbol.ID, it.Score)
		}
	}
}

// Every ranking must be decomposable, or "is the ranking any good" is
// unanswerable.
func TestContributionsAreRecorded(t *testing.T) {
	res := Rank(fullView().build(), params(), DefaultWeights())

	var found bool
	for _, it := range res.Items {
		if it.Symbol.ID != "mod/api.Serialize" {
			continue
		}
		found = true
		if it.Contributions[SignalCoupling] <= 0 {
			t.Errorf("Serialize's hidden coupling was not recorded: %v", it.Contributions)
		}
		if sig, val := it.Top(); sig == "" || val <= 0 {
			t.Errorf("Top() = (%q, %v)", sig, val)
		}
	}
	if !found {
		t.Fatal("Serialize missing from the ranking")
	}
}

// "No AI markers found" and "this code is not AI-written" are different
// claims, and the result reports which one it is making.
func TestSilentSignalsAreReported(t *testing.T) {
	res := Rank(fullView().build(), params(), DefaultWeights())

	var aiSilent, proximitySilent bool
	for _, s := range res.Silent {
		if s == SignalAIDensity {
			aiSilent = true
		}
		if s == SignalProximity {
			proximitySilent = true
		}
	}
	if !aiSilent {
		t.Error("AI density had no data and should be reported silent")
	}
	if !proximitySilent {
		t.Error("no task was named, so proximity should be reported silent")
	}
	for _, s := range res.Active {
		if s == SignalAIDensity {
			t.Error("a signal with no data was counted as active")
		}
	}
}

// A repo with no git history must still produce meaningful numbers, not
// everything-near-zero.
func TestWeightsRenormalizeOverActiveSignals(t *testing.T) {
	b := newView()
	b.v.Symbols["mod.A"] = core.Symbol{ID: "mod.A", File: "a.go", Package: "mod"}
	b.v.Symbols["mod.B"] = core.Symbol{ID: "mod.B", File: "b.go", Package: "mod"}
	b.v.Calls.Add("mod.A")
	b.v.Calls.Add("mod.B")
	b.calls("mod.B", "mod.A")

	res := Rank(b.build(), params(), DefaultWeights())
	if len(res.Active) != 1 || res.Active[0] != SignalCentrality {
		t.Fatalf("active signals = %v, want just centrality", res.Active)
	}
	if res.Items[0].Score != 1 {
		t.Errorf("with one active signal the top item should score 1, got %v", res.Items[0].Score)
	}
}

func TestZeroWeightDisablesASignal(t *testing.T) {
	w := DefaultWeights()
	w[SignalCoupling] = 0

	res := Rank(fullView().build(), params(), w)
	for _, s := range res.Active {
		if s == SignalCoupling {
			t.Error("a zero-weighted signal was still computed and counted")
		}
	}
	for _, it := range res.Items {
		if it.Contributions[SignalCoupling] != 0 {
			t.Errorf("%s carries a coupling contribution despite zero weight", it.Symbol.ID)
		}
	}
}

// Familiarity is the only place the comprehension score touches output, and it
// moves the order rather than being displayed.
func TestFamiliarityDiscountsWhatIsAlreadyUnderstood(t *testing.T) {
	v := fullView().build()

	before := Rank(v, params(), DefaultWeights())
	topID := before.Items[0].Symbol.ID

	p := params()
	p.Familiarity = map[core.SymbolID]float64{topID: 0.9}
	after := Rank(v, p, DefaultWeights())

	var newScore float64
	for _, it := range after.Items {
		if it.Symbol.ID == topID {
			newScore = it.Score
		}
	}
	if newScore >= before.Items[0].Score {
		t.Errorf("a well-understood symbol scored %v, was %v", newScore, before.Items[0].Score)
	}
}

// Padding a top-ten to reach ten would fill a recommendation with items the
// tool has no reason to recommend.
func TestSelectDropsZeroScores(t *testing.T) {
	// Only hidden coupling is enabled, and only one symbol has any.
	w := Weights{SignalCoupling: 1}
	res := Rank(fullView().build(), params(), w)

	got := res.Select(10)
	if len(got) == 0 {
		t.Fatal("nothing selected at all")
	}
	if len(got) >= len(res.Items) {
		t.Errorf("selected %d of %d items; symbols no signal flagged were padded in",
			len(got), len(res.Items))
	}
	for _, it := range got {
		if it.Score <= 0 {
			t.Errorf("%s scored 0 and should not have been selected", it.Symbol.ID)
		}
	}
	if got[0].Symbol.ID != "mod/api.Serialize" {
		t.Errorf("top coupled symbol = %s, want mod/api.Serialize", got[0].Symbol.ID)
	}
}

func TestSelectRespectsTheLimit(t *testing.T) {
	res := Rank(fullView().build(), params(), DefaultWeights())
	if got := res.Select(2); len(got) != 2 {
		t.Errorf("Select(2) returned %d items", len(got))
	}
	if got := res.Select(0); len(got) != len(res.Select(1000)) {
		t.Error("Select(0) should mean unlimited")
	}
}

func TestRankIsDeterministic(t *testing.T) {
	v := fullView().build()
	first := Rank(v, params(), DefaultWeights()).Path(10)

	for i := 0; i < 10; i++ {
		got := Rank(v, params(), DefaultWeights()).Path(10)
		if len(got) != len(first) {
			t.Fatalf("run %d returned %d items, first returned %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j].Symbol.ID != first[j].Symbol.ID {
				t.Fatalf("run %d differs at %d: %s vs %s", i, j, got[j].Symbol.ID, first[j].Symbol.ID)
			}
		}
	}
}
