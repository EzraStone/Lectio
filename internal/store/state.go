package store

import (
	"context"
	"database/sql"
	"math"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
)

// Outcome is how a probe ended.
type Outcome string

const (
	OutcomeCorrect Outcome = "correct"
	OutcomePartial Outcome = "partial"
	OutcomeWrong   Outcome = "wrong"
	// OutcomeSkipped is neutral. The spec is explicit that a skip is never a
	// failure, so it is recorded for firing-rate purposes and excluded from
	// every accuracy calculation.
	OutcomeSkipped Outcome = "skipped"
)

// Engagement is what the tool believes about one person's grasp of one symbol.
// It is derived only from local evidence: their own edits and their own probe
// answers.
type Engagement struct {
	Symbol        core.SymbolID
	Edits         int
	ProbesSeen    int
	ProbesCorrect int
	ProbesSkipped int
	FirstSeen     time.Time
	LastSeen      time.Time
}

// ProbeRecord is one asked question and how it went.
type ProbeRecord struct {
	ID       int64
	Symbol   core.SymbolID
	Kind     string
	AskedAt  time.Time
	Answered time.Time
	Outcome  Outcome
	// Score is the graded result in [0,1] — F1 for blast radius, 0 or 1 for the
	// binary probes.
	Score   float64
	Elapsed time.Duration
}

// Familiarity is the internal comprehension score, in [0,1].
//
// This number exists to drive the reading path and nothing else. It is never
// the headline, it is never shown as a grade, and it never leaves this
// machine — the spec is unambiguous that a diagnostic is a thermometer and
// this product is not one.
//
// The shape: edit evidence and probe evidence combine as independent chances
// of understanding rather than as a weighted average, so either one alone can
// carry the score, and neither saturates it. Then recency decays it, because
// having understood something in March is not the same as understanding it now.
func (e Engagement) Familiarity(now time.Time) float64 {
	// Edits saturate: the third time you touch something teaches you much less
	// than the first.
	edit := 1 - math.Exp(-float64(e.Edits)/3.0)

	// Probe accuracy, discounted by how little we have asked. One correct
	// answer is weak evidence and must not read as mastery.
	var probe float64
	if e.ProbesSeen > 0 {
		accuracy := float64(e.ProbesCorrect) / float64(e.ProbesSeen)
		confidence := 1 - math.Exp(-float64(e.ProbesSeen)/2.0)
		probe = accuracy * confidence
	}

	combined := 1 - (1-edit)*(1-probe)

	if !e.LastSeen.IsZero() {
		days := now.Sub(e.LastSeen).Hours() / 24
		if days > 0 {
			combined *= math.Exp(-days / 90.0)
		}
	}
	return clamp01(combined)
}

// RecordEdit notes that the user changed a symbol.
func (s *Store) RecordEdit(ctx context.Context, symbol core.SymbolID, when time.Time) error {
	ts := when.Unix()
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO engagement(symbol, edits, first_seen, last_seen) VALUES(?, 1, ?, ?)
        ON CONFLICT(symbol) DO UPDATE SET
            edits      = edits + 1,
            first_seen = CASE WHEN first_seen = 0 THEN excluded.first_seen ELSE min(first_seen, excluded.first_seen) END,
            last_seen  = max(last_seen, excluded.last_seen)`, string(symbol), ts, ts)
	return err
}

// RecordProbe logs a probe and folds it into the engagement record.
func (s *Store) RecordProbe(ctx context.Context, r ProbeRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var answered int64
	if !r.Answered.IsZero() {
		answered = r.Answered.Unix()
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO probe_log(symbol, kind, asked_at, answered_at, outcome, score, elapsed_ms)
        VALUES(?,?,?,?,?,?,?)`,
		string(r.Symbol), r.Kind, r.AskedAt.Unix(), answered,
		string(r.Outcome), r.Score, r.Elapsed.Milliseconds()); err != nil {
		return err
	}

	// A skip advances nothing but the skip counter. Counting it as seen would
	// make skipping cheaper than answering, which inverts the incentive the
	// firing rule is trying to create.
	seen, correct, skipped := 1, 0, 0
	switch r.Outcome {
	case OutcomeSkipped:
		seen, skipped = 0, 1
	case OutcomeCorrect:
		correct = 1
	}

	ts := r.AskedAt.Unix()
	_, err = tx.ExecContext(ctx, `
        INSERT INTO engagement(symbol, probes_seen, probes_correct, probes_skipped, first_seen, last_seen)
        VALUES(?,?,?,?,?,?)
        ON CONFLICT(symbol) DO UPDATE SET
            probes_seen    = probes_seen    + excluded.probes_seen,
            probes_correct = probes_correct + excluded.probes_correct,
            probes_skipped = probes_skipped + excluded.probes_skipped,
            first_seen     = CASE WHEN first_seen = 0 THEN excluded.first_seen ELSE min(first_seen, excluded.first_seen) END,
            last_seen      = max(last_seen, excluded.last_seen)`,
		string(r.Symbol), seen, correct, skipped, ts, ts)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Engagement returns what is known about one symbol. An untouched symbol
// returns a zero record, which is the honest answer rather than an error.
func (s *Store) Engagement(ctx context.Context, symbol core.SymbolID) (Engagement, error) {
	e := Engagement{Symbol: symbol}
	var first, last int64
	err := s.db.QueryRowContext(ctx, `
        SELECT edits, probes_seen, probes_correct, probes_skipped, first_seen, last_seen
        FROM engagement WHERE symbol = ?`, string(symbol)).
		Scan(&e.Edits, &e.ProbesSeen, &e.ProbesCorrect, &e.ProbesSkipped, &first, &last)
	if err == sql.ErrNoRows {
		return e, nil
	}
	if err != nil {
		return e, err
	}
	e.FirstSeen, e.LastSeen = fromUnix(first), fromUnix(last)
	return e, nil
}

// AllEngagement returns every engagement record, keyed by symbol.
func (s *Store) AllEngagement(ctx context.Context) (map[core.SymbolID]Engagement, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT symbol, edits, probes_seen, probes_correct, probes_skipped, first_seen, last_seen
        FROM engagement`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[core.SymbolID]Engagement)
	for rows.Next() {
		var e Engagement
		var first, last int64
		if err := rows.Scan(&e.Symbol, &e.Edits, &e.ProbesSeen, &e.ProbesCorrect,
			&e.ProbesSkipped, &first, &last); err != nil {
			return nil, err
		}
		e.FirstSeen, e.LastSeen = fromUnix(first), fromUnix(last)
		out[e.Symbol] = e
	}
	return out, rows.Err()
}

// HasEngaged reports whether the user has any recorded history with a symbol.
// This is the trigger condition for the firing rule: probes fire on the first
// modification of a symbol not previously engaged.
func (s *Store) HasEngaged(ctx context.Context, symbol core.SymbolID) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM engagement WHERE symbol = ?`, string(symbol)).Scan(&n)
	return n > 0, err
}

// ProbesSince returns probes asked at or after t, oldest first.
func (s *Store) ProbesSince(ctx context.Context, t time.Time) ([]ProbeRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, symbol, kind, asked_at, answered_at, outcome, score, elapsed_ms
        FROM probe_log WHERE asked_at >= ? ORDER BY asked_at`, t.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProbeRecord
	for rows.Next() {
		var r ProbeRecord
		var asked, answered, elapsed int64
		if err := rows.Scan(&r.ID, &r.Symbol, &r.Kind, &asked, &answered,
			&r.Outcome, &r.Score, &elapsed); err != nil {
			return nil, err
		}
		r.AskedAt, r.Answered = fromUnix(asked), fromUnix(answered)
		r.Elapsed = time.Duration(elapsed) * time.Millisecond
		out = append(out, r)
	}
	return out, rows.Err()
}

// PurgeState deletes every trace of the user's history, leaving the index
// intact. The spec promises individual scores never leave the machine; the
// least this can do is make removing them one command.
func (s *Store) PurgeState(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"probe_log", "engagement"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func fromUnix(ts int64) time.Time {
	if ts == 0 {
		return time.Time{}
	}
	return time.Unix(ts, 0).UTC()
}

func clamp01(v float64) float64 {
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
