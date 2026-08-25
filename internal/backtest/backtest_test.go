package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/vcs"
)

// fakeHistory serves a scripted commit log, so case selection is tested
// without building a repository with a hundred real commits.
type fakeHistory struct{ commits []core.Commit }

func (f fakeHistory) Commits(context.Context, string, time.Time) ([]core.Commit, error) {
	return f.commits, nil
}
func (f fakeHistory) AuthorActivity(context.Context, string) (map[string]time.Time, error) {
	return nil, nil
}
func (f fakeHistory) HeadCommit(context.Context, string) (string, error) { return "head", nil }

var _ vcs.History = fakeHistory{}

func synthHistory() fakeHistory {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	var commits []core.Commit

	// 120 commits of prior history by the founders, creating a set of files.
	for i := 0; i < 120; i++ {
		commits = append(commits, core.Commit{
			Hash:        "old" + itoa(i),
			AuthorEmail: "founder@example.com",
			When:        base.AddDate(0, 0, i),
			Subject:     "early work",
			Files:       []core.FileChange{{Path: "pkg/file" + itoa(i%10) + ".go", Added: 10}},
		})
	}

	// A newcomer arrives and touches four existing files plus one new one.
	arrival := base.AddDate(0, 0, 130)
	for i, path := range []string{"pkg/file1.go", "pkg/file2.go", "pkg/file3.go", "pkg/file4.go", "pkg/brand-new.go"} {
		commits = append(commits, core.Commit{
			Hash:        "new" + itoa(i),
			AuthorEmail: "newcomer@example.com",
			When:        arrival.AddDate(0, 0, i*3),
			Subject:     "newcomer work",
			Files:       []core.FileChange{{Path: path, Added: 5}},
		})
	}

	// Well past the ninety-day horizon: must not count.
	commits = append(commits, core.Commit{
		Hash:        "late",
		AuthorEmail: "newcomer@example.com",
		When:        arrival.AddDate(0, 6, 0),
		Subject:     "much later",
		Files:       []core.FileChange{{Path: "pkg/file9.go", Added: 5}},
	})
	return fakeHistory{commits: commits}
}

func TestFindCasesSelectsMidHistoryContributors(t *testing.T) {
	cases, err := FindCases(context.Background(), "/repo", synthHistory(), DefaultCaseOptions())
	if err != nil {
		t.Fatalf("FindCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("cases = %d (%+v), want 1", len(cases), cases)
	}

	c := cases[0]
	if c.Contributor != "newcomer@example.com" {
		t.Errorf("contributor = %q", c.Contributor)
	}
	if c.FirstCommit != "new0" {
		t.Errorf("first commit = %q, want new0", c.FirstCommit)
	}
	// The rewind point is the commit before their first.
	if c.RewindTo != "old119" {
		t.Errorf("rewind = %q, want old119", c.RewindTo)
	}
}

// A repository's founders have no onboarding to predict — they wrote the
// thing — and their first commits arrive when there is nothing to orient them
// in.
func TestFindCasesExcludesFounders(t *testing.T) {
	cases, err := FindCases(context.Background(), "/repo", synthHistory(), DefaultCaseOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Contributor == "founder@example.com" {
			t.Error("a founder was selected as an onboarding case")
		}
	}
}

// Predictions can only name files that existed, so scoring against files the
// contributor created would penalize the ranker for not seeing the future.
func TestFindCasesSeparatesNewFilesFromExisting(t *testing.T) {
	cases, _ := FindCases(context.Background(), "/repo", synthHistory(), DefaultCaseOptions())
	c := cases[0]

	if len(c.TouchedExisting) != 4 {
		t.Errorf("existing files touched = %v, want 4", c.TouchedExisting)
	}
	if len(c.Touched) != 5 {
		t.Errorf("all files touched = %v, want 5", c.Touched)
	}
	for _, f := range c.TouchedExisting {
		if f == "pkg/brand-new.go" {
			t.Error("a file the contributor created is being scored as predictable")
		}
	}
}

func TestFindCasesRespectsTheHorizon(t *testing.T) {
	cases, _ := FindCases(context.Background(), "/repo", synthHistory(), DefaultCaseOptions())
	for _, f := range cases[0].Touched {
		if f == "pkg/file9.go" {
			t.Error("a commit six months after arrival counted inside the ninety-day horizon")
		}
	}
}

func TestFindCasesNeedsEnoughHistory(t *testing.T) {
	thin := fakeHistory{commits: []core.Commit{
		{Hash: "a", AuthorEmail: "x@e.com", When: time.Now(), Files: []core.FileChange{{Path: "a.go"}}},
	}}
	if _, err := FindCases(context.Background(), "/repo", thin, DefaultCaseOptions()); err == nil {
		t.Error("a one-commit repo should be rejected: there is nothing to rewind to")
	}
}

func TestFindCasesRequiresEnoughEvidence(t *testing.T) {
	opts := DefaultCaseOptions()
	opts.MinFiles = 10 // more than the newcomer touched
	cases, err := FindCases(context.Background(), "/repo", synthHistory(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 0 {
		t.Errorf("cases = %+v, want none — the contributor touched too few files to score", cases)
	}
}

func TestFilesAsOfFollowsRenames(t *testing.T) {
	got := filesAsOf([]core.Commit{
		{Files: []core.FileChange{{Path: "old.go"}}},
		{Files: []core.FileChange{{Path: "new.go", Renamed: "old.go"}}},
	})
	if got["old.go"] {
		t.Error("a renamed-away path still counts as existing; a prediction could score a hit on a file nobody could read")
	}
	if !got["new.go"] {
		t.Error("the new path is missing")
	}
}

func TestIsSourceExcludesNoise(t *testing.T) {
	for _, p := range []string{"pkg/a.go", "main.go"} {
		if !isSource(p) {
			t.Errorf("isSource(%q) = false", p)
		}
	}
	for _, p := range []string{"README.md", "go.sum", "pkg/a_test.go", "docs/x.txt"} {
		if isSource(p) {
			t.Errorf("isSource(%q) = true", p)
		}
	}
}

// ---------------------------------------------------------------- verdict --

func TestVerdictRequiresBeatingAllFour(t *testing.T) {
	rep := Report{
		Cases: 30, K: 10,
		Aggregates: []Aggregate{
			{Strategy: "lectio", PrecisionA: 0.40},
			{Strategy: "largest files", PrecisionA: 0.10},
			{Strategy: "most churned, 12mo", PrecisionA: 0.35},
			{Strategy: "most recently modified", PrecisionA: 0.20},
			{Strategy: "most distinct authors", PrecisionA: 0.30},
		},
	}
	v := decide(rep)
	if !v.Passed {
		t.Errorf("should have passed: %+v", v)
	}
	if len(v.Beaten) != 4 {
		t.Errorf("beaten = %v, want all four", v.Beaten)
	}
}

// A gate that can be argued past is not a gate.
func TestVerdictFailsOnOneLoss(t *testing.T) {
	rep := Report{
		Cases: 30, K: 10,
		Aggregates: []Aggregate{
			{Strategy: "lectio", PrecisionA: 0.30},
			{Strategy: "largest files", PrecisionA: 0.10},
			{Strategy: "most churned, 12mo", PrecisionA: 0.35}, // loses to churn
			{Strategy: "most recently modified", PrecisionA: 0.20},
			{Strategy: "most distinct authors", PrecisionA: 0.25},
		},
	}
	v := decide(rep)
	if v.Passed {
		t.Error("losing to one baseline must fail the gate")
	}
	if len(v.Lost) != 1 || v.Lost[0] != "most churned, 12mo" {
		t.Errorf("lost = %v", v.Lost)
	}
	if v.Note == "" {
		t.Error("a failing verdict must say what failed")
	}
}

// A tie is not a win. Matching a baseline anyone could write in an afternoon
// is not evidence the seven signals are doing anything.
func TestVerdictTreatsATieAsALoss(t *testing.T) {
	rep := Report{
		Cases: 10, K: 10,
		Aggregates: []Aggregate{
			{Strategy: "lectio", PrecisionA: 0.25},
			{Strategy: "most churned, 12mo", PrecisionA: 0.25},
		},
	}
	if decide(rep).Passed {
		t.Error("a tie with a baseline passed the gate")
	}
}

func TestSummarizeCountsFailures(t *testing.T) {
	results := []CaseResult{
		{Case: Case{Contributor: "a"}, Scores: []Score{
			{Strategy: "lectio", Precision: 0.4}, {Strategy: "largest files", Precision: 0.1},
		}},
		{Case: Case{Contributor: "b"}, Err: context.Canceled},
		{Case: Case{Contributor: "c"}, Scores: []Score{
			{Strategy: "lectio", Precision: 0.6}, {Strategy: "largest files", Precision: 0.2},
		}},
	}
	rep := Summarize(results, 10)

	if rep.Cases != 2 || rep.Failed != 1 {
		t.Errorf("cases=%d failed=%d, want 2 and 1", rep.Cases, rep.Failed)
	}
	if len(rep.Aggregates) != 2 {
		t.Fatalf("aggregates = %+v", rep.Aggregates)
	}
	if rep.Aggregates[0].Strategy != "lectio" {
		t.Errorf("lectio should be reported first, got %q", rep.Aggregates[0].Strategy)
	}
	near(t, rep.Aggregates[0].PrecisionA, 0.5, "lectio mean precision")
	near(t, rep.Medians["lectio"], 0.5, "lectio median precision")
}

func TestSummarizeWithNoResults(t *testing.T) {
	rep := Summarize(nil, 10)
	if rep.Verdict.Passed {
		t.Error("an empty run must not pass the gate")
	}
	if rep.Verdict.Note == "" {
		t.Error("an empty run should say why there is no verdict")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

// ------------------------------------------------------------- index health --

func TestIndexHealthDegraded(t *testing.T) {
	cases := []struct {
		loaded, failed int
		want           float64
	}{
		{100, 0, 0},
		{100, 20, 0.20},
		{100, 90, 0.90},
		{0, 0, 1}, // nothing loaded is total degradation, not perfect health
	}
	for _, c := range cases {
		h := IndexHealth{PackagesLoaded: c.loaded, PackagesFailed: c.failed}
		if got := h.Degraded(); got != c.want {
			t.Errorf("Degraded(%d/%d) = %v, want %v", c.failed, c.loaded, got, c.want)
		}
	}
}

// A degraded index depresses lectio and leaves all four baselines intact, so
// scoring one biases the gate toward abandoning a ranking that might be fine.
func TestSummarizeExcludesDegradedCases(t *testing.T) {
	good := CaseResult{
		Health: IndexHealth{PackagesLoaded: 100},
		Scores: []Score{
			{Strategy: "lectio", Precision: 0.6},
			{Strategy: "most churned, 12mo", Precision: 0.3},
		},
	}
	// The runner marks a degraded case with Err, which Summarize already
	// counts as a failure rather than averaging in.
	degraded := CaseResult{
		Health: IndexHealth{PackagesLoaded: 100, PackagesFailed: 90},
		Err:    context.DeadlineExceeded,
	}

	rep := Summarize([]CaseResult{good, degraded}, 10)
	if rep.Cases != 1 || rep.Failed != 1 {
		t.Fatalf("cases=%d failed=%d, want 1 and 1", rep.Cases, rep.Failed)
	}
	near(t, rep.Aggregates[0].PrecisionA, 0.6, "lectio precision")
	if !rep.Verdict.Passed {
		t.Errorf("the sound case beat its baseline and should pass: %+v", rep.Verdict)
	}
}

func TestMaxDegradedIsStrictEnoughToMatter(t *testing.T) {
	// The go-git case that motivated this: 157 of 157 packages failed, which
	// halved the call graph while leaving every baseline untouched.
	h := IndexHealth{PackagesLoaded: 157, PackagesFailed: 157}
	if h.Degraded() <= MaxDegraded {
		t.Errorf("a total type-check failure (%.2f) must exceed MaxDegraded (%.2f)", h.Degraded(), MaxDegraded)
	}
	// A couple of broken packages in a large repo is normal and must not
	// discard the case.
	ok := IndexHealth{PackagesLoaded: 157, PackagesFailed: 3}
	if ok.Degraded() > MaxDegraded {
		t.Errorf("3 of 157 failed (%.2f) should still be scorable", ok.Degraded())
	}
}

func TestPrepareModulesIsSafeOnANonModule(t *testing.T) {
	// No go.mod: must return immediately rather than shelling out.
	done := make(chan struct{})
	go func() {
		prepareModules(context.Background(), t.TempDir(), 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("prepareModules blocked on a directory with no go.mod")
	}
}

// Offline runs must not stall for minutes per case.
func TestPrepareModulesCanBeDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	prepareModules(context.Background(), dir, -1)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("a negative timeout should skip fetching entirely, took %v", elapsed)
	}
}

// "We could not analyze this revision" and "something went wrong" are facts
// about different things, and merging them hides whichever is real.
func TestSummarizeSeparatesDegradedFromOtherFailures(t *testing.T) {
	results := []CaseResult{
		{Scores: []Score{{Strategy: "lectio", Precision: 0.5}}},
		{Err: &DegradedError{Health: IndexHealth{PackagesLoaded: 100, PackagesFailed: 95}}},
		{Err: &DegradedError{Health: IndexHealth{PackagesLoaded: 40, PackagesFailed: 40}}},
		{Err: context.Canceled},
	}
	rep := Summarize(results, 10)

	if rep.Cases != 1 {
		t.Errorf("scored cases = %d, want 1", rep.Cases)
	}
	if rep.Failed != 3 {
		t.Errorf("failed = %d, want 3", rep.Failed)
	}
	if rep.Degraded != 2 {
		t.Errorf("degraded = %d, want 2 — the cancelled case is not a corpus problem", rep.Degraded)
	}
}

func TestDegradedErrorReportsTheNumbers(t *testing.T) {
	err := &DegradedError{Health: IndexHealth{PackagesLoaded: 157, PackagesFailed: 157}}
	msg := err.Error()
	for _, want := range []string{"157", "100%"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q should contain %q", msg, want)
		}
	}
}

// Two runs are only comparable when they measured the same cases. Which cases
// survive depends on whether each rewound revision's dependencies resolved,
// which depends on the module cache and the network — the same command scored
// 85 cases one day and 77 the next, every per-case score identical and the
// totals half a point apart. A report that hides that looks reproducible and
// is not.
func TestCaseSetFingerprintTracksWhichCasesWereScored(t *testing.T) {
	mk := func(repo, rev, who string) CaseResult {
		return CaseResult{
			Case:   Case{Repo: repo, RewindTo: rev, Contributor: who},
			Scores: []Score{{Strategy: "lectio", Precision: 0.5}},
		}
	}
	a := mk("r", "aaa", "x")
	b := mk("r", "bbb", "y")
	c := mk("r", "ccc", "z")

	full := Summarize([]CaseResult{a, b, c}, 10)
	if full.CaseSet == "" {
		t.Fatal("no fingerprint produced")
	}

	// Order must not matter: cases arrive in filesystem order, which says
	// nothing about the population.
	if got := Summarize([]CaseResult{c, a, b}, 10); got.CaseSet != full.CaseSet {
		t.Errorf("fingerprint moved with case order: %s vs %s", got.CaseSet, full.CaseSet)
	}

	// Losing one case to a failed dependency resolution must change it.
	dropped := Summarize([]CaseResult{a, b, {Case: c.Case, Err: &DegradedError{}}}, 10)
	if dropped.CaseSet == full.CaseSet {
		t.Error("fingerprint unchanged after a case was discarded — two different populations share an identity")
	}
	if dropped.Cases != 2 {
		t.Errorf("Cases = %d, want 2", dropped.Cases)
	}
}

func TestCaseSetFingerprintIsEmptyWhenNothingScored(t *testing.T) {
	rep := Summarize([]CaseResult{{Err: &DegradedError{}}}, 10)
	if rep.CaseSet != "" {
		t.Errorf("CaseSet = %q on a run that scored nothing", rep.CaseSet)
	}
}

// The text report is for people and may change freely. The JSON is a machine
// interface, and a consumer that cannot tell which shape it received has to
// guess from the fields present.
func TestReportCarriesItsSchemaVersion(t *testing.T) {
	rep := Summarize([]CaseResult{{
		Case:   Case{Repo: "r", RewindTo: "a", Contributor: "x"},
		Scores: []Score{{Strategy: "lectio", Precision: 0.5}},
	}}, 10)

	if rep.Schema != ReportSchema {
		t.Errorf("Schema = %d, want %d", rep.Schema, ReportSchema)
	}

	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}

	// Field names are the contract. A rename is a schema bump, and these are
	// the ones a consumer would key on.
	for _, key := range []string{"schema", "cases", "aggregates", "verdict", "case_set", "target"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("JSON is missing %q: %s", key, b)
		}
	}
	// Go's default field names must not leak back in.
	for _, gone := range []string{"Cases", "PrecisionA", "CaseSet"} {
		if _, ok := raw[gone]; ok {
			t.Errorf("Go field name %q leaked into the JSON", gone)
		}
	}
}

// A holdout run reported 73 cases scored and 172 discarded, only 10 of them
// degraded. The other 162 were the machine running out of disk, and the
// summary said nothing — the totals read like a corpus with hard cases. A
// report that cannot say why it threw two thirds of its cases away is not
// reporting a measurement.
func TestSummarizeBreaksDownFailuresByCause(t *testing.T) {
	results := []CaseResult{
		{Case: Case{Repo: "r", RewindTo: "a", Contributor: "x"},
			Scores: []Score{{Strategy: "lectio", Precision: 0.4}}},
		{Err: errors.New("index rewound tree: write index: no space left on device")},
		{Err: errors.New("index rewound tree: write index: no space left on device")},
		{Err: errors.New("index rewound tree: extract symbols: no type-checkable Go packages found in /tmp/x")},
		{Err: &DegradedError{Health: IndexHealth{PackagesLoaded: 10, PackagesFailed: 9}}},
	}
	rep := Summarize(results, 10)

	if rep.Failed != 4 {
		t.Errorf("Failed = %d, want 4", rep.Failed)
	}
	if rep.Degraded != 1 {
		t.Errorf("Degraded = %d, want 1", rep.Degraded)
	}
	if got := rep.FailureReasons["out of disk"]; got != 2 {
		t.Errorf("out-of-disk count = %d, want 2: %v", got, rep.FailureReasons)
	}
	if got := rep.FailureReasons["nothing type-checkable at that revision"]; got != 1 {
		t.Errorf("type-check count = %d, want 1: %v", got, rep.FailureReasons)
	}
	// A degraded case is already counted separately and must not be
	// double-counted here.
	total := 0
	for _, n := range rep.FailureReasons {
		total += n
	}
	if total != 3 {
		t.Errorf("failure reasons total %d, want 3 — a degraded case was counted twice", total)
	}
}

func TestClassifyFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errors.New("write index: no space left on device"), "out of disk"},
		{errors.New("extract symbols: no type-checkable Go packages found"), "nothing type-checkable at that revision"},
		{errors.New("git worktree add: fatal"), "could not create a worktree"},
		{errors.New("context canceled"), "cancelled"},
		{errors.New("something nobody anticipated"), "other"},
		{nil, "unknown"},
	} {
		if got := classifyFailure(tc.err); got != tc.want {
			t.Errorf("classifyFailure(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// Reports repeat, so the ordering has to be total rather than dependent on map
// iteration.
func TestSortedFailuresIsDeterministic(t *testing.T) {
	m := map[string]int{"a": 3, "b": 3, "c": 5, "d": 1}
	first := SortedFailures(m)
	for i := 0; i < 50; i++ {
		got := SortedFailures(m)
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("order moved: %+v then %+v", first, got)
			}
		}
	}
	if first[0].Reason != "c" || first[1].Reason != "a" || first[2].Reason != "b" {
		t.Errorf("got %+v, want c, then a and b by name", first)
	}
}
