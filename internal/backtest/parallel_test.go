package backtest

import (
	"context"
	"sync"
	"testing"
)

func fakeCases(n int) []Case {
	out := make([]Case, n)
	for i := range out {
		out[i] = Case{
			Repo:            "/repo",
			Contributor:     string(rune('a' + i%26)),
			RewindTo:        "rev",
			TouchedExisting: []string{"a.go"},
		}
	}
	return out
}

// Two runs of the same corpus must produce the same report. A report whose
// rows depend on which worktree finished first is not a reproducible number,
// even when the case-set fingerprint matches.
func TestRunCasesPreservesInputOrder(t *testing.T) {
	cases := fakeCases(12)
	for i := range cases {
		cases[i].FirstCommit = string(rune('A' + i))
	}

	// Every case fails fast on a nonexistent repo, which is enough to exercise
	// ordering without indexing anything.
	serial := RunCases(context.Background(), cases, RunOptions{K: 10}, nil)
	parallel := RunCases(context.Background(), cases, RunOptions{K: 10, Workers: 4}, nil)

	if len(serial) != len(parallel) {
		t.Fatalf("serial produced %d results, parallel %d", len(serial), len(parallel))
	}
	for i := range serial {
		if serial[i].Case.FirstCommit != parallel[i].Case.FirstCommit {
			t.Fatalf("position %d: serial has %q, parallel has %q — completion order leaked in",
				i, serial[i].Case.FirstCommit, parallel[i].Case.FirstCommit)
		}
	}
}

// The callback is fired from several goroutines; the caller should not have to
// know that. Interleaved half-lines of progress output are worse than none.
func TestRunCasesSerializesTheCallback(t *testing.T) {
	cases := fakeCases(20)

	var mu sync.Mutex
	var concurrent, maxConcurrent int
	RunCases(context.Background(), cases, RunOptions{K: 10, Workers: 6}, func(CaseResult) {
		mu.Lock()
		concurrent++
		if concurrent > maxConcurrent {
			maxConcurrent = concurrent
		}
		mu.Unlock()
		mu.Lock()
		concurrent--
		mu.Unlock()
	})

	if maxConcurrent > 1 {
		t.Errorf("callback ran %d at a time, want 1", maxConcurrent)
	}
}

func TestRunCasesCallsBackOncePerCase(t *testing.T) {
	for _, workers := range []int{0, 1, 4} {
		var mu sync.Mutex
		var n int
		RunCases(context.Background(), fakeCases(9), RunOptions{K: 10, Workers: workers},
			func(CaseResult) { mu.Lock(); n++; mu.Unlock() })
		if n != 9 {
			t.Errorf("workers=%d: %d callbacks for 9 cases", workers, n)
		}
	}
}

// A zero CaseResult has no error and no scores. Left in the slice, Summarize
// would count it as a scored case with nothing in it and quietly deflate every
// average.
func TestRunCasesDropsCasesThatNeverRan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := RunCases(ctx, fakeCases(10), RunOptions{K: 10, Workers: 4}, nil)
	for _, r := range got {
		if r.Err == nil && len(r.Scores) == 0 {
			t.Fatal("a case that never ran survived into the results")
		}
	}
	if rep := Summarize(got, 10); rep.Cases != 0 {
		t.Errorf("Summarize counted %d scored cases from a cancelled run", rep.Cases)
	}
}

func TestRunCasesHandlesMoreWorkersThanCases(t *testing.T) {
	got := RunCases(context.Background(), fakeCases(2), RunOptions{K: 10, Workers: 32}, nil)
	if len(got) != 2 {
		t.Errorf("got %d results for 2 cases", len(got))
	}
}
