package backtest

import (
	"context"

	"github.com/EzraStone/Lectio/internal/adapter"
	golangadapter "github.com/EzraStone/Lectio/internal/adapter/golang"
	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
	"github.com/EzraStone/Lectio/internal/vcs"
)

// scoreSymbolic grades one case in declarations rather than file paths.
//
// The ground truth is not known until here. Attribution matches declaration
// names against the symbol table, and the table does not exist until the
// rewound revision has been indexed — so unlike the file targets, a symbolic
// case can turn out to be unscorable only after the expensive part is done.
//
// Attribution reads the *original* repository, not the rewound worktree. The
// commits being attributed happened after the rewind point and are not
// reachable from the detached worktree's HEAD, though they are perfectly
// reachable from the repository that worktree belongs to.
func scoreSymbolic(ctx context.Context, res *CaseResult, c Case, v *index.View, p rank.Params, opts RunOptions) {
	a := golangadapter.New()
	resolver, ok := any(a).(adapter.SpanResolver)
	if !ok {
		res.Err = &UnscorableError{Target: opts.Target}
		return
	}

	// Only the hashes are carried on a case; AttributeSymbols re-reads each
	// commit's diff from git, so a bare hash is all it needs.
	commits := make([]core.Commit, 0, len(c.CommitsFor(opts.Target)))
	for _, h := range c.CommitsFor(opts.Target) {
		commits = append(commits, core.Commit{Hash: h})
	}

	ground, err := AttributeSymbols(ctx, vcs.NewGit(), resolver, v, c.Repo,
		commits, PathTrackerFrom(c.PathOrigins))
	if err != nil {
		res.Err = err
		return
	}

	// The guard has to live here rather than in Scorable, because until the
	// index existed there was no way to know how many declarations the
	// contributor's commits would resolve to. A case that resolves to one
	// symbol is a coin flip with a number attached.
	if len(ground) < MinSymbols {
		res.Err = &UnscorableError{Target: opts.Target, Have: len(ground)}
		return
	}
	res.GroundSymbols = ground

	variants := opts.Variants
	if len(variants) == 0 {
		variants = []Variant{{Name: DefaultVariant, Weights: opts.Weights}}
	}

	strategies := make([]SymbolStrategy, 0, len(variants)+8)
	for _, variant := range variants {
		strategies = append(strategies, LectioSymbols{Label: variant.Name, Weights: variant.Weights})
	}
	strategies = append(strategies, SymbolBaselines()...)
	strategies = append(strategies, SymbolControls()...)

	// Banded by declaration size, for the same reason the file targets are
	// banded by file size: once a size heuristic wins, the question is whether
	// it chooses better declarations or merely longer ones.
	sizes := SymbolSizes(v)

	want := symbolIDs(ground)

	// Size-matched pairs, built once and shared. Every strategy has to face the
	// same pairs or the column compares different questions.
	pairs := BuildMatchedPairsAt(want, sizes, opts.SizeRatio)
	if len(pairs) < MinPairs {
		// Not an error: the case is still scored on precision, and the matched
		// column simply has nothing to say about it.
		pairs = nil
	}

	for _, s := range strategies {
		predicted := symbolIDs(s.RankSymbols(v, p))
		matched, nPairs := ScoreMatchedPairs(predicted, pairs)
		res.Scores = append(res.Scores, Score{
			Strategy:  s.Name(),
			Precision: PrecisionAt(predicted, want, opts.K),
			Recall:    RecallAt(predicted, want, opts.K),
			MRR:       MeanReciprocalRank(predicted, want),
			Predicted: truncate(predicted, opts.K),
			Matched:   matched,
			Pairs:     nPairs,
		})
		for _, ss := range scoreStrata(predicted, want, sizes, opts.K) {
			ss.Strategy = s.Name()
			res.Strata = append(res.Strata, ss)
		}
	}
}
