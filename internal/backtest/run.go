package backtest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EzraStone/Lectio/internal/adapter"
	golangadapter "github.com/EzraStone/Lectio/internal/adapter/golang"
	"github.com/EzraStone/Lectio/internal/core"
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
	// Matched is accuracy over size-matched pairs, where 0.5 is chance. Zero
	// when the case produced too few pairs to say anything.
	Matched float64
	// Pairs is how many matched pairs backed that number.
	Pairs int
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
	// Strata holds the same scores split by file-size quartile, which is what
	// distinguishes "this strategy chooses better files" from "this strategy
	// chooses bigger files".
	Strata []StratumScore
	// Collapse records the symbol-to-file rule this case was scored under. A
	// Gate A number quoted without it is not reproducible, and the rule is
	// worth four to five points.
	Collapse Collapse
	// Target records what this case was graded against.
	Target Target
	// GroundSymbols is the attributed answer set for a symbolic target. Empty
	// for file targets, where the ground truth is on the Case itself.
	GroundSymbols []core.SymbolID
}

// UnscorableError marks a case skipped because it has too little ground truth
// for the chosen target.
//
// Distinct from a degraded index and from an ordinary failure: nothing went
// wrong here, and nothing about the ranking was measured. Counting these as
// failures would make the corrective target look like it breaks the harness,
// when what it does is ask a question most cases cannot answer.
type UnscorableError struct {
	Target Target
	Have   int
}

func (e *UnscorableError) Error() string {
	unit, need := "files", MinCorrectedFiles
	if e.Target.Symbolic() {
		unit, need = "symbols", MinSymbols
	}
	return fmt.Sprintf("not scorable on the %s target: %d %s, need %d",
		e.Target, e.Have, unit, need)
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
	// weighting against the shipped one. Ignored when Variants is set.
	Weights rank.Weights
	// Variants scores several weightings against the same index. Empty means
	// the single default weighting.
	Variants []Variant
	// Collapse is how symbol scores become file scores for every lectio
	// variant. Empty means DefaultCollapse.
	Collapse Collapse
	// Target is what predictions are graded against. Empty means
	// DefaultTarget, the spec's primary measure.
	Target Target
	// WorkDir holds the temporary worktrees. Empty means the system temp dir.
	WorkDir string
	// Workers is how many cases run at once. Zero means one.
	//
	// Each case is an independent worktree, module download and type-check, so
	// they parallelize cleanly — but only up to a point. `go mod download`
	// writes to a shared module cache, and type-checking a large repository is
	// memory-hungry enough that too many at once trades wall-clock for
	// swapping. Left at one by default, because a backtest that quietly
	// exhausts a laptop is worse than a slow one.
	Workers int
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
	if opts.K <= 0 {
		opts.K = 10
	}
	if opts.Collapse == "" {
		opts.Collapse = DefaultCollapse
	}
	if opts.Target == "" {
		opts.Target = DefaultTarget
	}
	res := CaseResult{Case: c, Collapse: opts.Collapse, Target: opts.Target}

	// Checked before the expensive part. Rewinding and indexing a revision to
	// score it against an empty ground truth costs minutes and produces a zero
	// that means "this contributor broke nothing", not "the ranking missed".
	if !c.Scorable(opts.Target) {
		res.Err = &UnscorableError{Target: opts.Target, Have: len(c.Ground(opts.Target))}
		res.Elapsed = time.Since(start)
		return res
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

	// Every variant scores against the one index built above, which is what
	// keeps an eight-way ablation the same cost as a plain run.
	variants := opts.Variants
	if len(variants) == 0 {
		variants = []Variant{{Name: DefaultVariant, Weights: opts.Weights}}
	}

	strategies := make([]Baseline, 0, len(variants)+len(Baselines())+len(Controls()))
	for _, variant := range variants {
		strategies = append(strategies, Lectio{
			Label: variant.Name, Weights: variant.Weights, Collapse: opts.Collapse,
		})
	}
	strategies = append(strategies, Baselines()...)
	strategies = append(strategies, Controls()...)

	// Computed once and shared, so every strategy is stratified against
	// identical band boundaries. Deriving them per strategy would let two rows
	// of the same table mean different things by "Q4".
	if opts.Target.Symbolic() {
		scoreSymbolic(ctx, &res, c, v, p, opts)
		res.Elapsed = time.Since(start)
		return res
	}

	sizes := FileSizes(v)
	ground := c.Ground(opts.Target)

	for _, s := range strategies {
		predicted := s.RankFiles(v, p)
		res.Scores = append(res.Scores, Score{
			Strategy:  s.Name(),
			Precision: PrecisionAt(predicted, ground, opts.K),
			Recall:    RecallAt(predicted, ground, opts.K),
			MRR:       MeanReciprocalRank(predicted, ground),
			Predicted: truncate(predicted, opts.K),
		})
		for _, ss := range scoreStrata(predicted, ground, sizes, opts.K) {
			ss.Strategy = s.Name()
			res.Strata = append(res.Strata, ss)
		}
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

// ReportSchema versions the JSON shape.
//
// The text report is for people and can change freely; the JSON is a machine
// interface, and a consumer that cannot tell which shape it received has to
// guess from the fields present. Bumped on any change that removes or renames
// a field, never on an addition.
//
// The matched-pair fields did not bump it: they are additions, so a version-1
// consumer still reads a version-1 document correctly and simply does not see
// them. Recorded here to keep the next person from bumping out of caution and
// breaking every consumer to no purpose.
const ReportSchema = 1

// Report aggregates results across cases.
type Report struct {
	// Schema is ReportSchema, emitted so a consumer can branch on it.
	Schema int `json:"schema"`
	Cases  int `json:"cases"`
	// Failed counts cases that errored for any reason.
	Failed int `json:"failed"`
	// Degraded counts the subset of Failed discarded for a thin index. A run
	// where this is large is not measuring ranking quality, whatever number it
	// prints, and the report says so rather than leaving it to be inferred.
	Degraded int `json:"degraded"`
	// Unscorable counts the subset skipped for lack of ground truth on the
	// chosen target. Reported apart from Degraded because it says something
	// about the question, not about the corpus or the run.
	Unscorable int `json:"unscorable"`
	// Target is what these cases were graded against.
	Target     Target      `json:"target"`
	K          int         `json:"k"`
	Aggregates []Aggregate `json:"aggregates"`
	// Medians is the median precision per strategy, keyed by name.
	Medians map[string]float64 `json:"medians"`
	// MatchedPairs totals the size-matched pairs behind that column, summed
	// over every case that produced any. Pairs, not cases — the distinction
	// matters because it is the number a reader sizes the result by.
	MatchedPairs int `json:"matched_pairs"`
	// MatchedCases counts the cases that contributed pairs.
	MatchedCases int `json:"matched_cases"`
	// Strata holds mean precision per strategy within each file-size quartile.
	Strata []StratumAggregate `json:"strata"`
	// Collapse is the symbol-to-file rule the scored cases ran under, carried
	// up from them rather than passed in, so the report cannot claim a rule
	// the run did not use.
	Collapse Collapse `json:"collapse"`
	// CaseSet fingerprints exactly which cases were scored.
	//
	// Two runs of this harness are only comparable when this matches. Which
	// cases survive depends on whether each rewound revision's dependencies
	// resolved, which depends on what is in the module cache and on the
	// network — so the same command on the same corpus can score 85 cases one
	// day and 77 the next, with every per-case score identical and the totals
	// half a point apart.
	//
	// The alternative to printing this is a report that looks reproducible and
	// is not, which is worse than one that admits when two numbers were
	// computed over different populations.
	CaseSet string `json:"case_set"`
	// Verdict states whether Gate A passed and why.
	Verdict Verdict `json:"verdict"`
}

// StratumAggregate is one strategy's mean precision inside one size band,
// across every case where that band had enough files and at least one touched
// file to make the number mean something.
type StratumAggregate struct {
	Strategy  string  `json:"strategy"`
	Stratum   int     `json:"stratum"`
	Label     string  `json:"label"`
	Cases     int     `json:"cases"`
	Precision float64 `json:"precision"`
	// Spread is the median within-band size ratio across those cases. A band
	// spanning 20x has not controlled for size, and a win inside it is weak
	// evidence of better choices.
	Spread float64 `json:"spread"`
}

// Verdict is the go/no-go.
type Verdict struct {
	Passed bool `json:"passed"`
	// Beaten lists baselines lectio outscored on mean precision@K.
	Beaten []string `json:"beaten"`
	// Lost lists baselines it did not.
	Lost []string `json:"lost"`
	// OutscoredByControl lists controls that beat lectio.
	//
	// A control cannot fail the gate — the spec names four baselines and
	// adding a fifth after seeing the score would be moving the goalposts. But
	// a PASS printed beside a control that doubled lectio's precision is a
	// technically-true headline, and the whole point of the controls is to
	// make that visible rather than leave it in a table for someone to notice.
	OutscoredByControl []string `json:"outscored_by_control"`
	Note               string   `json:"note"`
}

// Hollow reports a pass that a control undercuts.
func (v Verdict) Hollow() bool { return v.Passed && len(v.OutscoredByControl) > 0 }

// Summarize turns per-case results into the report Gate A is decided on.
func Summarize(results []CaseResult, k int) Report {
	rep := Report{Schema: ReportSchema, K: k, Medians: map[string]float64{}}

	precisions := map[string][]float64{}
	recalls := map[string][]float64{}
	mrrs := map[string][]float64{}
	matched := map[string][]float64{}
	matchedPairs := map[string]int{}
	strata := map[stratumKey][]float64{}
	spreads := map[stratumKey][]float64{}
	var order []string

	for _, r := range results {
		if r.Err != nil {
			rep.Failed++
			var degraded *DegradedError
			if errors.As(r.Err, &degraded) {
				rep.Degraded++
			}
			var unscorable *UnscorableError
			if errors.As(r.Err, &unscorable) {
				rep.Unscorable++
				if rep.Target == "" {
					rep.Target = unscorable.Target
				}
			}
			continue
		}
		rep.Cases++
		if rep.Collapse == "" {
			rep.Collapse = r.Collapse
			rep.Target = r.Target
		}
		for _, s := range r.Scores {
			if _, seen := precisions[s.Strategy]; !seen {
				order = append(order, s.Strategy)
			}
			precisions[s.Strategy] = append(precisions[s.Strategy], s.Precision)
			recalls[s.Strategy] = append(recalls[s.Strategy], s.Recall)
			mrrs[s.Strategy] = append(mrrs[s.Strategy], s.MRR)
			// Only cases that produced pairs. A case the pairing could not
			// reach has not been measured, and counting it as zero would report
			// every strategy far below chance in proportion to how often that
			// happened.
			if s.Pairs > 0 {
				matched[s.Strategy] = append(matched[s.Strategy], s.Matched)
				matchedPairs[s.Strategy] += s.Pairs
			}
		}
		for _, ss := range r.Strata {
			key := stratumKey{ss.Strategy, ss.Stratum}
			strata[key] = append(strata[key], ss.Precision)
			spreads[key] = append(spreads[key], ss.Spread)
		}
	}

	rep.CaseSet = fingerprint(results)

	for _, name := range order {
		a := Mean(name, precisions[name], recalls[name], mrrs[name])
		a.MatchedA = MeanMatched(matched[name])
		rep.Aggregates = append(rep.Aggregates, a)
		rep.Medians[name] = Median(precisions[name])
		if n := len(matched[name]); n > rep.MatchedCases {
			rep.MatchedCases = n
		}
		// Pairs, not cases. Every strategy faces the same pairs, so the max
		// across strategies is the run's total.
		if n := matchedPairs[name]; n > rep.MatchedPairs {
			rep.MatchedPairs = n
		}
	}
	// Strategy order follows the main table; band order is smallest to
	// largest, so a row reads left to right as size increases.
	for _, name := range order {
		for q := 0; q < NumStrata; q++ {
			xs := strata[stratumKey{name, q}]
			if len(xs) == 0 {
				continue
			}
			rep.Strata = append(rep.Strata, StratumAggregate{
				Strategy:  name,
				Stratum:   q,
				Label:     StratumLabels[q],
				Cases:     len(xs),
				Precision: mean(xs),
				// Median, not mean: one repository with a single one-line file
				// in Q1 produces a ratio in the hundreds and would set the
				// caveat for the whole corpus.
				Spread: Median(spreads[stratumKey{name, q}]),
			})
		}
	}
	rep.Verdict = decide(rep)
	return rep
}

type stratumKey struct {
	strategy string
	stratum  int
}

// fingerprint identifies the set of cases that were actually scored.
//
// Sorted before hashing, because the order cases arrive in depends on
// filesystem iteration and says nothing about which population was measured.
// Only scored cases count: a case that was discarded contributed nothing to
// any number in the report.
func fingerprint(results []CaseResult) string {
	ids := make([]string, 0, len(results))
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		ids = append(ids, r.Case.Repo+"@"+r.Case.RewindTo+"/"+r.Case.Contributor)
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)

	h := sha256.New()
	for _, id := range ids {
		_, _ = h.Write([]byte(id))
		_, _ = h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
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

	// Only the four baselines decide the gate. An ablation variant scoring
	// higher is diagnostic — it says a signal is hurting — not a gate failure,
	// and counting it as one produced a FAIL note listing "lectio
	// −hidden_coupling" as something lectio must beat.
	isBaseline := make(map[string]bool, 4)
	for _, b := range Baselines() {
		isBaseline[b.Name()] = true
	}
	isControl := make(map[string]bool, 4)
	for _, c := range Controls() {
		isControl[c.Name()] = true
	}
	for _, c := range SymbolControls() {
		isControl[c.Name()] = true
	}

	v := Verdict{Passed: true}
	for _, a := range rep.Aggregates {
		if isControl[a.Strategy] && a.PrecisionA > lectio {
			v.OutscoredByControl = append(v.OutscoredByControl, a.Strategy)
		}
		if !isBaseline[a.Strategy] {
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
		if v.Hollow() {
			v.Note += fmt.Sprintf(" — but %s scored higher, and it is a control",
				strings.Join(v.OutscoredByControl, " and "))
		}
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
