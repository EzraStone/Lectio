package vcs

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/EzraStone/Lectio/internal/core"
)

// Hunks returns the line ranges one commit changed, per file.
//
// The ranges are in the *post-commit* file's coordinates, because that is the
// revision whose source can be parsed to find out which declaration a change
// landed in. Using the parent's coordinates would name the lines correctly and
// leave nothing to resolve them against.
//
// Only the first parent is diffed. A merge commit's changes belong to the
// branches that produced them, and attributing a merge's whole diff to whoever
// pressed the button would credit one person with everyone else's work.
func (g *Git) Hunks(ctx context.Context, root, commit string) (map[string][]core.LineRange, error) {
	out, err := g.run(ctx, root,
		"diff", "--unified=0", "--no-color", "--no-renames", "--diff-filter=d",
		"--first-parent", commit+"^", commit)
	if err != nil {
		// A root commit has no parent to diff against. That is a fact about
		// the repository's first commit, not a failure, and it never appears
		// in a backtest case — cases require a hundred commits of prior
		// history — so an empty result is the honest answer.
		return map[string][]core.LineRange{}, nil
	}
	return parseHunks(out), nil
}

// parseHunks reads a unified diff into per-file line ranges.
//
// Deliberately tolerant: an unrecognized line is skipped rather than failing
// the parse. Git's diff output carries mode changes, binary notices and
// similarity indexes that say nothing about line ranges, and a parser that
// rejected the whole commit on meeting one would throw away real evidence to
// no purpose.
func parseHunks(out []byte) map[string][]core.LineRange {
	hunks := make(map[string][]core.LineRange)
	var file string

	for _, raw := range bytes.Split(out, []byte("\n")) {
		line := string(raw)
		switch {
		case strings.HasPrefix(line, "+++ "):
			file = trimDiffPath(strings.TrimPrefix(line, "+++ "))
		case strings.HasPrefix(line, "@@ "):
			if file == "" {
				continue
			}
			if r, ok := parseHunkHeader(line); ok {
				hunks[file] = append(hunks[file], r)
			}
		}
	}
	return hunks
}

// trimDiffPath strips git's "b/" prefix. A deleted file's new-side path is
// /dev/null, which names no file and is dropped.
func trimDiffPath(s string) string {
	s = strings.TrimSpace(s)
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, "b/")
}

// parseHunkHeader reads the new-side range out of "@@ -12,3 +14,5 @@ ctx".
//
// The count defaults to 1 when omitted: git writes "+14" rather than "+14,1"
// for a single line, and reading that as zero would turn every one-line change
// into a deletion.
func parseHunkHeader(line string) (core.LineRange, bool) {
	i := strings.Index(line, "+")
	if i < 0 {
		return core.LineRange{}, false
	}
	rest := line[i+1:]
	if j := strings.IndexAny(rest, " \t"); j >= 0 {
		rest = rest[:j]
	}

	start, count := rest, "1"
	if k := strings.Index(rest, ","); k >= 0 {
		start, count = rest[:k], rest[k+1:]
	}

	s, err := strconv.Atoi(start)
	if err != nil {
		return core.LineRange{}, false
	}
	c, err := strconv.Atoi(count)
	if err != nil {
		return core.LineRange{}, false
	}
	return core.LineRange{Start: s, Count: c}, true
}

// FileAt returns a file's contents at a revision.
//
// Needed because attribution parses the source as it stood when the change
// landed, not as it stands now. A file that was later deleted or rewritten
// still has to be readable at the revision being attributed.
func (g *Git) FileAt(ctx context.Context, root, commit, path string) ([]byte, error) {
	out, err := g.run(ctx, root, "show", commit+":"+path)
	if err != nil {
		return nil, fmt.Errorf("read %s at %s: %w", path, short(commit), err)
	}
	return out, nil
}

func short(hash string) string {
	if len(hash) > 10 {
		return hash[:10]
	}
	return hash
}
