// Package vcs reads revision history. Everything here is language-agnostic:
// churn, fix density, orphaning and hidden coupling all come from history, and
// none of them care what language the files are written in.
package vcs

import (
	"context"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
)

// History provides revision data for a repository.
//
// It is an interface so the backtest can substitute a provider pinned to a
// historical commit — rewinding the index to the day before someone's first
// commit is the whole mechanism of Gate A, and it must not require mutating
// the working tree.
type History interface {
	// Commits returns revisions at or after since, oldest first.
	Commits(ctx context.Context, root string, since time.Time) ([]core.Commit, error)

	// AuthorActivity returns each author's most recent commit anywhere in the
	// repository. Orphaning is derived from this: code whose authors have all
	// gone quiet is code with nobody left to ask.
	AuthorActivity(ctx context.Context, root string) (map[string]time.Time, error)

	// HeadCommit returns the hash currently checked out.
	HeadCommit(ctx context.Context, root string) (string, error)
}

// Blamer is implemented by providers that can attribute surviving lines.
// Blame is expensive enough that it is a separate, optional capability rather
// than part of History.
type Blamer interface {
	Blame(ctx context.Context, root string, paths []string) ([]core.Authorship, error)
}

// Inactive is how long an author must be silent before the code they wrote
// counts as orphaned. Ninety days is the spec's threshold: long enough to
// survive a sabbatical, short enough to catch someone who has left.
const Inactive = 90 * 24 * time.Hour

// Orphaned reports whether an author counts as gone, as of now.
func Orphaned(lastActive, now time.Time) bool {
	if lastActive.IsZero() {
		return true
	}
	return now.Sub(lastActive) > Inactive
}
