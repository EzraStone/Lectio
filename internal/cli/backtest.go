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
		ablate    = fs.Bool("ablate", false, "also score the ranking with each signal disabled in turn")
		collapse  = fs.String("collapse", string(backtest.DefaultCollapse),
			"how symbol scores become file scores: max, mean, or sum")
		target = fs.String("target", string(backtest.DefaultTarget),
			"what to grade against: touched, corrected, symbols, or corrected-symbols")
		coupling = fs.Bool("coupling", false,
			"run the second backtest instead: does hidden coupling predict where newcomers go wrong?")
		workers = fs.Int("workers", 1,
			"cases to run at once; each one type-checks a whole repository, so raise this carefully")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	collapseRule, err := backtest.ParseCollapse(*collapse)
	if err != nil {
		return err
	}
	targetVar, err := backtest.ParseTarget(*target)
	if err != nil {
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

	if *coupling {
		return runCouplingCheck(ctx, env, repos, caseOpts, *asJSON, *verbose)
	}

	runOpts := backtest.DefaultRunOptions()
	runOpts.K = *k
	runOpts.Collapse = collapseRule
	runOpts.Target = targetVar
	runOpts.Workers = *workers
	if *offline {
		runOpts.ModuleTimeout = -1
	}
	if *ablate {
		// Costs nothing extra: every variant scores against the same index.
		runOpts.Variants = backtest.Ablations()
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

		// Progress is reported on completion rather than on start. With
		// workers > 1 a "starting" line would interleave with other workers'
		// results and read as though the wrong case had failed.
		repoName := shortRepo(root)
		done := func(res backtest.CaseResult) {
			if !*verbose {
				return
			}
			env.note("%s %s · %s · %d files touched",
				env.dim("case:"), repoName, res.Case.Contributor, len(res.Case.TouchedExisting))
			if res.Err != nil {
				env.note("  %s %v", env.warn("failed:"), res.Err)
			}
		}
		results = append(results, backtest.RunCases(ctx, cases, runOpts, done)...)

		if err := ctx.Err(); err != nil {
			return err
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
	// Only meaningful when there was a collapse. A symbolic run never reduces
	// symbols to files, and printing a rule it did not apply is a small lie in
	// the one place a report is supposed to be exact.
	if r.Collapse != "" && !r.Target.Symbolic() {
		// Stated, not assumed. The rule is worth several points, so a number
		// quoted without it cannot be reproduced or compared to another run.
		env.out("%s", env.dim(fmt.Sprintf("  symbol scores collapsed to files by %s", r.Collapse)))
	}
	if r.CaseSet != "" {
		// Two runs are only comparable when this matches. Which cases survive
		// depends on whether each revision's dependencies resolved, so the same
		// command can measure a different population on a different day.
		env.out("%s", env.dim(fmt.Sprintf("  case set %s", r.CaseSet)))
	}
	if r.Degraded > 0 {
		env.out("%s", env.warn(fmt.Sprintf(
			"  %d of those were discarded for a thin index — their revisions did not type-check", r.Degraded)))
	}
	if r.Unscorable > 0 {
		env.out("%s", env.warn(fmt.Sprintf(
			"  %d had too little ground truth on the %s target to score", r.Unscorable, r.Target)))
	}
	if r.Target != "" && r.Target != backtest.DefaultTarget {
		// Worth naming plainly: a precision@10 over declarations is not
		// comparable with one over file paths, and a reader scanning two
		// reports would otherwise assume it was.
		what := string(r.Target) + " files"
		if r.Target.Symbolic() {
			what = "declarations, not file paths"
			if r.Target == backtest.TargetCorrectedSymbols {
				what = "declarations they had to correct, not file paths"
			}
		}
		env.out("%s", env.dim(fmt.Sprintf(
			"  graded against %s — not the spec's primary measure", what)))
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
	switch {
	case r.Verdict.Hollow():
		// A pass a control undercuts is the most misleading line a report can
		// print, so it does not get printed green. The gate's rule is met and
		// its purpose is not, and both halves belong on the same line.
		env.out("%s %s", env.warn("PASS"), r.Verdict.Note)
		for _, l := range wrap(
			"A control is not a baseline and cannot fail the gate. It can show that the "+
				"result does not mean what the verdict says — which is what it is doing here.", 68) {
			env.out("%s", env.dim("  "+l))
		}
	case r.Verdict.Passed:
		env.out("%s %s", env.good("PASS"), r.Verdict.Note)
	default:
		env.out("%s %s", env.bad("FAIL"), r.Verdict.Note)
	}

	renderStrata(env, r)
	renderContributions(env, r)

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

// renderStrata prints precision inside each file-size band.
//
// The overall table cannot distinguish "chose better files" from "chose bigger
// files", and after a run where largest-files won, that is the only question
// worth asking. Splitting the same cases by candidate size answers it: a
// strategy that wins overall while losing every band is winning on the size
// composition of its top ten rather than on its choices.
func renderStrata(env *Env, r backtest.Report) {
	if len(r.Strata) == 0 {
		return
	}
	reading := backtest.ReadStrata(r)
	if len(reading.Bands) == 0 {
		return
	}

	// Same arithmetic, different unit. Labelling a declaration-banded table as
	// file sizes would describe a control the run did not apply.
	unit, candidates := "file-size", "files"
	if r.Target.Symbolic() {
		unit, candidates = "declaration-size", "declarations"
	}

	env.out("")
	env.out("%s", env.bold("Precision within "+unit+" bands"))
	env.out("%s", env.dim("  the same cases, split by how large the candidate "+candidates+" are"))
	env.out("")

	header := fmt.Sprintf("  %-26s", "strategy")
	for _, b := range reading.Bands {
		header += fmt.Sprintf(" %11s", backtest.StratumLabels[b.Stratum])
	}
	env.out("%s", header)
	env.out("  %s", dashes(26+12*len(reading.Bands)))

	byStrategy := map[string]map[int]float64{}
	var order []string
	for _, s := range r.Strata {
		if _, seen := byStrategy[s.Strategy]; !seen {
			byStrategy[s.Strategy] = map[int]float64{}
			order = append(order, s.Strategy)
		}
		byStrategy[s.Strategy][s.Stratum] = s.Precision
	}

	for _, name := range order {
		// Pad before colouring: width verbs count escape bytes.
		label := fmt.Sprintf("%-26s", name)
		if name == "lectio" {
			label = env.accent(label)
		}
		row := "  " + label
		for _, b := range reading.Bands {
			row += fmt.Sprintf(" %10.1f%%", byStrategy[name][b.Stratum]*100)
		}
		env.out("%s", row)
	}

	// The spread row is the table's own caveat. Quartiles by count do not
	// equalize size inside a band, and a band spanning 20x has not controlled
	// for the thing the table exists to control for.
	spreadRow := "  " + fmt.Sprintf("%-26s", env.dim("size spread within band"))
	for _, b := range reading.Bands {
		cell := fmt.Sprintf("%10.0fx", b.Spread)
		if b.Tight() {
			cell = env.good(cell)
		} else {
			cell = env.warn(cell)
		}
		spreadRow += " " + cell
	}
	env.out("  %s", dashes(26+12*len(reading.Bands)))
	env.out("%s", spreadRow)

	env.out("")
	env.out("%s", env.dim("  Quartiles by lines spanned, equal by "+candidates[:len(candidates)-1]+" count. Each band is scored at"))
	env.out("%s", env.dim("  its own cutoff — at most half the band, so ordering still decides it."))
	env.out("%s", env.dim("  A band with too few "+candidates+", or none the contributor touched, is omitted."))
	env.out("%s", env.dim(fmt.Sprintf(
		"  Equal by count is not equal by size: a band spanning more than %.0fx still", backtest.TightSpread)))
	env.out("%s", env.dim("  carries size information, so a result there is weaker evidence than one"))
	env.out("%s", env.dim("  from a tight band."))
	env.out("")

	render := env.warn
	if reading.OverallLectio > reading.OverallLargest || reading.LectioWins == len(reading.Bands) {
		render = env.good
	}
	lines := wrap(reading.Note, 68)
	env.out("%s %s", render("reading:"), lines[0])
	for _, l := range lines[1:] {
		env.out("         %s", l)
	}
}

// wrap breaks text at word boundaries.
//
// The size readings run to a hundred and fifty characters, and a conclusion
// that scrolls off the right edge of an eighty-column terminal is one nobody
// reads — which for the line that states what the whole table means is the
// worst place to lose text.
func wrap(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}

	var out []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			out = append(out, line)
			line = w
			continue
		}
		line += " " + w
	}
	return append(out, line)
}

// renderContributions turns an ablation into per-signal effects.
//
// The table above already holds these numbers, but reading them requires
// subtracting each ablation row from the baseline by eye across seven rows of
// near-identical figures. Doing the subtraction is the difference between a
// report someone skims and one someone acts on.
func renderContributions(env *Env, r backtest.Report) {
	cs := backtest.Contributions(r)
	if len(cs) == 0 {
		return
	}

	env.out("")
	env.out("%s", env.bold("What each signal is worth"))
	env.out("%s", env.dim("  the change in precision@"+itoa(r.K)+" from removing it"))
	env.out("")

	for _, c := range cs {
		sign := "+"
		render := env.good
		switch {
		case c.Delta < -0.001:
			sign, render = "", env.bad
		case c.Delta <= 0.001:
			sign, render = " ", env.dim
		}
		env.out("  %-20s %s", c.Signal, render(fmt.Sprintf("%s%.1f pp", sign, c.Delta*100)))
	}

	if harmful := backtest.Harmful(cs); len(harmful) > 0 {
		names := make([]string, 0, len(harmful))
		for _, c := range harmful {
			names = append(names, string(c.Signal))
		}
		env.out("")
		env.out("%s removing %s would improve the ranking.",
			env.warn("look here:"), strings.Join(names, " and "))
		env.out("%s", env.dim("A signal that hurts is the spec's named failure mode: the ranking"))
		env.out("%s", env.dim("has been fitted to a proxy rather than to what a newcomer needs."))
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
