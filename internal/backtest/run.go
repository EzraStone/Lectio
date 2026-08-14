package backtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
}

// RunOptions configure a backtest run.
type RunOptions struct {
	// K is the cutoff for precision@K. Ten, per the spec.
	K int
	// Weights overrides the ranking weights, so a run can test a candidate
	// weighting against the shipped one.
	Weights rank.Weights
	// WorkDir holds the temporary worktrees. Empty means the system temp dir.
	WorkDir string
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

	v, err := indexAt(ctx, tree, c.FirstSeen)
	if err != nil {
		res.Err = err
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
func indexAt(ctx context.Context, tree string, asOf time.Time) (*index.View, error) {
	dbDir, err := os.MkdirTemp("", "lectio-backtest-db-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dbDir)

	s, err := store.Open(ctx, filepath.Join(dbDir, "index.db"))
	if err != nil {
		return nil, err
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

	if _, err := index.Build(ctx, s, a, tree, opts); err != nil {
		return nil, fmt.Errorf("index rewound tree: %w", err)
	}

	v, err := index.Load(ctx, s)
	if err != nil {
		return nil, err
	}
	v.Now = asOf
	return v, nil
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
	Cases      int
	Failed     int
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
		v.Note = fmt.Sprintf("did not beat %v — phase 1 has failed, and no amount of interface saves it", v.Lost)
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
