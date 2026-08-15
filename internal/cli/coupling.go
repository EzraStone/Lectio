package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/EzraStone/Lectio/internal/adapter"
	golangadapter "github.com/EzraStone/Lectio/internal/adapter/golang"
	"github.com/EzraStone/Lectio/internal/backtest"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
	"github.com/EzraStone/Lectio/internal/store"
	"github.com/EzraStone/Lectio/internal/vcs"
)

// runCouplingCheck is the second backtest: does hidden coupling predict where
// newcomers go wrong?
//
// It exists because precision@10 is the wrong instrument for this question.
// Gate A asks whether a top-ten list contains files someone touched, and a
// signal that fires on twenty file pairs across a repository can be entirely
// correct and still move that number by a fraction of a point. This asks the
// claim directly: newcomer corrective commits should land on hidden-coupled
// files more often than the same newcomers' ordinary commits do.
//
// Unlike Gate A this runs at HEAD, not at a rewind point. The question is
// about the signal's relationship to history rather than about predicting from
// a past vantage, and the full history is the larger sample.
func runCouplingCheck(ctx context.Context, env *Env, repos []string, caseOpts backtest.CaseOptions, asJSON, verbose bool) error {
	git := vcs.NewGit()
	var results []backtest.RepoCoupling

	for _, repo := range repos {
		root, err := repoArg([]string{repo})
		if err != nil {
			env.note("%s %s: %v", env.warn("skipping"), repo, err)
			continue
		}
		if !git.IsRepo(ctx, root) {
			env.note("%s %s is not a git repository", env.warn("skipping"), repo)
			continue
		}
		if verbose {
			env.note("%s %s", env.dim("indexing:"), shortRepo(root))
		}

		v, health, err := indexForCoupling(ctx, root)
		if err != nil {
			env.note("%s %s: %v", env.warn("skipping"), shortRepo(root), err)
			continue
		}
		// The same guard Gate A applies, and for a sharper reason. relatedness
		// uses call edges to decide whether a co-change is already explained by
		// a static dependency, so a repository that type-checks badly reports
		// *more* hidden pairs than one that resolves cleanly — the signal is
		// inflated by its own analysis failing. Measured at about 1% (go-git
		// gave 235 pairs one run and 233 another), which did not matter to a
		// result that came out at 1.02 and would matter a great deal to one
		// that came out above 1.
		if d := health.Degraded(); d > backtest.MaxDegraded {
			env.note("%s %s: %.0f%% of packages failed to type-check — hidden pairs would be inflated",
				env.warn("skipping"), shortRepo(root), d*100)
			continue
		}

		res := backtest.CheckCoupling(v, couplingParams(v), caseOpts)
		results = append(results, backtest.RepoCoupling{Repo: shortRepo(root), CouplingResult: res})

		if err := ctx.Err(); err != nil {
			return err
		}
	}

	if len(results) == 0 {
		return fmt.Errorf("no repositories could be analyzed")
	}

	pooled := backtest.PooledCoupling(results)
	if asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Repos  []backtest.RepoCoupling `json:"repos"`
			Pooled backtest.CouplingResult `json:"pooled"`
		}{results, pooled})
	}
	renderCoupling(env, results, pooled)
	return nil
}

// couplingWindow is how far back the coupling check reads. Twenty years, which
// is every Go repository in existence.
const couplingWindow = 20 * 365 * 24 * time.Hour

// couplingParams anchors the check at the repository's own last commit, over
// its whole history.
//
// Both halves matter and both are easy to get wrong the same way. HiddenCoupling
// computes co-change inside [Now-ChurnWindow, Now], so leaving Now at the wall
// clock asks a repository pinned to a 2023 commit what changed together in the
// last twelve months and correctly answers: nothing. That produced zero pairs
// across the corpus on the first attempt — the same wall-clock trap that once
// made every history baseline score 0.0%, in a different function.
//
// The window then has to match the history actually loaded, or the check reads
// a twelfth of the evidence it was given.
func couplingParams(v *index.View) rank.Params {
	p := rank.DefaultParams()
	p.ChurnWindow = couplingWindow
	p.Now = v.Now
	// Commits arrive oldest-first, so the last is the repository's most recent.
	if n := len(v.Commits); n > 0 {
		p.Now = v.Commits[n-1].When
	}
	return p
}

// indexForCoupling builds a throwaway index of a repository at HEAD.
//
// Written to a temp directory rather than the repository's own .lectio, so
// running the check against someone's corpus does not leave thirty index
// databases scattered through it.
func indexForCoupling(ctx context.Context, root string) (*index.View, backtest.IndexHealth, error) {
	var health backtest.IndexHealth

	dbDir, err := os.MkdirTemp("", "lectio-coupling-")
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
	a.History = vcs.NewGit()

	opts := adapter.DefaultOptions()
	opts.RunTests = false
	// The whole history, not the ranking window. Hidden coupling is computed
	// from co-change, and a twelve-month window on a ten-year repository
	// throws away most of the evidence the check is meant to weigh.
	opts.HistoryWindow = couplingWindow

	built, err := index.Build(ctx, s, a, root, opts)
	if err != nil {
		return nil, health, err
	}
	health = backtest.IndexHealth{
		PackagesLoaded: built.PackagesLoaded,
		PackagesFailed: built.PackagesFailed,
		Symbols:        built.Stats.Symbols,
		CallEdges:      built.Stats.CallEdges,
		Warnings:       built.Warnings,
	}

	v, err := index.Load(ctx, s)
	return v, health, err
}

func renderCoupling(env *Env, rs []backtest.RepoCoupling, pooled backtest.CouplingResult) {
	env.out("")
	env.out("%s", env.bold("Hidden coupling · does it predict where newcomers go wrong?"))
	env.out("%s", env.dim("  newcomer fixes landing on coupled files, against those newcomers' own base rate"))
	env.out("")
	env.out("  %-24s %7s %7s %9s %9s %7s", "repository", "pairs", "fixes", "on coupled", "base rate", "lift")
	env.out("  %s", dashes(70))

	for _, r := range rs {
		// Repositories with too little evidence are shown, not hidden. A table
		// listing only the ones that produced a number would look far more
		// conclusive than the run was.
		lift := env.dim("     --")
		if r.NewcomerFixes >= backtest.MinFixesForSignal && r.FixesOnCoupled >= backtest.MinHitsForSignal {
			lift = fmt.Sprintf("%7.2f", r.Lift)
			switch {
			case r.Lift >= 1.5:
				lift = env.good(lift)
			case r.Lift < 0.9:
				lift = env.bad(lift)
			}
		}
		env.out("  %-24s %7d %7d %9d %8.0f%% %s",
			truncateName(r.Repo, 24), r.Pairs, r.NewcomerFixes, r.FixesOnCoupled, r.BaseRate*100, lift)
	}

	env.out("  %s", dashes(70))
	env.out("  %-24s %7d %7d %9d %8.0f%% %7.2f",
		"pooled", pooled.Pairs, pooled.NewcomerFixes, pooled.FixesOnCoupled, pooled.BaseRate*100, pooled.Lift)

	env.out("")
	render := env.warn
	switch {
	case pooled.Lift >= 1.5:
		render = env.good
	case pooled.Lift < 0.9:
		render = env.bad
	}
	env.out("%s %s", render("reading:"), pooled.Verdict)
	env.out("")
	env.out("%s", env.dim("  Lift is the rate at which newcomer fixes touch a hidden-coupled file,"))
	env.out("%s", env.dim("  over the rate at which their ordinary commits do. 1.0 is no relationship."))
	env.out("%s", env.dim("  Comparing against their own commits rather than everyone's is what keeps"))
	env.out("%s", env.dim("  this from measuring how new someone is."))
}

func truncateName(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
