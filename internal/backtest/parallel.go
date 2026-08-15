package backtest

import (
	"context"
	"sync"
)

// RunCases scores many cases, optionally in parallel.
//
// Results come back in the order the cases were given, never in completion
// order. Two runs of the same corpus must produce the same report, and a
// report whose rows depend on which worktree finished first is not a
// reproducible number — the case-set fingerprint would still match while the
// verbose log told a different story each time.
//
// Cancellation stops new cases from starting; the ones already running finish
// their current step. Killing a case mid-index leaves a git worktree
// registered in the repository, and a stale entry makes every later
// `git worktree add` in that repository warn.
func RunCases(ctx context.Context, cases []Case, opts RunOptions, onDone func(CaseResult)) []CaseResult {
	results := make([]CaseResult, len(cases))

	workers := opts.Workers
	if workers <= 1 {
		for i, c := range cases {
			if ctx.Err() != nil {
				break
			}
			results[i] = RunCase(ctx, c, opts)
			if onDone != nil {
				onDone(results[i])
			}
		}
		return trimEmpty(results)
	}
	if workers > len(cases) {
		workers = len(cases)
	}

	// A callback fired from several workers needs serializing, and the caller
	// should not have to know that. Progress output is the usual consumer, and
	// interleaved half-lines are worse than no progress at all.
	var mu sync.Mutex

	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				res := RunCase(ctx, cases[i], opts)
				mu.Lock()
				results[i] = res
				if onDone != nil {
					onDone(res)
				}
				mu.Unlock()
			}
		}()
	}

	for i := range cases {
		if ctx.Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	return trimEmpty(results)
}

// trimEmpty drops slots for cases that never ran, which happens when the
// context was cancelled partway. A zero CaseResult has no error and no scores,
// and Summarize would count it as a scored case with nothing in it.
func trimEmpty(results []CaseResult) []CaseResult {
	out := make([]CaseResult, 0, len(results))
	for _, r := range results {
		if r.Err == nil && len(r.Scores) == 0 && r.Case.Repo == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}
