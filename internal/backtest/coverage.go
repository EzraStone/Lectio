package backtest

import "fmt"

// Every matched-pair result in this project carries the same caveat: at file
// granularity the measure reaches well under half the cases. That caveat has
// always been stated as a count of cases, which says how often the question
// could be asked and not why it could not.
//
// The why matters because the two reasons point at different fixes. A case
// that built four pairs and fell below the floor is a case the corpus is too
// small for; a case whose touched files had no size-matched twin anywhere is a
// case the *bound* is too tight for, and --size-ratio moves it. Reporting only
// "33 of 77" makes those look like the same problem.

// MatchedCoverage records how far the pairing reached, and where it stopped.
type MatchedCoverage struct {
	// Scored counts cases that cleared MinPairs and contributed to the column.
	Scored int `json:"scored"`
	// BelowFloor counts cases that built at least one pair but fewer than
	// MinPairs. The corpus is thin for these, not the bound.
	BelowFloor int `json:"below_floor"`
	// Unpairable counts cases that built no pairs at all: nothing the
	// contributor touched had a size-matched twin the candidate set could
	// offer. A looser bound would move these.
	Unpairable int `json:"unpairable"`
	// Ground totals the answer-set items across every case the pairing was
	// attempted on, and Paired how many of them found a twin. The ratio is
	// the sharpest statement of coverage: not how many cases were reached, but
	// how much of what the contributors actually touched.
	Ground int `json:"ground"`
	Paired int `json:"paired"`
}

// Cases is how many the pairing was attempted on.
func (c MatchedCoverage) Cases() int { return c.Scored + c.BelowFloor + c.Unpairable }

// Share is the fraction of touched items that found a size-matched twin.
func (c MatchedCoverage) Share() float64 {
	if c.Ground == 0 {
		return 0
	}
	return float64(c.Paired) / float64(c.Ground)
}

// Summary states the coverage in the terms a reader needs, including which of
// the two limits is binding.
//
// Naming the binding limit is the point. "33 of 77 cases" invites the reading
// that the corpus is too small, which is only sometimes true and is the more
// expensive of the two things to fix.
func (c MatchedCoverage) Summary() string {
	if c.Cases() == 0 {
		return ""
	}
	base := fmt.Sprintf("pairing reached %d of %d cases and %d of %d touched items (%.0f%%)",
		c.Scored, c.Cases(), c.Paired, c.Ground, c.Share()*100)
	switch {
	case c.Scored == c.Cases():
		return base + "; every case cleared the floor"
	case c.Unpairable > c.BelowFloor:
		return base + fmt.Sprintf("; %d cases found no size-matched twin at all, which a looser "+
			"--size-ratio would move, and %d built pairs but fell below the floor of %d",
			c.Unpairable, c.BelowFloor, MinPairs)
	default:
		return base + fmt.Sprintf("; %d cases built pairs but fell below the floor of %d, which "+
			"is the corpus being thin rather than the bound being tight, and %d found no twin at all",
			c.BelowFloor, MinPairs, c.Unpairable)
	}
}

// observeCoverage records one case's pairing outcome.
//
// ground is the size of the answer set the pairing was offered; pairs is what
// it built, before the MinPairs floor is applied.
func (c *MatchedCoverage) observeCoverage(ground, pairs int) {
	if ground == 0 {
		// Nothing to pair. Not a coverage failure — the case had no answer set
		// at this target, and it is counted elsewhere as unscorable.
		return
	}
	c.Ground += ground
	c.Paired += pairs
	switch {
	case pairs >= MinPairs:
		c.Scored++
	case pairs > 0:
		c.BelowFloor++
	default:
		c.Unpairable++
	}
}
