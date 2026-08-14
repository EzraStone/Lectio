package probe

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/store"
)

var fireNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func newScheduler(t *testing.T) (*Scheduler, *Context) {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewScheduler(s, fireNow), documentedView()
}

func TestDailyCap(t *testing.T) {
	sch, _ := newScheduler(t)
	ctx := context.Background()

	for i := 0; i < MaxPerDay; i++ {
		if ok, why, _ := sch.Allowed(ctx, TriggerFirstEdit); !ok {
			t.Fatalf("blocked after %d probes: %s", i, why)
		}
		if err := sch.Store.RecordProbe(ctx, store.ProbeRecord{
			Symbol: core.SymbolID("mod.S" + string(rune('a'+i))), Kind: "blast",
			AskedAt: fireNow.Add(-time.Duration(i) * time.Hour), Outcome: store.OutcomeCorrect,
		}); err != nil {
			t.Fatal(err)
		}
	}

	ok, why, err := sch.Allowed(ctx, TriggerFirstEdit)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("a fourth probe was allowed to interrupt")
	}
	if why == "" {
		t.Error("no reason given for declining")
	}
}

// Skips are neutral for scoring but not for attention, and the cap protects
// attention. Excluding them would let a bad day generate unlimited prompts.
func TestSkipsCountTowardTheCap(t *testing.T) {
	sch, _ := newScheduler(t)
	ctx := context.Background()

	for i := 0; i < MaxPerDay; i++ {
		sch.Store.RecordProbe(ctx, store.ProbeRecord{
			Symbol: core.SymbolID("mod.S" + string(rune('a'+i))), Kind: "blast",
			AskedAt: fireNow.Add(-time.Duration(i) * time.Minute), Outcome: store.OutcomeSkipped,
		})
	}
	if ok, _, _ := sch.Allowed(ctx, TriggerFirstEdit); ok {
		t.Error("three skips did not count toward the daily cap")
	}
}

// Someone who types the command has consented in a way an interruption never
// does.
func TestRequestedProbesBypassTheCap(t *testing.T) {
	sch, _ := newScheduler(t)
	ctx := context.Background()

	for i := 0; i < MaxPerDay+2; i++ {
		sch.Store.RecordProbe(ctx, store.ProbeRecord{
			Symbol: core.SymbolID("mod.S" + string(rune('a'+i))), Kind: "blast",
			AskedAt: fireNow, Outcome: store.OutcomeCorrect,
		})
	}
	if ok, why, _ := sch.Allowed(ctx, TriggerRequested); !ok {
		t.Errorf("an explicitly requested probe was blocked: %s", why)
	}
}

func TestCapIsADayNotForever(t *testing.T) {
	sch, _ := newScheduler(t)
	ctx := context.Background()

	for i := 0; i < MaxPerDay; i++ {
		sch.Store.RecordProbe(ctx, store.ProbeRecord{
			Symbol: core.SymbolID("mod.S" + string(rune('a'+i))), Kind: "blast",
			AskedAt: fireNow.AddDate(0, 0, -3), Outcome: store.OutcomeCorrect,
		})
	}
	if ok, why, _ := sch.Allowed(ctx, TriggerFirstEdit); !ok {
		t.Errorf("three-day-old probes still counted against today: %s", why)
	}
}

// Answering correctly and being asked again an hour later reads as not being
// believed.
func TestCooldownPreventsRepeats(t *testing.T) {
	sch, g := newScheduler(t)
	ctx := context.Background()

	subject := g.View.Symbols["mod.parseInterval"]
	if _, ok, _ := sch.Next(ctx, g, []core.Symbol{subject}); !ok {
		t.Fatal("no probe offered for a fresh subject")
	}

	sch.Store.RecordProbe(ctx, store.ProbeRecord{
		Symbol: subject.ID, Kind: "blast", AskedAt: fireNow.Add(-time.Hour),
		Outcome: store.OutcomeCorrect,
	})

	if _, ok, _ := sch.Next(ctx, g, []core.Symbol{subject}); ok {
		t.Error("the same symbol was offered again inside the cooldown")
	}
}

func TestCooldownExpires(t *testing.T) {
	sch, g := newScheduler(t)
	ctx := context.Background()

	subject := g.View.Symbols["mod.parseInterval"]
	sch.Store.RecordProbe(ctx, store.ProbeRecord{
		Symbol: subject.ID, Kind: "blast", AskedAt: fireNow.Add(-Cooldown - time.Hour),
		Outcome: store.OutcomeCorrect,
	})

	if _, ok, _ := sch.Next(ctx, g, []core.Symbol{subject}); !ok {
		t.Error("a subject probed long ago should be eligible again")
	}
}

// The spec's trigger: first modification of a symbol not previously engaged.
func TestFiresOnFirstEditOnly(t *testing.T) {
	sch, _ := newScheduler(t)
	ctx := context.Background()

	should, err := sch.ShouldFireOnEdit(ctx, "mod.parseInterval")
	if err != nil || !should {
		t.Errorf("first edit of an unengaged symbol should fire: %v %v", should, err)
	}

	sch.Store.RecordEdit(ctx, "mod.parseInterval", fireNow)
	should, err = sch.ShouldFireOnEdit(ctx, "mod.parseInterval")
	if err != nil {
		t.Fatal(err)
	}
	if should {
		t.Error("an already-engaged symbol should not fire again")
	}
}

// Blast radius is the primary probe; the others cover subjects it declines.
func TestGeneratorOrderPrefersBlastRadius(t *testing.T) {
	sch, g := newScheduler(t)

	p, ok, err := sch.Next(context.Background(), g, []core.Symbol{g.View.Symbols["mod.parseInterval"]})
	if err != nil || !ok {
		t.Fatalf("no probe: %v %v", ok, err)
	}
	if p.Kind != KindBlastRadius {
		t.Errorf("kind = %q, want blast radius for a subject with dependents", p.Kind)
	}
}

func TestFallsBackWhenBlastRadiusDeclines(t *testing.T) {
	sch, g := newScheduler(t)

	// cron.Register has no dependents at all — too few for a blast-radius
	// probe — but it calls scheduler.Next, so direction works.
	p, ok, err := sch.Next(context.Background(), g, []core.Symbol{g.View.Symbols["mod/cron.Register"]})
	if err != nil || !ok {
		t.Fatalf("no probe: %v %v", ok, err)
	}
	if p.Kind == KindBlastRadius {
		t.Errorf("blast radius should have declined a subject nothing depends on")
	}
	if p.Kind != KindDirection {
		t.Errorf("kind = %q, want direction as the fallback", p.Kind)
	}
}

func TestNextReportsNothingFairToAsk(t *testing.T) {
	sch, g := newScheduler(t)

	// A symbol with no dependents, no callees, and no doc.
	lonely := core.Symbol{ID: "mod.Lonely", Name: "Lonely", File: "lonely.go"}
	g.View.Symbols[lonely.ID] = lonely
	g.View.Calls.Add(string(lonely.ID))
	g.View.StaticCalls.Add(string(lonely.ID))

	if _, ok, err := sch.Next(context.Background(), g, []core.Symbol{lonely}); ok || err != nil {
		t.Errorf("expected no fair probe, got ok=%v err=%v", ok, err)
	}
}

// A stopping rule nobody can check is a rule nobody follows.
func TestHealthDetectsABrokenProbeDesign(t *testing.T) {
	sch, _ := newScheduler(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		sch.Store.RecordProbe(ctx, store.ProbeRecord{
			Symbol: core.SymbolID("mod.S" + string(rune('a'+i))), Kind: "blast",
			AskedAt: fireNow.Add(-time.Duration(i) * time.Hour),
			Outcome: store.OutcomeCorrect, Elapsed: 90 * time.Second,
		})
	}

	h, err := sch.Health(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !h.DesignBroke {
		t.Errorf("a 90-second median should trip the spec's stopping rule: %+v", h)
	}
	if h.MedianTime != 90*time.Second {
		t.Errorf("median = %v, want 90s", h.MedianTime)
	}
}

func TestHealthDetectsMassSkipping(t *testing.T) {
	sch, _ := newScheduler(t)
	ctx := context.Background()

	for i := 0; i < 8; i++ {
		outcome := store.OutcomeSkipped
		if i < 2 {
			outcome = store.OutcomeCorrect
		}
		sch.Store.RecordProbe(ctx, store.ProbeRecord{
			Symbol: core.SymbolID("mod.S" + string(rune('a'+i))), Kind: "blast",
			AskedAt: fireNow.Add(-time.Duration(i) * time.Hour),
			Outcome: outcome, Elapsed: 5 * time.Second,
		})
	}

	h, _ := sch.Health(ctx, 30*24*time.Hour)
	if !h.DesignBroke {
		t.Errorf("a 75%% skip rate should be reported as a problem: %+v", h)
	}
	if h.Skipped != 6 || h.Answered != 2 {
		t.Errorf("counts wrong: %+v", h)
	}
}

func TestHealthIsQuietWhenFine(t *testing.T) {
	sch, _ := newScheduler(t)
	ctx := context.Background()

	for i := 0; i < 6; i++ {
		sch.Store.RecordProbe(ctx, store.ProbeRecord{
			Symbol: core.SymbolID("mod.S" + string(rune('a'+i))), Kind: "blast",
			AskedAt: fireNow.Add(-time.Duration(i) * time.Hour),
			Outcome: store.OutcomeCorrect, Elapsed: 8 * time.Second,
		})
	}

	h, _ := sch.Health(ctx, 30*24*time.Hour)
	if h.DesignBroke {
		t.Errorf("healthy probes reported as broken: %+v", h)
	}
	if h.Note != "" {
		t.Errorf("note = %q, want empty", h.Note)
	}
}
