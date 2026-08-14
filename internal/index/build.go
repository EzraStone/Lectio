// Package index runs the adapter over a repository and persists the result,
// then reads it back as the view that ranking and probes consume.
//
// It is the only place that knows about both the adapter seam and the store,
// which keeps analysis unaware of persistence and ranking unaware of both.
package index

import (
	"context"
	"fmt"
	"time"

	"github.com/EzraStone/Lectio/internal/adapter"
	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/store"
	"github.com/EzraStone/Lectio/internal/vcs"
)

// Result summarizes one indexing run.
type Result struct {
	Adapter        string
	Head           string
	Stats          store.Stats
	PackagesLoaded int
	PackagesFailed int
	Duration       time.Duration
	// Warnings are conditions that degrade the index without invalidating it.
	// They are surfaced rather than swallowed: a user about to trust an answer
	// deserves to know the graph behind it is partial.
	Warnings []string
}

// Diagnoser is implemented by adapters that can report partial-load damage.
type Diagnoser interface {
	LoadDiagnostics(ctx context.Context, root string) (loaded, failed int, err error)
}

// Build analyzes root and replaces the index in s. User state is untouched.
func Build(ctx context.Context, s *store.Store, a adapter.LanguageAdapter, root string, opts adapter.Options) (Result, error) {
	start := time.Now()
	res := Result{Adapter: a.Name()}

	if c, ok := a.(adapter.Configurable); ok {
		c.Configure(opts)
	}

	symbols, err := a.Symbols(ctx, root)
	if err != nil {
		return res, fmt.Errorf("extract symbols: %w", err)
	}
	if len(symbols) == 0 {
		return res, fmt.Errorf("no symbols found in %s", root)
	}

	calls, imports, err := a.CallEdges(ctx, root)
	if err != nil {
		return res, fmt.Errorf("extract call graph: %w", err)
	}

	// Coverage is a nice-to-have. Losing it costs precision on blast-radius
	// answers; losing the index costs everything.
	coverage, err := a.TestCoverage(ctx, root)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("coverage unavailable: %v", err))
		coverage = nil
	}

	since := time.Time{}
	if opts.HistoryWindow > 0 {
		since = time.Now().Add(-opts.HistoryWindow)
	}
	commits, err := a.FileHistory(ctx, root, since)
	if err != nil {
		// A repo with no git history still ranks on centrality and depth. Five
		// of the seven signals go quiet, which is worth a loud warning and not
		// worth a failure.
		res.Warnings = append(res.Warnings, fmt.Sprintf("history unavailable, ranking will use structure only: %v", err))
		commits = nil
	}

	git := vcs.NewGit()
	if len(commits) > 0 {
		if err := git.AnnotateAI(ctx, root, commits); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("git-ai notes unreadable: %v", err))
		}
	}
	if head, err := git.HeadCommit(ctx, root); err == nil {
		res.Head = head
	}

	authorship := deriveAuthorship(ctx, git, root, commits, &res)

	err = s.WriteIndex(ctx, func(w *store.IndexWriter) error {
		for _, sym := range symbols {
			if err := w.Symbol(sym); err != nil {
				return err
			}
		}
		for _, e := range calls {
			if err := w.CallEdge(e); err != nil {
				return err
			}
		}
		for _, e := range imports {
			if err := w.ImportEdge(e); err != nil {
				return err
			}
		}
		for _, c := range coverage {
			if err := w.Coverage(c); err != nil {
				return err
			}
		}
		for _, c := range commits {
			if err := w.Commit(c); err != nil {
				return err
			}
		}
		for _, a := range authorship {
			if err := w.Authorship(a); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return res, fmt.Errorf("write index: %w", err)
	}

	if d, ok := a.(Diagnoser); ok {
		if loaded, failed, err := d.LoadDiagnostics(ctx, root); err == nil {
			res.PackagesLoaded, res.PackagesFailed = loaded, failed
			if failed > 0 {
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"%d of %d packages failed to type-check; their call edges are missing", failed, loaded))
			}
		}
	}

	for key, value := range map[string]string{
		"adapter":    a.Name(),
		"root":       root,
		"head":       res.Head,
		"indexed_at": time.Now().UTC().Format(time.RFC3339),
	} {
		if err := s.SetMeta(ctx, key, value); err != nil {
			return res, err
		}
	}

	if res.Stats, err = s.Stats(ctx); err != nil {
		return res, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

// deriveAuthorship attributes surviving code to authors, and records when each
// author was last active anywhere in the repository.
//
// Lines added per author per file is used rather than git blame. Blame is
// exact and costs one process per file — on a repo of any size that dominates
// the entire indexing run. Added-lines is a good proxy for a signal whose
// question is "has everyone who wrote this gone quiet", where being off by a
// few lines changes nothing and being ten minutes slower changes whether
// anyone runs the tool twice.
func deriveAuthorship(ctx context.Context, git *vcs.Git, root string, commits []core.Commit, res *Result) []core.Authorship {
	if len(commits) == 0 {
		return nil
	}

	activity, err := git.AuthorActivity(ctx, root)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("author activity unavailable: %v", err))
		activity = map[string]time.Time{}
	}

	type key struct{ path, author string }
	lines := make(map[key]int)
	for _, c := range commits {
		author := c.Author()
		if author == "" {
			continue
		}
		for _, f := range c.Files {
			lines[key{f.Path, author}] += f.Added
		}
	}

	out := make([]core.Authorship, 0, len(lines))
	for k, n := range lines {
		out = append(out, core.Authorship{
			Path:       k.path,
			Author:     k.author,
			Lines:      n,
			LastActive: activity[k.author],
		})
	}
	return out
}
