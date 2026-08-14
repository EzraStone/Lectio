package core

import (
	"regexp"
	"strings"
	"time"
)

// Commit is one revision, reduced to what ranking needs.
type Commit struct {
	Hash        string
	AuthorName  string
	AuthorEmail string
	When        time.Time
	Subject     string
	Files       []FileChange
	// AIAssisted is set when the commit carries a machine-authorship marker
	// (a git-ai note, or a recognized Co-Authored-By trailer). Absent markers
	// mean "unknown", never "human".
	AIAssisted bool
}

// FileChange is one file's participation in a commit.
type FileChange struct {
	Path    string
	Added   int
	Deleted int
	// Renamed carries the previous path when git detected a rename, so history
	// can be followed across moves.
	Renamed string
}

// Paths returns just the paths touched by the commit.
func (c Commit) Paths() []string {
	out := make([]string, 0, len(c.Files))
	for _, f := range c.Files {
		out = append(out, f.Path)
	}
	return out
}

var (
	fixPattern    = regexp.MustCompile(`(?i)\b(fix(es|ed|up)?|bug(fix)?|hotfix|patch|repair|broken|regression|crash|panic)\b`)
	revertPattern = regexp.MustCompile(`(?i)^\s*revert\b|\brevert(s|ed|ing)?\b`)
)

// IsFix reports whether the commit subject reads as a corrective change.
// Deliberately keyword-based: a heuristic that a reader can audit beats a
// classifier they cannot.
func (c Commit) IsFix() bool { return fixPattern.MatchString(c.Subject) }

// IsRevert reports whether the commit undoes earlier work.
func (c Commit) IsRevert() bool { return revertPattern.MatchString(c.Subject) }

// Author returns the identity used for orphaning. Email is preferred because
// display names change; it is lower-cased so casing differences do not split
// one person into two.
func (c Commit) Author() string {
	if c.AuthorEmail != "" {
		return strings.ToLower(c.AuthorEmail)
	}
	return strings.ToLower(c.AuthorName)
}

// FileHistory is the per-file view the ranker consumes.
type FileHistory struct {
	Path        string
	Commits     int
	FixCommits  int
	Authors     map[string]int // author identity -> commits
	FirstTouch  time.Time
	LastTouch   time.Time
	LinesAdded  int
	LinesDelete int
}

// DistinctAuthors returns how many people have touched the file.
func (h FileHistory) DistinctAuthors() int { return len(h.Authors) }

// Authorship is a blame-derived ownership record: how many surviving lines in
// a file belong to one author, and when that author was last active anywhere
// in the repo. Orphaning falls out of the second field.
type Authorship struct {
	Path       string
	Author     string
	Lines      int
	LastActive time.Time
}
