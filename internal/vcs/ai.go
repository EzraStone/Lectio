package vcs

import (
	"bufio"
	"bytes"
	"context"
	"strings"

	"github.com/EzraStone/Lectio/internal/core"
)

// AI authorship is recorded two ways in the wild, and neither is universal.
//
// The spec's preferred source is git-ai notes: a notes ref carrying structured
// authorship metadata. Where that is absent, Co-authored-by trailers naming an
// assistant are the next best thing — they are widely emitted by default and
// survive rebases.
//
// Both are opt-in markers, which sets a hard ceiling on this signal's recall.
// The absence of a marker means "unknown", never "human", and the AI-density
// signal is weighted accordingly. Inferring machine authorship from style
// would be guessing, and a tool whose trust property is "never grades by
// vibes" does not get to guess here either.
const (
	// AINotesRef is the git-ai notes ref, checked first.
	AINotesRef = "refs/notes/git-ai"
)

// aiCoAuthors are substrings matched case-insensitively against
// Co-authored-by trailer values.
var aiCoAuthors = []string{
	"noreply@anthropic.com",
	"claude",
	"copilot@github.com",
	"github-copilot",
	"cursor",
	"devin-ai",
	"codeium",
	"gemini-code-assist",
	"aider",
}

// hasAICoAuthor reports whether any trailer value names a known assistant.
func hasAICoAuthor(trailers string) bool {
	if trailers == "" {
		return false
	}
	lower := strings.ToLower(trailers)
	for _, marker := range aiCoAuthors {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// AINotes returns the set of commit hashes carrying a git-ai note.
//
// A repository without the notes ref is the common case and is not an error:
// the result is an empty set, the signal contributes nothing, and the other
// six carry the ranking.
func (g *Git) AINotes(ctx context.Context, root string) (map[string]bool, error) {
	out, err := g.run(ctx, root, "notes", "--ref="+AINotesRef, "list")
	if err != nil {
		// No such ref. Nothing to report, and nothing wrong.
		return map[string]bool{}, nil
	}

	notes := make(map[string]bool)
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		// "<note-object> <annotated-object>"
		_, commit, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		notes[strings.TrimSpace(commit)] = true
	}
	return notes, nil
}

// AnnotateAI marks commits carrying git-ai notes, in place.
//
// Trailer-derived marks set during the log parse are preserved rather than
// overwritten: the two sources are complementary and a commit flagged by
// either is flagged.
func (g *Git) AnnotateAI(ctx context.Context, root string, commits []core.Commit) error {
	notes, err := g.AINotes(ctx, root)
	if err != nil || len(notes) == 0 {
		return err
	}
	for i := range commits {
		if notes[commits[i].Hash] {
			commits[i].AIAssisted = true
		}
	}
	return nil
}
