package backtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EzraStone/Lectio/internal/adapter"
	golangadapter "github.com/EzraStone/Lectio/internal/adapter/golang"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
	"github.com/EzraStone/Lectio/internal/store"
	"github.com/EzraStone/Lectio/internal/vcs"
)

// Score is one strategy's result on one case.
type Score struct {
	Strategy  string
	Precision float64
	Recall    float64
	MRR       float64
	Predicted []string
}

// CaseResult holds every strategy's score on one case.
type CaseResult struct {
	Case    Case
	Scores  []Score
	Err     error
	Elapsed time.Duration
	// Health records how completely the rewound revision type-checked. A case
	// is only scored when the index behind it is sound enough to mean anything.
	Health IndexHealth
}

// DegradedError marks a case discarded because its index was too incomplete
// to score. It is a distinct type so a report can separate "we could not
// analyze this revision" from "something went wrong" — the first is a fact
// about the corpus and the second is a fact about the run, and confusing them
// hides whichever is actually the problem.
type DegradedError struct{ Health IndexHealth }

func (e *DegradedError) Error() string {
	return fmt.Sprintf("index too degraded to score: %d of %d packages failed to type-check (%.0f%%)",
		e.Health.PackagesFailed, e.Health.PackagesLoaded, e.Health.Degraded()*100)
}

// IndexHealth describes how much of a rewound revision the adapter could
// actually analyze.
type IndexHealth struct {
	PackagesLoaded int
	PackagesFailed int
	Symbols        int
	CallEdges      int
	Warnings       []string
}

// Degraded reports the share of packages that failed to type-check.
func (h IndexHealth) Degraded() float64 {
	if h.PackagesLoaded == 0 {
		return 1
	}
	return float64(h.PackagesFailed) / float64(h.PackagesLoaded)
}

// MaxDegraded is the share of failed packages above which a case is discarded
// rather than scored.
//
// This threshold is the difference between Gate A measuring ranking quality
// and Gate A measuring whether dependencies happened to resolve. When
// type-checking degrades, the damage is not symmetric:
//
//   - Lectio loses centrality, because the call graph is what collapses. On
//     go-git a failed load produced 19,005 edges where a clean one produced
//     36,615.
//   - Hidden coupling is worse than weakened, it is corrupted: relatedness
//     uses call edges to decide whether a co-change is already explained, so
//     missing edges make ordinary pairs look hidden and fill the
//     differentiator with noise.
//   - All four baselines are untouched. Churn, recency and author counts are
//     pure git; largest-files reads symbol counts, which survive a failed
//     type-check.
//
// So a degraded case depresses lectio and leaves the comparison intact, which
// biases the gate toward abandoning a ranking that might be fine. Discarding
// the case is the honest response; scoring it is not.
const MaxDegraded = 0.20

// RunOptions configure a backtest run.
type RunOptions struct {
	// K is the cutoff for precision@K. Ten, per the spec.
	K int
	// Weights overrides the ranking weights, so a run can test a candidate
	// weighting against the shipped one.
	Weights rank.Weights
	// WorkDir holds the temporary worktrees. Empty means the system temp dir.
	WorkDir string
	// ModuleTimeout bounds the dependency fetch per case. Zero means the
	// default; negative disables fetching entirely, for offline runs.
	ModuleTimeout time.Duration
}

// defaultModuleTimeout bounds `go mod download` for one rewound revision.
const defaultModuleTimeout = 4 * time.Minute

// prepareModules resolves a rewound revision's dependencies, best effort.
//
// A historical revision pins versions that today's module cache may not hold,
// and go/packages cannot type-check what it cannot resolve. Fetching first
// removes the most common cause of a degraded index — and it is the cause
// worth removing rather than merely detecting, because every case it rescues
// is a case Gate A gets to count.
//
// Failure is deliberately ignored. Offline runs, deleted modules, and proxies
// that no longer serve a version are all normal; indexing proceeds, and the
// health check decides whether the result is still worth scoring.
func prepareModules(ctx context.Context, tree string, timeout time.Duration) {
	if timeout < 0 {
		return
	}
	if timeout == 0 {
		timeout = defaultModuleTimeout
	}
	if _, err := os.Stat(filepath.Join(tree, "go.mod")); err != nil {
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "go", "mod", "download", "all")
	cmd.Dir = tree
	_ = cmd.Run()
}

// DefaultRunOptions returns the spec's settings.
func DefaultRunOptions() RunOptions { return RunOptions{K: 10} }

// RunCase rewinds the repository and scores every strategy against what the
// contributor actually touched.
//
// The rewind uses a detached git worktree rather than checking out in place.
// Mutating the user's working tree to run an evaluation would be a hostile
// thing for a tool to do, and a worktree gives a real filesystem at the
// historical revision, which go/packages needs — it type-checks files on disk
// and cannot be pointed at a commit.
func RunCase(ctx context.Context, c Case, opts RunOptions) CaseResult {
	start := time.Now()
	res := CaseResult{Case: c}
	if opts.K <= 0 {
		opts.K = 10
	}

	tree, cleanup, err := addWorktree(ctx, c.Repo, c.RewindTo, opts.WorkDir)
	if err != nil {
		res.Err = fmt.Errorf("rewind to %s: %w", short(c.RewindTo), err)
		res.Elapsed = time.Since(start)
		return res
	}
	defer cleanup()

	// Give the rewound revision a chance to resolve its dependencies before
	// analyzing it. Without this most degradation is self-inflicted: the
	// revision's go.mod names versions that are simply not in the local cache.
	prepareModules(ctx, tree, opts.ModuleTimeout)

	v, health, err := indexAt(ctx, tree, c.FirstSeen)
	if err != nil {
		res.Err = err
		res.Elapsed = time.Since(start)
		return res
	}
	res.Health = health

	// A case built on a broken index is not a data point, it is noise with a
	// number attached. Refusing to score it keeps it out of the average
	// instead of quietly dragging one side of the comparison down.
	if d := health.Degraded(); d > MaxDegraded {
		res.Err = &DegradedError{Health: health}
		res.Elapsed = time.Since(start)
		return res
	}

	// The clock is set to the moment the contributor arrived. Ranking with
	// today's date would let every recency-weighted signal see the future,
	// which is the classic way a backtest reports a number it cannot repeat.
	p := rank.DefaultParams()
	p.Now = c.FirstSeen

	strategies := append([]Baseline{Lectio{Weights: opts.Weights}}, Baselines()...)
	for _, s := range strategies {
		predicted := s.RankFiles(v, p)
		res.Scores = append(res.Scores, Score{
			Strategy:  s.Name(),
			Precision: PrecisionAt(predicted, c.TouchedExisting, opts.K),
			Recall:    RecallAt(predicted, c.TouchedExisting, opts.K),
			MRR:       MeanReciprocalRank(predicted, c.TouchedExisting),
			Predicted: truncate(predicted, opts.K),
		})
	}

	res.Elapsed = time.Since(start)
	return res
}

// indexAt builds an index of a worktree, with history pinned to that revision.
func indexAt(ctx context.Context, tree string, asOf time.Time) (*index.View, IndexHealth, error) {
	var health IndexHealth

	dbDir, err := os.MkdirTemp("", "lectio-backtest-db-")
	if err != nil {
		return nil, health, err
	}
	defer os.RemoveAll(dbDir)

	s, err := store.Open(ctx, filepath.Join(dbDir, "index.db"))
	if err != nil {
		return nil, health, err
	}
	defer s.Close()

	a := golangadapter.New()
	// The worktree's HEAD is already the historical revision, so a plain git
	// provider reads exactly the history that existed then. Nothing after the
	// rewind point is reachable, which is the guarantee the whole exercise
	// depends on.
	a.History = vcs.NewGit()

	opts := adapter.DefaultOptions()
	opts.RunTests = false // never execute code from an arbitrary historical revision
	opts.HistoryWindow = 365 * 24 * time.Hour
	// Anchor the window at the rewind date. Without this the window is
	// [today-12mo, today], which for a repository rewound to 2018 contains no
	// commits at all — every history signal goes silent and the ranking
	// degrades to structure while still reporting a number.
	opts.AsOf = asOf

	built, err := index.Build(ctx, s, a, tree, opts)
	if err != nil {
		return nil, health, fmt.Errorf("index rewound tree: %w", err)
	}
	health = IndexHealth{
		PackagesLoaded: built.PackagesLoaded,
		PackagesFailed: built.PackagesFailed,
		Symbols:        built.Stats.Symbols,
		CallEdges:      built.Stats.CallEdges,
		Warnings:       built.Warnings,
	}

	v, err := index.Load(ctx, s)
	if err != nil {
		return nil, health, err
	}
	v.Now = asOf
	return v, health, nil
}

// addWorktree checks out rev into a temporary detached worktree.
func addWorktree(ctx context.Context, repo, rev, workDir string) (string, func(), error) {
	base := workDir
	if base == "" {
		base = os.TempDir()
	}
	dir, err := os.MkdirTemp(base, "lectio-rewind-")
	if err != nil {
		return "", nil, err
	}
	tree := filepath.Join(dir, "tree")

	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", "--quiet", tree, rev)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", nil, fmt.Errorf("git worktree add: %w: %s", err, out)
	}

	cleanup := func() {
		// Remove the worktree through git so its administrative entry goes too;
		// a stale entry makes every later `git worktree add` in the repo warn.
		rm := exec.Command("git", "worktree", "remove", "--force", tree)
		rm.Dir = repo
		_ = rm.Run()
		os.RemoveAll(dir)
	}
	return tree, cleanup, nil
}

// Report aggregates results across cases.
type Report struct {
	Cases int
	// Failed counts cases that errored for any reason.
	Failed int
	// Degraded counts the subset of Failed discarded for a thin index. A run
	// where this is large is not measuring ranking quality, whatever number it
	// prints, and the report says so rather than leaving it to be inferred.
	Degraded   int
	K          int
	Aggregates []Aggregate
	// Medians is the median precision per strategy, keyed by name.
	Medians map[string]float64
	// Verdict states whether Gate A passed and why.
	Verdict Verdict
}

// Verdict is the go/no-go.
type Verdict struct {
	Passed bool
	// Beaten lists baselines lectio outscored on mean precision@K.
	Beaten []string
	// Lost lists baselines it did not.
	Lost []string
	Note string
}

// Summarize turns per-case results into the report Gate A is decided on.
func Summarize(results []CaseResult, k int) Report {
	rep := Report{K: k, Medians: map[string]float64{}}

	precisions := map[string][]float64{}
	recalls := map[string][]float64{}
	mrrs := map[string][]float64{}
	var order []string

	for _, r := range results {
		if r.Err != nil {
			rep.Failed++
			var degraded *DegradedError
			if errors.As(r.Err, &degraded) {
				rep.Degraded++
			}
			continue
		}
		rep.Cases++
		for _, s := range r.Scores {
			if _, seen := precisions[s.Strategy]; !seen {
				order = append(order, s.Strategy)
			}
			precisions[s.Strategy] = append(precisions[s.Strategy], s.Precision)
			recalls[s.Strategy] = append(recalls[s.Strategy], s.Recall)
			mrrs[s.Strategy] = append(mrrs[s.Strategy], s.MRR)
		}
	}

	for _, name := range order {
		rep.Aggregates = append(rep.Aggregates, Mean(name, precisions[name], recalls[name], mrrs[name]))
		rep.Medians[name] = Median(precisions[name])
	}
	rep.Verdict = decide(rep)
	return rep
}

// decide applies the gate: beat all four baselines on mean precision@K.
//
// All four, not most. The spec is unambiguous that clearing them is the whole
// test, and a gate that can be argued past is not a gate.
func decide(rep Report) Verdict {
	var lectio float64
	var found bool
	for _, a := range rep.Aggregates {
		if a.Strategy == "lectio" {
			lectio, found = a.PrecisionA, true
			break
		}
	}
	if !found || rep.Cases == 0 {
		return Verdict{Note: "no cases produced a result"}
	}

	v := Verdict{Passed: true}
	for _, a := range rep.Aggregates {
		if a.Strategy == "lectio" {
			continue
		}
		if lectio > a.PrecisionA {
			v.Beaten = append(v.Beaten, a.Strategy)
		} else {
			v.Lost = append(v.Lost, a.Strategy)
			v.Passed = false
		}
	}

	if v.Passed {
		v.Note = fmt.Sprintf("beat all %d baselines on mean precision@%d across %d cases",
			len(v.Beaten), rep.K, rep.Cases)
	} else {
		// Join explicitly: %v on a slice runs the names together with only
		// spaces between them, and these names contain spaces themselves.
		v.Note = fmt.Sprintf("did not beat %s — phase 1 has failed, and no amount of interface saves it",
			strings.Join(v.Lost, "; "))
	}
	return v
}

func truncate(xs []string, k int) []string {
	if k > 0 && len(xs) > k {
		return xs[:k]
	}
	return xs
}

func short(hash string) string {
	if len(hash) > 10 {
		return hash[:10]
	}
	return hash
}
