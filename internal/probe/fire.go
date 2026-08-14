package probe

import (
	"context"
	"sort"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/store"
)

// Firing rules, straight from the spec.
const (
	// MaxPerDay caps how often probes interrupt. Three is the number in the
	// spec, and the reasoning behind it is worth keeping: a tool that asks a
	// fourth question is a tool people turn off, and a turned-off tool has a
	// comprehension score of zero for every symbol forever.
	MaxPerDay = 3

	// Cooldown keeps a symbol from being asked about twice in quick
	// succession. Answering correctly and being asked again an hour later
	// reads as not being believed.
	Cooldown = 7 * 24 * time.Hour
)

// Trigger describes why a probe is being offered.
type Trigger string

const (
	// TriggerFirstEdit is the spec's rule: fires on first modification of a
	// symbol not previously engaged.
	TriggerFirstEdit Trigger = "first_edit"
	// TriggerRequested is a probe the user asked for explicitly, which is
	// exempt from the daily cap — someone who types the command has consented
	// in a way an interruption never does.
	TriggerRequested Trigger = "requested"
)

// Scheduler decides whether and what to ask.
type Scheduler struct {
	Store *store.Store
	Now   time.Time
	// Generators are tried in order. Blast radius first: it is the primary
	// probe and the others exist to cover subjects it declines.
	Generators []Generator
}

// NewScheduler returns a scheduler with the default generator order.
func NewScheduler(s *store.Store, now time.Time) *Scheduler {
	return &Scheduler{
		Store: s,
		Now:   now,
		Generators: []Generator{
			BlastRadius{},
			Direction{},
			Locate{},
		},
	}
}

// Allowed reports whether a probe may fire now, and why not when it may not.
func (s *Scheduler) Allowed(ctx context.Context, trigger Trigger) (bool, string, error) {
	if trigger == TriggerRequested {
		return true, "", nil
	}

	dayStart := s.Now.Add(-24 * time.Hour)
	recent, err := s.Store.ProbesSince(ctx, dayStart)
	if err != nil {
		return false, "", err
	}

	// Skips count toward the cap. They are neutral for scoring — a skip is
	// never a failure — but they are not neutral for attention, and the cap
	// exists to protect attention. Excluding them would let a bad day generate
	// unlimited interruptions.
	if len(recent) >= MaxPerDay {
		return false, "already asked three times today", nil
	}
	return true, "", nil
}

// Next picks a subject and builds a probe, or reports that there is nothing
// fair to ask.
//
// Candidates are supplied by the caller, usually from a reading path, so the
// question is about something the person has a reason to care about. Asking
// about a random symbol is how a tool becomes trivia.
func (s *Scheduler) Next(ctx context.Context, g *Context, candidates []core.Symbol) (Probe, bool, error) {
	eligible, err := s.eligible(ctx, candidates)
	if err != nil {
		return Probe{}, false, err
	}

	for _, subject := range eligible {
		for _, gen := range s.Generators {
			if p, ok := gen.Generate(ctx, g, subject); ok {
				return p, true, nil
			}
		}
	}
	return Probe{}, false, nil
}

// eligible filters candidates by the cooldown and engagement rules.
func (s *Scheduler) eligible(ctx context.Context, candidates []core.Symbol) ([]core.Symbol, error) {
	asked, err := s.Store.ProbesSince(ctx, s.Now.Add(-Cooldown))
	if err != nil {
		return nil, err
	}
	recent := make(map[core.SymbolID]bool, len(asked))
	for _, r := range asked {
		recent[r.Symbol] = true
	}

	out := make([]core.Symbol, 0, len(candidates))
	for _, sym := range candidates {
		if sym.IsTest() || recent[sym.ID] {
			continue
		}
		out = append(out, sym)
	}
	return out, nil
}

// ShouldFireOnEdit implements the spec's trigger: a probe fires on the first
// modification of a symbol the user has not engaged with before.
func (s *Scheduler) ShouldFireOnEdit(ctx context.Context, symbol core.SymbolID) (bool, error) {
	engaged, err := s.Store.HasEngaged(ctx, symbol)
	if err != nil {
		return false, err
	}
	if engaged {
		return false, nil
	}
	allowed, _, err := s.Allowed(ctx, TriggerFirstEdit)
	return allowed, err
}

// Record persists a graded probe.
func (s *Scheduler) Record(ctx context.Context, p Probe, grade Grade, asked time.Time, elapsed time.Duration) error {
	return s.Store.RecordProbe(ctx, store.ProbeRecord{
		Symbol:   p.Subject.ID,
		Kind:     string(p.Kind),
		AskedAt:  asked,
		Answered: asked.Add(elapsed),
		Outcome:  grade.Outcome,
		Score:    grade.Score,
		Elapsed:  elapsed,
	})
}

// Health reports whether the probes are working as designed.
//
// The spec gives a falsifiable stopping rule — if the median answer time
// passes thirty seconds, the probe design is wrong — and a rule nobody can
// check is a rule nobody follows. This is how it gets checked.
type Health struct {
	Asked       int
	Answered    int
	Skipped     int
	MedianTime  time.Duration
	SkipRate    float64
	DesignBroke bool
	Note        string
}

// MedianTarget is the spec's threshold.
const MedianTarget = 30 * time.Second

// Health summarizes probe behavior over a window.
func (s *Scheduler) Health(ctx context.Context, window time.Duration) (Health, error) {
	records, err := s.Store.ProbesSince(ctx, s.Now.Add(-window))
	if err != nil {
		return Health{}, err
	}

	var h Health
	var times []time.Duration
	for _, r := range records {
		h.Asked++
		if r.Outcome == store.OutcomeSkipped {
			h.Skipped++
			continue
		}
		h.Answered++
		if r.Elapsed > 0 {
			times = append(times, r.Elapsed)
		}
	}
	if h.Asked > 0 {
		h.SkipRate = float64(h.Skipped) / float64(h.Asked)
	}
	if len(times) > 0 {
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		h.MedianTime = times[len(times)/2]
	}

	switch {
	case h.MedianTime > MedianTarget:
		h.DesignBroke = true
		h.Note = "median answer time is over 30 seconds — the probes are too hard to be worth asking"
	case h.Asked >= 5 && h.SkipRate > 0.6:
		h.DesignBroke = true
		h.Note = "most probes are being skipped — they are not landing on anything useful"
	}
	return h, nil
}
