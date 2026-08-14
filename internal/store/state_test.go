package store

import (
	"context"
	"testing"
	"time"
)

var now = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func TestRecordEditAccumulates(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := s.RecordEdit(ctx, "pkg.A", now.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatalf("RecordEdit: %v", err)
		}
	}
	e, err := s.Engagement(ctx, "pkg.A")
	if err != nil {
		t.Fatalf("Engagement: %v", err)
	}
	if e.Edits != 3 {
		t.Errorf("edits = %d, want 3", e.Edits)
	}
	if !e.FirstSeen.Equal(now) {
		t.Errorf("first_seen = %v, want %v", e.FirstSeen, now)
	}
	if !e.LastSeen.Equal(now.Add(2 * time.Hour)) {
		t.Errorf("last_seen = %v, want %v", e.LastSeen, now.Add(2*time.Hour))
	}
}

func TestUnknownSymbolReturnsZeroRecord(t *testing.T) {
	s := openTemp(t)
	e, err := s.Engagement(context.Background(), "pkg.Never")
	if err != nil {
		t.Fatalf("Engagement on unknown symbol errored: %v", err)
	}
	if e.Edits != 0 || e.Symbol != "pkg.Never" {
		t.Errorf("zero record = %+v", e)
	}
	if f := e.Familiarity(now); f != 0 {
		t.Errorf("familiarity of an untouched symbol = %v, want 0", f)
	}
}

// The spec: a skip is neutral, never a failure.
func TestSkipIsNeutral(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.RecordProbe(ctx, ProbeRecord{
		Symbol: "pkg.A", Kind: "blast", AskedAt: now, Outcome: OutcomeSkipped,
	}); err != nil {
		t.Fatalf("RecordProbe: %v", err)
	}

	e, _ := s.Engagement(ctx, "pkg.A")
	if e.ProbesSeen != 0 {
		t.Errorf("probes_seen = %d after a skip, want 0", e.ProbesSeen)
	}
	if e.ProbesSkipped != 1 {
		t.Errorf("probes_skipped = %d, want 1", e.ProbesSkipped)
	}
	if f := e.Familiarity(now); f != 0 {
		t.Errorf("a skip moved familiarity to %v, want 0", f)
	}
}

func TestWrongAnswerDoesNotRaiseFamiliarity(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.RecordProbe(ctx, ProbeRecord{
		Symbol: "pkg.A", Kind: "blast", AskedAt: now, Outcome: OutcomeWrong,
	}); err != nil {
		t.Fatalf("RecordProbe: %v", err)
	}
	e, _ := s.Engagement(ctx, "pkg.A")
	if e.ProbesSeen != 1 || e.ProbesCorrect != 0 {
		t.Errorf("record = %+v", e)
	}
	if f := e.Familiarity(now); f != 0 {
		t.Errorf("familiarity after a wrong answer = %v, want 0", f)
	}
}

func TestFamiliarityRisesWithEvidenceAndSaturates(t *testing.T) {
	one := Engagement{Edits: 1, LastSeen: now}.Familiarity(now)
	three := Engagement{Edits: 3, LastSeen: now}.Familiarity(now)
	many := Engagement{Edits: 30, LastSeen: now}.Familiarity(now)

	if !(one < three && three < many) {
		t.Errorf("familiarity should rise with edits: %v %v %v", one, three, many)
	}
	if many > 1 {
		t.Errorf("familiarity = %v, must stay in [0,1]", many)
	}

	// Saturation means the marginal value of one more edit shrinks. Compare
	// equal-sized steps, early and late.
	early := Engagement{Edits: 2, LastSeen: now}.Familiarity(now) - one
	late := Engagement{Edits: 11, LastSeen: now}.Familiarity(now) -
		Engagement{Edits: 10, LastSeen: now}.Familiarity(now)
	if late >= early {
		t.Errorf("the 11th edit was worth %v and the 2nd was worth %v; edit evidence should saturate", late, early)
	}
}

// One correct answer is weak evidence and must not read as mastery.
func TestSingleCorrectAnswerIsNotMastery(t *testing.T) {
	single := Engagement{ProbesSeen: 1, ProbesCorrect: 1, LastSeen: now}.Familiarity(now)
	if single > 0.5 {
		t.Errorf("one correct probe scored %v; that is too confident", single)
	}
	repeated := Engagement{ProbesSeen: 8, ProbesCorrect: 8, LastSeen: now}.Familiarity(now)
	if repeated <= single {
		t.Errorf("eight correct (%v) should beat one correct (%v)", repeated, single)
	}
}

func TestFamiliarityDecaysWithTime(t *testing.T) {
	e := Engagement{Edits: 10, ProbesSeen: 4, ProbesCorrect: 4}
	fresh := Engagement{Edits: e.Edits, ProbesSeen: e.ProbesSeen, ProbesCorrect: e.ProbesCorrect, LastSeen: now}
	stale := Engagement{Edits: e.Edits, ProbesSeen: e.ProbesSeen, ProbesCorrect: e.ProbesCorrect, LastSeen: now.AddDate(0, -6, 0)}

	if stale.Familiarity(now) >= fresh.Familiarity(now) {
		t.Errorf("six-month-old understanding (%v) should score below current (%v)",
			stale.Familiarity(now), fresh.Familiarity(now))
	}
}

func TestHasEngagedGatesTheFiringRule(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if got, _ := s.HasEngaged(ctx, "pkg.A"); got {
		t.Error("HasEngaged on a fresh symbol should be false")
	}
	s.RecordEdit(ctx, "pkg.A", now)
	if got, _ := s.HasEngaged(ctx, "pkg.A"); !got {
		t.Error("HasEngaged after an edit should be true")
	}
}

func TestProbesSinceWindowsCorrectly(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	for i, when := range []time.Time{now.AddDate(0, 0, -10), now.AddDate(0, 0, -1), now} {
		if err := s.RecordProbe(ctx, ProbeRecord{
			Symbol: "pkg.A", Kind: "blast", AskedAt: when, Answered: when,
			Outcome: OutcomeCorrect, Score: 1, Elapsed: time.Duration(i+1) * time.Second,
		}); err != nil {
			t.Fatalf("RecordProbe: %v", err)
		}
	}

	recent, err := s.ProbesSince(ctx, now.AddDate(0, 0, -2))
	if err != nil {
		t.Fatalf("ProbesSince: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("probes in the last two days = %d, want 2", len(recent))
	}
	if recent[0].Elapsed != 2*time.Second {
		t.Errorf("elapsed did not round-trip: %v", recent[0].Elapsed)
	}
	if recent[0].Score != 1 || recent[0].Outcome != OutcomeCorrect {
		t.Errorf("record = %+v", recent[0])
	}
}

func TestPurgeStateLeavesIndexIntact(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	mustExec(t, s.DB(), `INSERT INTO files(id, path) VALUES(1,'a.go')`)
	mustExec(t, s.DB(), `INSERT INTO symbols(id,name,kind,package,file_id) VALUES('pkg.A','A','func','pkg',1)`)
	s.RecordEdit(ctx, "pkg.A", now)
	s.RecordProbe(ctx, ProbeRecord{Symbol: "pkg.A", Kind: "blast", AskedAt: now, Outcome: OutcomeCorrect})

	if err := s.PurgeState(ctx); err != nil {
		t.Fatalf("PurgeState: %v", err)
	}
	if got := count(t, s.DB(), "engagement"); got != 0 {
		t.Errorf("engagement rows = %d after purge, want 0", got)
	}
	if got := count(t, s.DB(), "probe_log"); got != 0 {
		t.Errorf("probe_log rows = %d after purge, want 0", got)
	}
	if got := count(t, s.DB(), "symbols"); got != 1 {
		t.Errorf("purge removed index data: symbols = %d, want 1", got)
	}
}

func TestAllEngagement(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	s.RecordEdit(ctx, "pkg.A", now)
	s.RecordEdit(ctx, "pkg.B", now)

	all, err := s.AllEngagement(ctx)
	if err != nil {
		t.Fatalf("AllEngagement: %v", err)
	}
	if len(all) != 2 || all["pkg.A"].Edits != 1 {
		t.Errorf("AllEngagement = %+v", all)
	}
}
