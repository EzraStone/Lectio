package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sort"
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
		candidates = fs.Bool("candidates", false,
			"score the named candidate weightings instead of a leave-one-out ablation")
		sizeRatio = fs.Float64("size-ratio", float64(backtest.MaxSizeRatio),
			"how unequal a size-matched pair may be; 1.0 admits only exact matches")
		sweep = fs.Bool("sweep-ratio", false,
			"also report the matched column at several pairing ratios; costs almost nothing")
		replay = fs.String("replay", "",
			"re-render a saved --json report using the current analysis, without re-running the corpus")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Checked before anything else resolves a corpus or a repository: a replay
	// reads a file and touches nothing else.
	if *replay != "" {
		if err := replayRejects(fs); err != nil {
			return err
		}
		return runReplay(env, *replay, *asJSON)
	}

	collapseRule, err := backtest.ParseCollapse(*collapse)
	if err != nil {
		return err
	}
	targetVar, err := backtest.ParseTarget(*target)
	if err != nil {
		return err
	}
	// Below 1.0 nothing can ever pair, and a run that silently scored zero
	// cases would look like a corpus problem rather than a flag problem.
	if !backtest.SizeRatio(*sizeRatio).Valid() {
		return fmt.Errorf("--size-ratio %.2f is below 1.0, at which no two sizes can match", *sizeRatio)
	}

	repos := fs.Args()
	if err := checkTrailingFlags(repos); err != nil {
		return err
	}
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
	runOpts.SizeRatio = backtest.SizeRatio(*sizeRatio)
	if *sweep {
		runOpts.SweepRatios = backtest.SweepRatios
	}
	if *offline {
		runOpts.ModuleTimeout = -1
	}
	switch {
	case *candidates && *ablate:
		return fmt.Errorf("--candidates and --ablate score different variant sets; pick one")
	case *candidates:
		// The hypotheses named in candidates.go, which were written down
		// before the holdout corpus existed.
		runOpts.Variants = backtest.Candidates()
		runOpts.VariantKind = backtest.VariantsCandidates
	case *ablate:
		// Costs nothing extra: every variant scores against the same index.
		runOpts.Variants = backtest.Ablations()
		runOpts.VariantKind = backtest.VariantsAblation
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
	env.note("%s %s — %d repositories from %s",
		env.dim("corpus:"), m.Label(), len(paths), manifestPath)
	if m.Note != "" {
		for _, l := range wrap(m.Note, 68) {
			env.note("%s", env.dim("  "+l))
		}
	}
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
	if r.Variants == backtest.VariantsCandidates {
		// A different experiment from the gate, and the verdict below still
		// reads as the gate's. Saying so keeps a candidate row from being
		// mistaken for a baseline lectio must beat.
		env.out("%s", env.dim("  comparing candidate weightings — the four baselines still decide the gate"))
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
	// Everything else, by cause. A run that throws away most of its cases for
	// one reason is reporting that reason, not a measurement — and without
	// this the totals look like a corpus with hard cases.
	for _, fr := range backtest.SortedFailures(r.FailureReasons) {
		line := fmt.Sprintf("  %d %s", fr.Count, fr.Reason)
		if fr.Reason == "out of disk" || fr.Count > r.Cases {
			env.out("%s", env.bad(line+" — this run is not a measurement"))
			continue
		}
		env.out("%s", env.warn(line))
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
	// The matched column only exists on symbolic runs, and printing an empty
	// one on a file run would imply a control that was never applied.
	matched := r.MatchedPairs > 0
	if matched {
		env.out("  %-26s %10s %10s %10s %8s %13s",
			"strategy", "prec@"+itoa(r.K), "recall", "MRR", "median", "matched 95%")
		env.out("  %s", dashes(82))
	} else {
		env.out("  %-26s %10s %10s %10s %8s", "strategy", "prec@"+itoa(r.K), "recall", "MRR", "median")
		env.out("  %s", dashes(68))
	}

	for _, a := range r.Aggregates {
		// Pad first, colour second. Width verbs count bytes, and ANSI escapes
		// are bytes, so colouring before padding silently shifts the coloured
		// row left by the length of the escape sequence.
		label := fmt.Sprintf("%-26s", a.Strategy)
		if a.Strategy == "lectio" {
			label = env.accent(label)
		}
		row := fmt.Sprintf("  %s %9.1f%% %9.1f%% %10.3f %7.1f%%",
			label, a.PrecisionA*100, a.RecallA*100, a.MRR, r.Medians[a.Strategy]*100)
		if matched {
			// Chance is 50%, and the interval decides which side of it a
			// strategy is actually on. Colouring by the point estimate alone
			// promotes any row that happened to land two points high, which is
			// well inside what this corpus can resolve.
			cell := fmt.Sprintf("%7.1f%% ±%3.1f", a.MatchedA*100, a.MatchedCI.HalfWidth()*100)
			switch {
			case !a.MatchedCI.ExcludesChance():
				cell = env.dim(cell)
			case a.MatchedA > backtest.MatchedChance:
				cell = env.good(cell)
			default:
				cell = env.bad(cell)
			}
			row += " " + cell
		}
		env.out("%s", row)
	}

	if matched {
		env.out("")
		unit := "file"
		if r.Target.Symbolic() {
			unit = "declaration"
		}
		// Out of how many, not just how many. File-level matching reaches
		// under half the cases — a repository has tens of files where it has
		// thousands of declarations, so exact size matches are much rarer —
		// and a reader sizing the result needs the denominator.
		for _, l := range wrap(fmt.Sprintf(
			"matched: accuracy over %d size-matched pairs from %d of %d cases — for each "+
				"%s the contributor touched, one of the same size they did not. "+
				"%.0f%% is chance, and size cannot beat chance here by construction.",
			r.MatchedPairs, r.MatchedCases, r.Cases, unit, backtest.MatchedChance*100), 72) {
			env.out("%s", env.dim("  "+l))
		}
		// The ± is what separates a result from a row that landed high, and
		// where it comes from changes how much of it to believe. Cases from one
		// repository are not independent observations, so the interval is
		// bootstrapped over repositories; only rows whose interval clears 50%
		// are coloured.
		for _, l := range wrap(fmt.Sprintf(
			"±: half a 95%% interval, bootstrapped over the %d repositories that "+
				"produced pairs rather than over the pairs themselves. Only rows whose "+
				"interval clears %.0f%% are marked. Pairs matched within %.2fx on size.",
			bootstrapClusters(r), backtest.MatchedChance*100,
			r.SizeRatio.Or(backtest.MaxSizeRatio)), 72) {
			env.out("%s", env.dim("  "+l))
		}
		if summary := r.Coverage.Summary(); summary != "" {
			// The coverage line sits directly under the matched footnote
			// because it is the denominator that footnote is quoting, stated
			// in touched items rather than only in cases.
			for _, l := range wrap(summary, 72) {
				env.out("%s", env.dim("  "+l))
			}
		}
		renderConsistency(env, r)
		renderSweep(env, r)
		renderLeak(env, r)
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

	matched := r.MatchedPairs > 0

	env.out("")
	env.out("%s", env.bold("What each signal is worth"))
	if matched {
		env.out("%s", env.dim("  the change from removing it, on both measures"))
		env.out("")
		env.out("  %-20s %12s %12s", "", "prec@"+itoa(r.K), "matched")
	} else {
		env.out("%s", env.dim("  the change in precision@"+itoa(r.K)+" from removing it"))
		env.out("")
	}

	pp := func(d float64) (string, func(string) string) {
		switch {
		case d < -0.001:
			return "", env.bad
		case d <= 0.001:
			return " ", env.dim
		default:
			return "+", env.good
		}
	}

	for _, c := range cs {
		sign, render := pp(c.Delta)
		row := fmt.Sprintf("  %-20s %s", c.Signal,
			render(fmt.Sprintf("%11s", fmt.Sprintf("%s%.1f pp", sign, c.Delta*100))))
		if matched {
			msign, mrender := pp(c.MatchedDelta)
			row += " " + mrender(fmt.Sprintf("%11s",
				fmt.Sprintf("%s%.1f pp", msign, c.MatchedDelta*100)))
		}
		env.out("%s", row)
	}

	if matched {
		env.out("")
		for _, l := range wrap(
			"Read the matched column first. A signal can lift precision purely by "+
				"preferring larger candidates; on size-matched pairs it cannot, so a "+
				"positive delta there is information the pairing did not already remove.", 70) {
			env.out("%s", env.dim("  "+l))
		}
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

// bootstrapClusters reports how many repositories the intervals were resampled
// over, read off whichever aggregate carries an interval.
//
// Every strategy faces the same cases, so the count is the same on all of
// them; taking the max rather than the first is defensive against a strategy
// that produced no pairs at all.
func bootstrapClusters(r backtest.Report) int {
	n := 0
	for _, a := range r.Aggregates {
		if a.MatchedCI.Units > n {
			n = a.MatchedCI.Units
		}
	}
	return n
}

// renderConsistency prints how many repositories each strategy beat chance in.
//
// Only for the rows where that changes the reading. A mean and an interval
// together still cannot distinguish "a little better nearly everywhere" from
// "enormously better in three repositories and level in the rest", and those
// are different products: the first generalizes to the next repository and the
// second is a description of three.
func renderConsistency(env *Env, r backtest.Report) {
	rows := make([]backtest.Aggregate, 0, len(r.Aggregates))
	for _, a := range r.Aggregates {
		if a.MatchedRepos.Repos() > 0 {
			rows = append(rows, a)
		}
	}
	if len(rows) == 0 {
		return
	}

	corrected := rows[0].MatchedRepos.Family > 0

	env.out("")
	if corrected {
		env.out("  %-26s %10s %9s %9s  %s",
			"strategy", "repos >50%", "sign p", "adjusted", "reading")
		env.out("  %s", dashes(82))
	} else {
		env.out("  %-26s %10s %9s  %s", "strategy", "repos >50%", "sign p", "reading")
		env.out("  %s", dashes(72))
	}

	var nominal int
	for _, a := range rows {
		c := a.MatchedRepos
		reading := "a coin, repository by repository"
		switch {
		case c.NominalOnly():
			// The most interesting row in the table, because it is the one a
			// reader would otherwise quote.
			nominal++
			reading = env.warn("clears its own p, not a table of " + itoa(c.Family))
		case !c.Lopsided():
			// Left as the default. Most rows land here, which is the finding.
		case c.Above > c.Below:
			reading = env.good("above chance in most repositories")
		default:
			reading = env.bad("below chance in most repositories")
		}
		row := fmt.Sprintf("  %-26s %6d / %-3d %9.3f", a.Strategy, c.Above, c.Above+c.Below, c.P)
		if corrected {
			row += fmt.Sprintf(" %9.3f", c.PAdjusted)
		}
		env.out("%s  %s", row, reading)
	}

	note := "repos >50%: how many repositories the strategy's own cases averaged above " +
		"chance in, one vote each. sign p is two-sided and exact. A strategy " +
		"picking up something general wins most repositories by a little; one " +
		"carried by an outlier wins about half of them by a lot."
	if corrected {
		note += fmt.Sprintf(" adjusted: Holm–Bonferroni across the %d strategies scored here, "+
			"since a table of that size produces about one nominal 0.05 by construction. "+
			"Readings are made against the adjusted column.", rows[0].MatchedRepos.Family)
	}
	for _, l := range wrap(note, 72) {
		env.out("%s", env.dim("  "+l))
	}
	if nominal > 0 {
		for _, l := range wrap(fmt.Sprintf(
			"%s clears 0.05 on its own p and not after correction. That is what one "+
				"table of %d looks like when nothing in it is real — and it is also what a "+
				"real effect looks like before there is enough evidence for it, which is why "+
				"it is marked rather than hidden.",
			plural(nominal, "row"), rows[0].MatchedRepos.Family), 72) {
			env.out("%s", env.dim("  "+l))
		}
	}
}

// renderSweep prints the matched column recomputed at several pairing ratios.
//
// Read it down a column rather than across a row. A finding that holds at every
// ratio is a finding about the data; one that appears only at the loose end is
// a finding about how much size the pairing was still letting through.
func renderSweep(env *Env, r backtest.Report) {
	ratios, rows := backtest.SweepTable(r.Sweep)
	if len(ratios) == 0 {
		return
	}

	env.out("")
	header := fmt.Sprintf("  %-26s", "strategy")
	for _, ratio := range ratios {
		header += fmt.Sprintf(" %9s", fmt.Sprintf("%.2fx", float64(ratio)))
	}
	env.out("%s", header)
	env.out("  %s", dashes(28+10*len(ratios)))

	for _, row := range rows {
		label := fmt.Sprintf("%-26s", row.Strategy)
		if row.Strategy == "lectio" {
			label = env.accent(label)
		}
		line := "  " + label
		for _, ratio := range ratios {
			cell, ok := row.At(ratio)
			if !ok {
				// Not the same as chance: the question could not be asked at
				// this ratio, because too few pairs survived it.
				line += fmt.Sprintf(" %9s", env.dim("—"))
				continue
			}
			// Three states, not two. A cell whose interval was refused —
			// too few repositories to resample — is not the same as one whose
			// interval straddles chance, and printing them identically is how
			// a column resting on four repositories reads like a measurement.
			text := fmt.Sprintf("%8.1f%%", cell.Matched*100)
			switch {
			case cell.CI.Units < backtest.MinClusters:
				text = env.dim(fmt.Sprintf("%7.1f%%?", cell.Matched*100))
			case !cell.CI.ExcludesChance():
				text = env.dim(text)
			case cell.Matched > backtest.MatchedChance:
				text = env.good(text)
			default:
				text = env.bad(text)
			}
			line += " " + text
		}
		env.out("%s", line)
	}

	// The denominators, which is the half of the sweep that is easy to skip
	// and changes how every cell reads.
	counts := fmt.Sprintf("  %-26s", "cases / pairs")
	repos := fmt.Sprintf("  %-26s", "repositories")
	var thin bool
	for _, ratio := range ratios {
		cases, pairs, units := 0, 0, 0
		for _, row := range rows {
			cell, ok := row.At(ratio)
			if !ok {
				continue
			}
			if cell.Cases > cases {
				cases, pairs = cell.Cases, cell.Pairs
			}
			if cell.CI.Units > units {
				units = cell.CI.Units
			}
		}
		counts += fmt.Sprintf(" %9s", fmt.Sprintf("%d/%d", cases, pairs))
		label := itoa(units)
		if units < backtest.MinClusters {
			label += "?"
			thin = true
		}
		repos += fmt.Sprintf(" %9s", label)
	}
	env.out("  %s", dashes(28+10*len(ratios)))
	env.out("%s", env.dim(counts))
	env.out("%s", env.dim(repos))
	if thin {
		for _, l := range wrap(fmt.Sprintf(
			"? marks a column resampled over fewer than %d repositories, where no interval "+
				"is computed at all. The point estimate is still the arithmetic; it simply "+
				"carries no statement about how far it could have landed from chance by "+
				"accident.", backtest.MinClusters), 72) {
			env.out("%s", env.dim("  "+l))
		}
	}

	for _, l := range wrap(
		"Read down a column, not across a row. A tighter ratio leaves less size "+
			"information inside a pair and reaches fewer cases, so the strictest "+
			"column is the cleanest and the thinnest. A result that only appears at "+
			"the loose end is a result about the pairing bound.", 72) {
		env.out("%s", env.dim("  "+l))
	}
}

// renderLeak warns when the run's own size controls beat chance.
//
// Printed after the tables rather than before them, because it is a statement
// about how to read what is above it. Printed in warning colour because a
// leaking pairing does not invalidate the run — precision is unaffected, and
// the ordering usually survives — it inflates every size-correlated row by a
// couple of points, which is enough to turn a null into a finding.
func renderLeak(env *Env, r backtest.Report) {
	warning := backtest.PairingLeak(r)
	if warning == "" {
		return
	}
	env.out("")
	for _, l := range wrap(warning, 72) {
		env.out("%s", env.warn("  "+l))
	}
	// A sweep can do better than warn: it can name a bound that works.
	if ratio, ok := backtest.SweepLeak(r); ok && ratio < r.SizeRatio.Or(backtest.MaxSizeRatio) {
		for _, l := range wrap(fmt.Sprintf(
			"The sweep above is clean at %.2fx, which is the loosest bound in this ladder "+
				"that holds size out. Re-run with --size-ratio %.2f to read the table there.",
			ratio, ratio), 72) {
			env.out("%s", env.dim("  "+l))
		}
	}
}

// runReplay re-renders a saved report through the current analysis.
//
// The analysis has changed more often than the data. Intervals, the sign test
// over repositories and the leak check all landed after the runs that needed
// them, and each one cost another forty-minute pass over a corpus to add a
// column to numbers already on disk. A report carries its per-case scores, so
// it does not have to.
func runReplay(env *Env, path string, asJSON bool) error {
	stored, err := readReport(path)
	if err != nil {
		return err
	}
	replayed, err := backtest.Replay(stored)
	if err != nil {
		return err
	}

	if asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(replayed)
	}
	env.note("%s %s", env.dim("replaying:"), path)
	renderReport(env, replayed)
	return nil
}

// replayOnlyFlags are the flags that survive a replay: they change how a
// stored report is presented, not how it was measured.
var replayOnlyFlags = map[string]bool{"replay": true, "json": true}

// replayRejects refuses a replay combined with a flag that could only have
// applied to a fresh run.
//
// Silently ignoring them is the worse option by some distance. Every one of
// these describes something the stored report already committed to — the
// corpus it ran against, the target it graded, the ratio it paired at — and a
// reader who passed --size-ratio 1.1 alongside --replay would reasonably
// believe they were seeing the table at 1.1x. They would be seeing it at
// whatever the run used.
func replayRejects(fs *flag.FlagSet) error {
	var ignored []string
	fs.Visit(func(f *flag.Flag) {
		if !replayOnlyFlags[f.Name] {
			ignored = append(ignored, "-"+f.Name)
		}
	})
	if len(ignored) == 0 {
		return nil
	}
	sort.Strings(ignored)
	return fmt.Errorf("--replay re-renders a stored report and cannot change how it was "+
		"measured, so %s would be ignored — drop them, or re-run the corpus",
		strings.Join(ignored, ", "))
}
