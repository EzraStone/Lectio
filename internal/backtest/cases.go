package backtest

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/vcs"
)

// Case is one retroactive onboarding: a contributor, the moment before they
// arrived, and what they went on to touch.
type Case struct {
	Repo        string
	Contributor string

	// FirstCommit is their first commit in this repository.
	FirstCommit string
	FirstSeen   time.Time
	// RewindTo is the commit before it — the state of the world on the day
	// they joined, which is what the index is rebuilt from.
	RewindTo string

	// Touched is what they actually changed over the following ninety days.
	Touched []string
	// TouchedExisting is Touched restricted to files that already existed at
	// RewindTo. This is what predictions are scored against.
	TouchedExisting []string
	// Corrected is where their corrective commits landed: files touched by a
	// fix or revert inside the horizon, again restricted to files that already
	// existed.
	//
	// This is the spec's own tiebreaker, and it is a different target rather
	// than a different weighting. Touched-files answers "where did they go",
	// which a newcomer answers with the biggest files because that is where the
	// code is. Corrected-files answers "where did they go wrong", which is
	// nearer to what orientation is for — and it is not obviously size-biased,
	// because a mistake is about misunderstanding rather than about volume.
	Corrected []string
}

// Ground returns the file set a prediction is scored against under one target.
func (c Case) Ground(t Target) []string {
	if t == TargetCorrected {
		return c.Corrected
	}
	return c.TouchedExisting
}

// Target names what a prediction is graded against.
type Target string

const (
	// TargetTouched is every existing file the newcomer changed. The spec's
	// primary measure, and the one largest-files won on.
	TargetTouched Target = "touched"
	// TargetCorrected is the subset they had to fix or revert.
	TargetCorrected Target = "corrected"
)

// DefaultTarget is the spec's primary measure. The tiebreaker is reported
// beside it, never in place of it — swapping the target after a failing run
// and quoting only the new number would be choosing the measure that flatters.
const DefaultTarget = TargetTouched

// ParseTarget validates a target name.
func ParseTarget(s string) (Target, error) {
	switch t := Target(s); t {
	case TargetTouched, TargetCorrected:
		return t, nil
	case "":
		return DefaultTarget, nil
	default:
		return "", fmt.Errorf("unknown target %q (want touched or corrected)", s)
	}
}

// MinCorrectedFiles is the fewest corrected files a case needs to be scorable
// on the corrective target.
//
// Lower than MinFiles because corrective commits are a small fraction of a
// newcomer's output. Measured over the pinned corpus — 114 cases across 26
// repositories — the distribution is:
//
//	0 corrected files   56 cases
//	1 corrected file     9 cases
//	2 or more           49 cases   (43% of the corpus)
//
// The median newcomer corrects one pre-existing file in ninety days. Requiring
// three would leave a number computed on the contributors who broke the most
// things, which is a different population rather than a smaller sample of the
// same one. Two is the lowest bar at which a single commit cannot be the whole
// result.
//
// 43% is a real limit on this target, not a tuning knob, and it should be
// stated wherever the corrective number is. Raising this constant to rescue a
// disappointing result would be selecting the population that gives the answer.
const MinCorrectedFiles = 2

// Scorable reports whether this case has enough ground truth for a target.
func (c Case) Scorable(t Target) bool {
	if t == TargetCorrected {
		return len(c.Corrected) >= MinCorrectedFiles
	}
	return len(c.TouchedExisting) > 0
}

// CaseOptions bound which contributors make usable cases.
type CaseOptions struct {
	// Horizon is how long after arrival to observe. Ninety days, per the spec.
	Horizon time.Duration
	// MinFiles is the fewest existing files a contributor must touch to be
	// worth scoring. Someone who changed one typo tells us nothing.
	MinFiles int
	// MinPriorCommits keeps the rewind point far enough into history that
	// there is a codebase to orient someone in.
	MinPriorCommits int
	// MaxCases caps how many contributors are used per repository, so one
	// enormous project does not dominate the corpus.
	MaxCases int
}

// DefaultCaseOptions returns the spec's settings.
func DefaultCaseOptions() CaseOptions {
	return CaseOptions{
		Horizon:         90 * 24 * time.Hour,
		MinFiles:        3,
		MinPriorCommits: 100,
		MaxCases:        5,
	}
}

// FindCases identifies contributors whose first commit lands mid-history.
//
// "Mid-history" is the load-bearing constraint. A repository's founders have
// no onboarding to predict — they wrote the thing — and their first commits
// arrive when there is nothing to orient them in. Requiring a hundred commits
// of prior history is what makes the exercise resemble joining a team.
func FindCases(ctx context.Context, root string, h vcs.History, opts CaseOptions) ([]Case, error) {
	if opts.Horizon <= 0 {
		opts = DefaultCaseOptions()
	}

	commits, err := h.Commits(ctx, root, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	if len(commits) <= opts.MinPriorCommits {
		return nil, fmt.Errorf("%s has %d commits, need more than %d for a rewind to mean anything",
			root, len(commits), opts.MinPriorCommits)
	}

	// Commits arrive oldest-first, so the first sighting of an author is their
	// first commit.
	firstIndex := make(map[string]int)
	for i, c := range commits {
		who := c.Author()
		if who == "" {
			continue
		}
		if _, seen := firstIndex[who]; !seen {
			firstIndex[who] = i
		}
	}

	// Files that existed at each candidate rewind point. Built by replaying
	// history forward once rather than asking git per case, which would be one
	// process per contributor.
	var cases []Case
	for who, idx := range firstIndex {
		if idx < opts.MinPriorCommits {
			continue // a founder, or near enough
		}

		first := commits[idx]
		deadline := first.When.Add(opts.Horizon)

		existing := filesAsOf(commits[:idx])

		touched := make(map[string]bool)
		touchedExisting := make(map[string]bool)
		corrected := make(map[string]bool)
		for _, c := range commits[idx:] {
			if c.When.After(deadline) {
				break
			}
			if c.Author() != who {
				continue
			}
			corrective := c.IsFix() || c.IsRevert()
			for _, f := range c.Files {
				if !isSource(f.Path) {
					continue
				}
				touched[f.Path] = true
				if !existing[f.Path] {
					continue
				}
				touchedExisting[f.Path] = true
				if corrective {
					corrected[f.Path] = true
				}
			}
		}

		if len(touchedExisting) < opts.MinFiles {
			continue
		}

		cases = append(cases, Case{
			Repo:            root,
			Contributor:     who,
			FirstCommit:     first.Hash,
			FirstSeen:       first.When,
			RewindTo:        commits[idx-1].Hash,
			Touched:         sortedKeys(touched),
			TouchedExisting: sortedKeys(touchedExisting),
			Corrected:       sortedKeys(corrected),
		})
	}

	// Deterministic order, then take the contributors with the most evidence:
	// a case built on twenty touched files says more than one built on three.
	sort.Slice(cases, func(i, j int) bool {
		if len(cases[i].TouchedExisting) != len(cases[j].TouchedExisting) {
			return len(cases[i].TouchedExisting) > len(cases[j].TouchedExisting)
		}
		return cases[i].Contributor < cases[j].Contributor
	})
	if opts.MaxCases > 0 && len(cases) > opts.MaxCases {
		cases = cases[:opts.MaxCases]
	}
	return cases, nil
}

// filesAsOf replays history to determine which files existed at a point.
//
// Renames are followed, so a file moved before the rewind point is not counted
// as still existing under its old name — which would let a prediction score a
// hit on a path nobody could have read.
func filesAsOf(commits []core.Commit) map[string]bool {
	existing := make(map[string]bool)
	for _, c := range commits {
		for _, f := range c.Files {
			if f.Renamed != "" {
				delete(existing, f.Renamed)
			}
			existing[f.Path] = true
		}
	}
	return existing
}

// isSource excludes files no reading path would ever recommend, so a
// contributor who spent their first week updating docs and lockfiles does not
// register as unpredictable.
func isSource(path string) bool {
	if core.IsTestFile(path) {
		return false
	}
	for _, suffix := range []string{".go"} {
		if len(path) > len(suffix) && path[len(path)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
