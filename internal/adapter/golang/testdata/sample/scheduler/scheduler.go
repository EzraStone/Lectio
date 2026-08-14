// Package scheduler decides when work runs next.
package scheduler

import (
	"time"

	"example.com/sample"
)

// Clock abstracts time so tests can control it. It exists here to give the
// call-graph extractor an interface call to resolve.
type Clock interface {
	Now() time.Time
}

// realClock reads the wall clock.
type realClock struct{}

// Now returns the current time.
func (realClock) Now() time.Time { return time.Now() }

// fixedClock always reports the same instant.
type fixedClock struct{ at time.Time }

// Now returns the fixed instant.
func (c fixedClock) Now() time.Time { return c.at }

// Scheduler computes the next run time for a spec.
type Scheduler struct {
	clock Clock
}

// New returns a Scheduler on the real clock.
func New() *Scheduler { return &Scheduler{clock: realClock{}} }

// Next returns when the next run is due.
func (s *Scheduler) Next(spec string) (time.Time, error) {
	iv, err := sample.Parse(spec)
	if err != nil {
		return time.Time{}, err
	}
	// An interface call: only class-hierarchy analysis resolves this, and it
	// resolves to both implementations rather than the one actually installed.
	return s.clock.Now().Add(iv.Every), nil
}

// Frozen returns a Scheduler pinned to an instant, for tests.
func Frozen(at time.Time) *Scheduler { return &Scheduler{clock: fixedClock{at: at}} }
