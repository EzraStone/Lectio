package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EzraStone/Lectio/internal/backtest"
	"github.com/EzraStone/Lectio/internal/corpus"
	"github.com/EzraStone/Lectio/internal/vcs"
)

func backtestCmd() *Command {
	return &Command{
		Name:    "backtest",
		Summary: "Gate A: predict what a past newcomer actually touched",
		Run:     runBacktest,
	}
}

func runBacktest(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "backtest", "backtest [flags] [repo...]")
	var (
		k         = fs.Int("k", 10, "cutoff for precision@k")
		maxCases  = fs.Int("cases", 5, "maximum contributors to test per repository")
		minPrior  = fs.Int("min-history", 100, "commits of prior history required before a rewind means anything")
		asJSON    = fs.Bool("json", false, "emit JSON instead of formatted text")
		verbose   = fs.Bool("v", false, "show each case as it runs")
		useCorpus = fs.String("corpus", "", "run against a pinned corpus manifest instead of the named repos")
		cacheDir  = fs.String("cache", "", "corpus cache directory")
		offline   = fs.Bool("offline", false, "skip dependency fetching for rewound revisions")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	repos := fs.Args()
	if *useCorpus != "" {
		resolved, err := corpusRepos(ctx, env, *useCorpus, *cacheDir)
		if err != nil {
			return err
		}
		repos = resolved
	}
	if len(repos) == 0 {
		repos = []string{"."}
	}

	caseOpts := backtest.DefaultCaseOptions()
	caseOpts.MaxCases = *maxCases
	caseOpts.MinPriorCommits = *minPrior

	runOpts := backtest.DefaultRunOptions()
	runOpts.K = *k
	if *offline {
		runOpts.ModuleTimeout = -1
	}

	git := vcs.NewGit()
	var results []backtest.CaseResult

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

		cases, err := backtest.FindCases(ctx, root, git, caseOpts)
		if err != nil {
			env.note("%s %s: %v", env.warn("skipping"), repo, err)
			continue
		}
		if len(cases) == 0 {
			env.note("%s %s: no mid-history contributors with enough evidence", env.warn("skipping"), repo)
			continue
		}

		for _, c := range cases {
			if *verbose {
				env.note("%s %s · %s · %d files touched",
					env.dim("case:"), shortRepo(root), c.Contributor, len(c.TouchedExisting))
			}
			res := backtest.RunCase(ctx, c, runOpts)
			if res.Err != nil && *verbose {
				env.note("  %s %v", env.warn("failed:"), res.Err)
			}
			results = append(results, res)

			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}

	if len(results) == 0 {
		return fmt.Errorf("no usable cases found — Gate A needs repositories with mid-history contributors")
	}

	report := backtest.Summarize(results, *k)
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	renderReport(env, report)
	return nil
}

// corpusRepos resolves a manifest to local clone paths, skipping anything not
// yet fetched.
//
// Skipping rather than failing: a corpus is materialized over many minutes and
// a single remote that moved should not block a run of the other twenty-nine.
// The count of what was skipped is reported, because a Gate A number computed
// over half the corpus is a different claim from one computed over all of it.
func corpusRepos(ctx context.Context, env *Env, manifestPath, cacheDir string) ([]string, error) {
	m, err := corpus.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	cache := corpus.NewCache(cacheDir)

	var paths []string
	var missing []string
	for _, s := range cache.Status(ctx, m) {
		if !s.Ready {
			missing = append(missing, s.Repo.Name)
			continue
		}
		paths = append(paths, cache.Path(s.Repo))
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("no corpus repositories are ready in %s — run: lectio corpus fetch", cache.Dir)
	}
	if len(missing) > 0 {
		env.note("%s %d of %d corpus repositories are not fetched and will be skipped: %s",
			env.warn("note:"), len(missing), len(m.Repos), strings.Join(missing, ", "))
	}
	env.note("%s %d repositories from %s", env.dim("corpus:"), len(paths), manifestPath)
	return paths, nil
}

func renderReport(env *Env, r backtest.Report) {
	env.out("")
	headline := fmt.Sprintf("Gate A · %d cases scored, %d discarded", r.Cases, r.Failed)
	env.out("%s", env.bold(headline))
	if r.Degraded > 0 {
		env.out("%s", env.warn(fmt.Sprintf(
			"  %d of those were discarded for a thin index — their revisions did not type-check", r.Degraded)))
	}
	env.out("")
	env.out("  %-26s %10s %10s %10s %8s", "strategy", "prec@"+itoa(r.K), "recall", "MRR", "median")
	env.out("  %s", dashes(68))

	for _, a := range r.Aggregates {
		// Pad first, colour second. Width verbs count bytes, and ANSI escapes
		// are bytes, so colouring before padding silently shifts the coloured
		// row left by the length of the escape sequence.
		label := fmt.Sprintf("%-26s", a.Strategy)
		if a.Strategy == "lectio" {
			label = env.accent(label)
		}
		env.out("  %s %9.1f%% %9.1f%% %10.3f %7.1f%%",
			label, a.PrecisionA*100, a.RecallA*100, a.MRR, r.Medians[a.Strategy]*100)
	}

	env.out("")
	if r.Verdict.Passed {
		env.out("%s %s", env.good("PASS"), r.Verdict.Note)
	} else {
		env.out("%s %s", env.bad("FAIL"), r.Verdict.Note)
	}

	// The gate is only meaningful at scale. Reporting a pass on four cases
	// without saying so would be the most flattering possible reading of the
	// evidence, which is exactly what a go/no-go must not do.
	if r.Cases < 30 {
		env.out("")
		env.out("%s", env.warn(fmt.Sprintf(
			"This is %d cases. The spec calls for roughly thirty Go repositories;", r.Cases)))
		env.out("%s", env.warn("below that, treat the verdict as a smoke test rather than an answer."))
	}
}

func dashes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '-'
	}
	return string(b)
}

func shortRepo(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
