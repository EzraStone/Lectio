package backtest

import (
	"context"
	"sort"

	"github.com/EzraStone/Lectio/internal/adapter"
	golangadapter "github.com/EzraStone/Lectio/internal/adapter/golang"
	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
)

// HunkReader is the part of a history provider that symbol attribution needs.
//
// Narrower than vcs.History on purpose: attribution is the only caller, and
// stating exactly what it uses keeps a large interface from being required by
// a small job.
type HunkReader interface {
	Hunks(ctx context.Context, root, commit string) (map[string][]core.LineRange, error)
	FileAt(ctx context.Context, root, commit, path string) ([]byte, error)
}

// AttributeSymbols finds which indexed symbols a contributor's commits touched.
//
// The obstacle this works around is line drift. Symbols are indexed at the
// rewind point; the newcomer's edits land up to ninety days later, by which
// time line numbers have moved — often by hundreds of lines, as other people
// commit to the same files. Intersecting raw line ranges against the indexed
// spans would attribute changes to whatever happens to occupy those lines now.
//
// So attribution goes through names instead of positions. Each commit's hunks
// are resolved against the source as it stood *at that commit*, producing
// declaration names, and those names are matched back to the symbol table.
// Names do not drift, so the mapping is stable however much the file moved.
//
// What this misses is renames: a newcomer who renames and edits a function
// registers as touching nothing. That is a real undercount and it biases
// toward reporting less evidence than exists, which is the safe direction for
// a measure being used to judge a ranking.
func AttributeSymbols(
	ctx context.Context,
	h HunkReader,
	res adapter.SpanResolver,
	v *index.View,
	repo string,
	commits []core.Commit,
	paths *PathTracker,
) ([]core.SymbolID, error) {
	if h == nil || res == nil || v == nil {
		return nil, nil
	}

	// (file at rewind, local name) -> symbol. Keyed on both because a bare
	// name collides constantly: every package has a New, and half of them
	// have a String.
	type key struct{ file, name string }
	byName := make(map[key]core.SymbolID, len(v.Symbols))
	for id, sym := range v.Symbols {
		byName[key{sym.File, localName(sym)}] = id
	}

	found := make(map[core.SymbolID]bool, 16)
	for _, c := range commits {
		hunks, err := h.Hunks(ctx, repo, c.Hash)
		if err != nil {
			// One unreadable commit is not a reason to discard a case. The
			// contributor's other commits still carry evidence.
			continue
		}
		for path, ranges := range hunks {
			if !isSource(path) {
				continue
			}
			// Where did this file live at the rewind point? A file the
			// newcomer created has no answer, and nothing at the rewind point
			// could have recommended reading it.
			origin, ok := paths.Origin(path)
			if !ok {
				continue
			}

			src, err := h.FileAt(ctx, repo, c.Hash, path)
			if err != nil {
				continue
			}
			names, _ := res.ResolveSpans(src, ranges)
			for _, name := range names {
				if id, ok := byName[key{origin, name}]; ok {
					found[id] = true
				}
			}
		}
	}

	out := make([]core.SymbolID, 0, len(found))
	for id := range found {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// PathTracker maps a file's path at some later commit back to the path it had
// at the rewind point.
//
// Renames are why this exists. A contributor who edits internal/rank/score.go
// is editing the file that was rank.go when they arrived, and scoring their
// change against a path that did not exist then would count a correct
// prediction as a miss.
type PathTracker struct {
	// current maps a present-day path to its rewind-point path. A file with
	// no entry did not exist at the rewind point.
	current map[string]string
}

// NewPathTracker seeds a tracker with the files that existed at the rewind
// point, each mapping to itself.
func NewPathTracker(existing map[string]bool) *PathTracker {
	p := &PathTracker{current: make(map[string]string, len(existing))}
	for path := range existing {
		p.current[path] = path
	}
	return p
}

// Observe replays a commit's renames so later lookups follow the move.
//
// Called in commit order. Out of order, a rename chain a -> b -> c resolves
// only as far as the last link seen, which is the failure mode this type
// exists to prevent.
func (p *PathTracker) Observe(c core.Commit) {
	for _, f := range c.Files {
		if f.Renamed == "" {
			continue
		}
		if origin, ok := p.current[f.Renamed]; ok {
			p.current[f.Path] = origin
			delete(p.current, f.Renamed)
		}
	}
}

// Origin returns the path this file had at the rewind point.
func (p *PathTracker) Origin(path string) (string, bool) {
	origin, ok := p.current[path]
	return origin, ok
}

// localName is the adapter's naming convention, kept behind a function so the
// dependency is one line rather than scattered.
func localName(sym core.Symbol) string {
	return golangadapter.LocalName(sym)
}
