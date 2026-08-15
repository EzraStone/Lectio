package probe

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/store"
)

func recordStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRecordPersistsAGradedProbe(t *testing.T) {
	ctx := context.Background()
	s := recordStore(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	sched := NewScheduler(s, now)

	p := Probe{Kind: KindBlastRadius, Subject: core.Symbol{ID: "m/p.Parse"}}
	err := sched.Record(ctx, p, Grade{Outcome: store.OutcomeCorrect, Score: 0.8}, now, 12*time.Second)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	e, err := s.Engagement(ctx, "m/p.Parse")
	if err != nil {
		t.Fatal(err)
	}
	if e.ProbesCorrect != 1 {
		t.Errorf("ProbesCorrect = %d, want 1", e.ProbesCorrect)
	}
}

// A skip is neutral, never a failure. If skipping cost accuracy, skipping
// would be more expensive than answering badly and nobody would ever use it —
// which is the spec's explicit design constraint on the firing rules.
func TestRecordingASkipDoesNotTouchAccuracy(t *testing.T) {
	ctx := context.Background()
	s := recordStore(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	sched := NewScheduler(s, now)

	p := Probe{Kind: KindBlastRadius, Subject: core.Symbol{ID: "m/p.Skipped"}}
	if err := sched.Record(ctx, p, Grade{Outcome: store.OutcomeSkipped}, now, time.Second); err != nil {
		t.Fatal(err)
	}

	e, err := s.Engagement(ctx, "m/p.Skipped")
	if err != nil {
		t.Fatal(err)
	}
	if e.ProbesCorrect != 0 {
		t.Errorf("a skip counted as correct: %+v", e)
	}
	if e.ProbesSkipped != 1 {
		t.Errorf("the skip was not recorded as a skip: %+v", e)
	}
	// The accuracy denominator is what matters: a skip must not make the next
	// answer worth less.
	if e.ProbesSeen != 0 {
		t.Errorf("a skip entered the accuracy denominator: %+v", e)
	}
}

// The elapsed time is what the spec's stopping rule is measured on: if the
// median answer passes thirty seconds, the probe design is wrong. Recording it
// incorrectly would make that rule uncheckable.
func TestRecordedElapsedTimeReachesHealth(t *testing.T) {
	ctx := context.Background()
	s := recordStore(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	sched := NewScheduler(s, now)

	for i, d := range []time.Duration{10 * time.Second, 20 * time.Second, 45 * time.Second} {
		p := Probe{Kind: KindBlastRadius, Subject: core.Symbol{ID: core.SymbolID("m/p.S" + string(rune('0'+i)))}}
		if err := sched.Record(ctx, p, Grade{Outcome: store.OutcomeCorrect, Score: 1}, now, d); err != nil {
			t.Fatal(err)
		}
	}

	h, err := sched.Health(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if h.Answered != 3 {
		t.Errorf("Answered = %d, want 3", h.Answered)
	}
	if h.MedianTime != 20*time.Second {
		t.Errorf("MedianTime = %v, want 20s", h.MedianTime)
	}
	if h.DesignBroke {
		t.Errorf("a 20s median should not trip the %v rule", MedianTarget)
	}
}

// The stopping rule has to actually fire, or it is decoration.
func TestHealthReportsABrokenProbeDesign(t *testing.T) {
	ctx := context.Background()
	s := recordStore(t)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	sched := NewScheduler(s, now)

	for i := 0; i < 3; i++ {
		p := Probe{Kind: KindBlastRadius, Subject: core.Symbol{ID: core.SymbolID("m/p.Slow" + string(rune('0'+i)))}}
		if err := sched.Record(ctx, p, Grade{Outcome: store.OutcomeCorrect, Score: 1}, now, 90*time.Second); err != nil {
			t.Fatal(err)
		}
	}

	h, err := sched.Health(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !h.DesignBroke {
		t.Errorf("a 90s median did not trip the %v rule: %+v", MedianTarget, h)
	}
	if h.Note == "" {
		t.Error("a broken design was reported without saying so in words")
	}
}
