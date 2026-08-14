// Package sample is a miniature of the codebase in the spec's hero figure:
// one heavily depended-on parser with a fan of callers above it.
package sample

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrBadInterval is returned for an unparseable interval spec.
var ErrBadInterval = errors.New("bad interval")

// DefaultInterval is used when no interval is configured.
const DefaultInterval = time.Minute

// Interval is a parsed schedule interval.
type Interval struct {
	Every  time.Duration
	Jitter time.Duration
}

// parseInterval parses a duration spec such as "30s" or "5m+1s".
//
// This is the symbol every probe in the tests is aimed at: it is unexported,
// it has many dependents, and nothing it calls belongs to this repo.
func parseInterval(spec string) (Interval, error) {
	base, jitter, hasJitter := strings.Cut(spec, "+")
	every, err := time.ParseDuration(strings.TrimSpace(base))
	if err != nil {
		return Interval{}, ErrBadInterval
	}
	out := Interval{Every: every}
	if hasJitter {
		j, err := time.ParseDuration(strings.TrimSpace(jitter))
		if err != nil {
			return Interval{}, ErrBadInterval
		}
		out.Jitter = j
	}
	return out, nil
}

// Parse is the exported entry point every other package goes through.
func Parse(spec string) (Interval, error) {
	if spec == "" {
		return Interval{Every: DefaultInterval}, nil
	}
	return parseInterval(spec)
}

// Describe renders an interval for logs.
func (i Interval) Describe() string {
	return strconv.FormatInt(int64(i.Every/time.Second), 10) + "s"
}
