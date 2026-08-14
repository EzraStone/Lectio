package vcs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
)

// Git reads history by shelling out to the git binary.
//
// Shelling out rather than linking a pure-Go implementation is deliberate:
// rename detection, mailmap, and pathspec handling are subtle, and the
// system git is the definition of correct for all three. The cost is a
// dependency on git being installed, which for a tool you point at a repo
// you just cloned is not much of an assumption.
type Git struct {
	// Rev pins reads to a revision. Empty means HEAD. The backtest sets this
	// to rewind the index without touching the working tree.
	Rev string

	// Exec runs a git command; overridable in tests.
	Exec func(ctx context.Context, root string, args ...string) ([]byte, error)
}

// NewGit returns a provider reading the current checkout.
func NewGit() *Git { return &Git{} }

// AtRev returns a provider pinned to a revision.
func AtRev(rev string) *Git { return &Git{Rev: rev} }

func (g *Git) run(ctx context.Context, root string, args ...string) ([]byte, error) {
	if g.Exec != nil {
		return g.Exec(ctx, root, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// rev returns the revision to read from.
func (g *Git) rev() string {
	if g.Rev == "" {
		return "HEAD"
	}
	return g.Rev
}

// HeadCommit returns the pinned revision's hash.
func (g *Git) HeadCommit(ctx context.Context, root string) (string, error) {
	out, err := g.run(ctx, root, "rev-parse", g.rev())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Field and record separators. Commit messages can contain almost anything, so
// the markers are control characters git will not emit on its own.
const (
	fieldSep  = "\x1f"
	commitSep = "\x02"
)

// Commits returns revisions at or after since, oldest first.
//
// Merge commits are excluded. With --numstat a merge either reports nothing or
// reports the union of both sides, and counting that union would double every
// change that ever went through a pull request — which is most of them, and
// would make churn a measure of branching style rather than of change.
func (g *Git) Commits(ctx context.Context, root string, since time.Time) ([]core.Commit, error) {
	args := []string{
		"log", g.rev(),
		"--no-merges",
		"--numstat",
		"--find-renames",
		"--date=unix",
		"--pretty=tformat:" + commitSep + "%H" + fieldSep + "%an" + fieldSep + "%ae" +
			fieldSep + "%at" + fieldSep + "%s" + fieldSep +
			"%(trailers:key=Co-authored-by,valueonly,separator=%x1e)",
	}
	if !since.IsZero() {
		args = append(args, "--since="+strconv.FormatInt(since.Unix(), 10))
	}

	out, err := g.run(ctx, root, args...)
	if err != nil {
		return nil, err
	}

	commits := parseLog(out)
	// git log is newest-first; every consumer wants chronological order.
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

func parseLog(out []byte) []core.Commit {
	var commits []core.Commit
	var cur *core.Commit

	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, commitSep) {
			if c, ok := parseHeader(line[len(commitSep):]); ok {
				commits = append(commits, c)
				cur = &commits[len(commits)-1]
			} else {
				cur = nil
			}
			continue
		}
		if cur == nil || line == "" {
			continue
		}
		if fc, ok := parseNumstat(line); ok {
			cur.Files = append(cur.Files, fc)
		}
	}
	return commits
}

func parseHeader(s string) (core.Commit, bool) {
	f := strings.Split(s, fieldSep)
	if len(f) < 5 {
		return core.Commit{}, false
	}
	ts, err := strconv.ParseInt(f[3], 10, 64)
	if err != nil {
		return core.Commit{}, false
	}
	c := core.Commit{
		Hash:        f[0],
		AuthorName:  f[1],
		AuthorEmail: f[2],
		When:        time.Unix(ts, 0).UTC(),
		Subject:     f[4],
	}
	if len(f) > 5 {
		c.AIAssisted = hasAICoAuthor(f[5])
	}
	return c, true
}

// parseNumstat reads one "added<TAB>deleted<TAB>path" line. Binary files
// report "-" for both counts; they are recorded with zero lines because the
// fact that a file changed is signal even when the size of the change is not.
func parseNumstat(line string) (core.FileChange, bool) {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return core.FileChange{}, false
	}
	added, aok := parseCount(parts[0])
	deleted, dok := parseCount(parts[1])
	if !aok || !dok {
		return core.FileChange{}, false
	}
	newPath, oldPath := parseRenamePath(parts[2])
	return core.FileChange{
		Path: newPath, Added: added, Deleted: deleted, Renamed: oldPath,
	}, true
}

func parseCount(s string) (int, bool) {
	if s == "-" {
		return 0, true // binary file
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// parseRenamePath decodes the paths git emits for renames, returning the new
// path and, when there was a rename, the old one.
//
// git has two spellings and both appear in practice:
//
//	old.go => new.go
//	internal/{old => new}/file.go
//
// The braced form can also have an empty side (a file moved into or out of a
// directory), which is why the result goes through path.Clean.
func parseRenamePath(s string) (newPath, oldPath string) {
	arrow := strings.Index(s, " => ")
	if arrow < 0 {
		return s, ""
	}

	if open := strings.Index(s, "{"); open >= 0 && open < arrow {
		if rel := strings.Index(s[open:], "}"); rel >= 0 && open+rel > arrow {
			closeIdx := open + rel
			prefix, suffix := s[:open], s[closeIdx+1:]
			mid := strings.SplitN(s[open+1:closeIdx], " => ", 2)
			if len(mid) == 2 {
				return path.Clean(prefix + mid[1] + suffix), path.Clean(prefix + mid[0] + suffix)
			}
		}
	}

	parts := strings.SplitN(s, " => ", 2)
	return parts[1], parts[0]
}

// AuthorActivity returns each author's most recent commit anywhere in the
// repository.
//
// The window is deliberately unbounded while everything else reads twelve
// months. Orphaning asks "is this person still around", and someone who last
// committed fourteen months ago is not more present than someone who last
// committed eleven — but a history window would report the first as having
// never existed, which reads as orphaned for a different and wrong reason.
func (g *Git) AuthorActivity(ctx context.Context, root string) (map[string]time.Time, error) {
	out, err := g.run(ctx, root, "log", g.rev(), "--pretty=tformat:%ae"+fieldSep+"%at")
	if err != nil {
		return nil, err
	}

	activity := make(map[string]time.Time)
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		email, tsRaw, ok := strings.Cut(sc.Text(), fieldSep)
		if !ok {
			continue
		}
		ts, err := strconv.ParseInt(tsRaw, 10, 64)
		if err != nil {
			continue
		}
		author := strings.ToLower(strings.TrimSpace(email))
		when := time.Unix(ts, 0).UTC()
		if prev, seen := activity[author]; !seen || when.After(prev) {
			activity[author] = when
		}
	}
	return activity, nil
}

// IsRepo reports whether root is inside a git work tree.
func (g *Git) IsRepo(ctx context.Context, root string) bool {
	out, err := g.run(ctx, root, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}
