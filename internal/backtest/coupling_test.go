package backtest

import (
	"strings"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/graph"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
)

var couplingNow = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// couplingView builds a repo where serialize.go and mobile/schema.json change
// together without importing each other, then scripts newcomer behavior.
func couplingView(fixesOnCoupled, fixesElsewhere, ordinaryOnCoupled, ordinaryElsewhere int) *index.View {
	v := &index.View{
		Symbols:   map[core.SymbolID]core.Symbol{},
		CoveredBy: map[core.SymbolID][]core.SymbolID{},
		Calls:     graph.New(0),
	}
	for _, s := range []struct{ id, file, pkg string }{
		{"mod/api.Serialize", "internal/api/serialize.go", "mod/api"},
		{"mod/other.Thing", "internal/other/thing.go", "mod/other"},
	} {
		id := core.SymbolID(s.id)
		v.Symbols[id] = core.Symbol{ID: id, Name: s.id, File: s.file, Package: s.pkg}
		v.Calls.Add(s.id)
	}
	v.StaticCalls = v.Calls

	base := couplingNow.AddDate(0, -10, 0)
	add := func(when time.Time, author, subject string, files ...string) {
		c := core.Commit{Hash: subject + when.String(), Subject: subject, AuthorEmail: author, When: when}
		for _, f := range files {
			c.Files = append(c.Files, core.FileChange{Path: f, Added: 5})
		}
		v.Commits = append(v.Commits, c)
	}

	// Founder history: establishes the hidden pair and satisfies MinPriorCommits.
	for i := 0; i < 130; i++ {
		if i%3 == 0 {
			add(base.AddDate(0, 0, i), "founder@example.com", "update payload",
				"internal/api/serialize.go", "mobile/schema.json")
		} else {
			add(base.AddDate(0, 0, i), "founder@example.com", "other work", "internal/other/thing.go")
		}
	}

	// A newcomer arrives after all that.
	arrival := base.AddDate(0, 0, 140)
	for i := 0; i < fixesOnCoupled; i++ {
		add(arrival.AddDate(0, 0, i), "newcomer@example.com", "fix: forgot the schema",
			"internal/api/serialize.go")
	}
	for i := 0; i < fixesElsewhere; i++ {
		add(arrival.AddDate(0, 0, i), "newcomer@example.com", "fix: unrelated bug",
			"internal/other/thing.go")
	}
	for i := 0; i < ordinaryOnCoupled; i++ {
		add(arrival.AddDate(0, 0, i), "newcomer@example.com", "add a feature",
			"internal/api/serialize.go")
	}
	for i := 0; i < ordinaryElsewhere; i++ {
		add(arrival.AddDate(0, 0, i), "newcomer@example.com", "add a feature",
			"internal/other/thing.go")
	}
	return v
}

func couplingParams() rank.Params {
	p := rank.DefaultParams()
	p.Now = couplingNow
	p.ChurnWindow = 400 * 24 * time.Hour
	return p
}

// The hypothesis: newcomer corrective commits cluster on hidden-coupled files.
func TestCouplingPredictsSurprise(t *testing.T) {
	v := couplingView(18, 2, 2, 18)
	got := CheckCoupling(v, couplingParams(), DefaultCaseOptions())

	if got.Pairs == 0 {
		t.Fatalf("no hidden pairs found: %+v", got)
	}
	if got.NewcomerFixes < MinFixesForSignal {
		t.Fatalf("fixture produced too few fixes: %+v", got)
	}
	if got.Lift <= 1 {
		t.Errorf("lift = %v, expected fixes to concentrate on coupled files: %+v", got.Lift, got)
	}
	if !strings.Contains(got.Verdict, "concentrate") {
		t.Errorf("verdict = %q", got.Verdict)
	}
}

// The opposite result has to be reportable, or the backtest is decoration.
// Coupled files get an equal share of corrective and ordinary work here, which
// is exactly what "no relationship" looks like.
func TestCouplingReportsANullResult(t *testing.T) {
	v := couplingView(8, 12, 8, 12)
	got := CheckCoupling(v, couplingParams(), DefaultCaseOptions())

	if got.Lift >= 1.1 || got.Lift <= 0.9 {
		t.Errorf("lift = %v, expected roughly 1.0: %+v", got.Lift, got)
	}
	if !strings.Contains(got.Verdict, "no relationship") {
		t.Errorf("a null result was not reported as one: %q", got.Verdict)
	}
}

// Twenty-six fixes clears the denominator guard comfortably, but if one of
// them touched a coupled file the lift is decided by that single commit.
func TestCouplingRefusesOnTooFewHits(t *testing.T) {
	v := couplingView(1, 25, 0, 20)
	got := CheckCoupling(v, couplingParams(), DefaultCaseOptions())

	if got.NewcomerFixes < MinFixesForSignal {
		t.Fatalf("fixture should clear the fix-count guard: %+v", got)
	}
	if got.Lift <= 1 {
		t.Fatalf("fixture should produce a misleadingly high lift: %+v", got)
	}
	if !strings.Contains(got.Verdict, "apart from chance") {
		t.Errorf("a lift built on one hit was reported as a finding: %q", got.Verdict)
	}
}

// Three fixes, two on coupled files, is a lift of 2.0 that would vanish on any
// other sample. Reporting it as a result would be exactly the error the spec
// warns about.
func TestCouplingRefusesToSpeakOnThinEvidence(t *testing.T) {
	v := couplingView(2, 1, 0, 5)
	got := CheckCoupling(v, couplingParams(), DefaultCaseOptions())

	if got.NewcomerFixes >= MinFixesForSignal {
		t.Fatalf("fixture is not thin enough: %+v", got)
	}
	if !strings.Contains(got.Verdict, "sample-size") {
		t.Errorf("verdict = %q, want an explicit refusal to conclude", got.Verdict)
	}
}

func TestCouplingWithNoPairs(t *testing.T) {
	v := &index.View{
		Symbols:   map[core.SymbolID]core.Symbol{},
		CoveredBy: map[core.SymbolID][]core.SymbolID{},
		Calls:     graph.New(0),
	}
	v.StaticCalls = v.Calls

	got := CheckCoupling(v, couplingParams(), DefaultCaseOptions())
	if got.Pairs != 0 || !strings.Contains(got.Verdict, "nothing to test") {
		t.Errorf("result = %+v", got)
	}
}

// Comparing newcomer fixes against a repo-wide base rate would measure how new
// someone is rather than whether the pairs predict anything.
func TestBaseRateComesFromTheSameNewcomers(t *testing.T) {
	v := couplingView(18, 2, 2, 18)
	got := CheckCoupling(v, couplingParams(), DefaultCaseOptions())

	if got.BaseRate <= 0 || got.BaseRate >= 1 {
		t.Errorf("base rate = %v, expected it drawn from newcomer commits generally", got.BaseRate)
	}
	// The founders' commits are far more concentrated on the coupled pair; if
	// they leaked into the base rate it would sit much higher.
	if got.BaseRate > 0.75 {
		t.Errorf("base rate = %v, suspiciously high — founder commits may be leaking in", got.BaseRate)
	}
}

func TestTopCoupledPairsShowsItsWorking(t *testing.T) {
	v := couplingView(18, 2, 2, 18)
	pairs := TopCoupledPairs(v, couplingParams(), 5)

	if len(pairs) == 0 {
		t.Fatal("no pairs returned")
	}
	found := false
	for _, p := range pairs {
		if (p.A == "internal/api/serialize.go" && p.B == "mobile/schema.json") ||
			(p.B == "internal/api/serialize.go" && p.A == "mobile/schema.json") {
			found = true
			if p.Together < 3 {
				t.Errorf("pair support = %d, too low to be reported", p.Together)
			}
		}
	}
	if !found {
		t.Errorf("the planted pair is missing from %+v", pairs)
	}
}

// Lift is a ratio, and the mean of thirty ratios built on small numerators is
// decided by whichever repository had four fixes and got lucky. Pooling has to
// add the counts first.
func TestPooledCouplingWeightsByEvidenceNotByRepository(t *testing.T) {
	rs := []RepoCoupling{
		// Tiny and extreme: 3 of 3 fixes on coupled files, lift 3.0.
		{Repo: "tiny", CouplingResult: CouplingResult{
			NewcomerFixes: 3, FixesOnCoupled: 3, BaseRate: 0.33, FixRate: 1.0, Lift: 3.0,
		}},
		// Large and flat: 100 fixes, half on coupled files, lift 1.0.
		{Repo: "large", CouplingResult: CouplingResult{
			NewcomerFixes: 100, FixesOnCoupled: 50, BaseRate: 0.5, FixRate: 0.5, Lift: 1.0,
		}},
	}
	got := PooledCoupling(rs)

	if got.NewcomerFixes != 103 || got.FixesOnCoupled != 53 {
		t.Fatalf("counts not pooled: %d fixes, %d on coupled", got.NewcomerFixes, got.FixesOnCoupled)
	}
	// A mean of the two lifts would be 2.0. Pooled has to land near the large
	// repository, which supplied 97% of the evidence.
	if got.Lift > 1.3 {
		t.Errorf("lift = %.2f — the three-fix repository dominated a hundred-fix one", got.Lift)
	}
}

func TestPooledCouplingHandlesAnEmptyRun(t *testing.T) {
	got := PooledCoupling(nil)
	if got.Lift != 0 {
		t.Errorf("lift = %v on no data, want 0", got.Lift)
	}
	if got.Verdict == "" {
		t.Error("an empty run should still say why it has no answer")
	}
}

// The guards are the difference between a result and arithmetic on noise, and
// pooling must not launder a thin corpus into a confident number.
func TestPooledCouplingStillReportsThinEvidence(t *testing.T) {
	rs := []RepoCoupling{
		{Repo: "a", CouplingResult: CouplingResult{NewcomerFixes: 4, FixesOnCoupled: 4, BaseRate: 0.5, Lift: 2}},
		{Repo: "b", CouplingResult: CouplingResult{NewcomerFixes: 3, FixesOnCoupled: 0, BaseRate: 0.5, Lift: 0}},
	}
	got := PooledCoupling(rs)
	if got.NewcomerFixes >= MinFixesForSignal {
		t.Fatalf("test fixture no longer exercises the guard: %d fixes", got.NewcomerFixes)
	}
	if !strings.Contains(got.Verdict, "sample-size") {
		t.Errorf("a thin pooled result did not say so: %q", got.Verdict)
	}
}
