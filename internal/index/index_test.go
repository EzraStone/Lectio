package index

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/EzraStone/Lectio/internal/adapter"
	golangadapter "github.com/EzraStone/Lectio/internal/adapter/golang"
	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/graph"
	"github.com/EzraStone/Lectio/internal/store"
)

// sampleRepo copies the Go adapter's fixture module into a temp dir and makes
// it a git repository, so the full pipeline — symbols, call graph, history —
// runs against something real.
func sampleRepo(t *testing.T) string {
	t.Helper()
	for _, bin := range []string{"go", "git"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}

	src, err := filepath.Abs(filepath.Join("..", "adapter", "golang", "testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if out, err := exec.Command("cp", "-r", src+"/.", dst).CombinedOutput(); err != nil {
		t.Fatalf("copy fixture: %v\n%s", err, out)
	}

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dst
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE=2026-05-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-05-01T00:00:00Z",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "Fixture Author")
	git("config", "user.email", "fixture@example.com")
	git("add", ".")
	git("commit", "-q", "-m", "initial import")

	// A second commit so churn and fix density have something to see.
	if err := os.WriteFile(filepath.Join(dst, "billing", "billing.go"),
		append(readFile(t, filepath.Join(dst, "billing", "billing.go")), []byte("\n// touched\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-qam", "fix: adjust billing rounding")

	return dst
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func buildIndex(t *testing.T) (*store.Store, *View) {
	t.Helper()
	root := sampleRepo(t)
	ctx := context.Background()

	s, err := store.OpenRepo(ctx, root)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	res, err := Build(ctx, s, golangadapter.New(), root, adapter.DefaultOptions())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Stats.Symbols == 0 {
		t.Fatal("index has no symbols")
	}
	if res.Head == "" {
		t.Error("head commit was not recorded")
	}
	if res.Duration <= 0 {
		t.Error("duration not measured")
	}

	v, err := Load(ctx, s)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s, v
}

func TestBuildAndLoadRoundTrip(t *testing.T) {
	_, v := buildIndex(t)

	if len(v.Symbols) == 0 {
		t.Fatal("view has no symbols")
	}
	if _, ok := v.Symbols["example.com/sample.parseInterval"]; !ok {
		t.Error("parseInterval missing from the view")
	}
	if v.Head == "" {
		t.Error("head not carried into the view")
	}
	if len(v.Commits) != 2 {
		t.Errorf("commits = %d, want 2", len(v.Commits))
	}
	if len(v.Imports) == 0 {
		t.Error("no import edges")
	}
	if len(v.Authorship) == 0 {
		t.Error("no authorship records")
	}
}

// The two-graph split is the trust property made structural.
func TestViewSeparatesStaticFromDynamicEdges(t *testing.T) {
	_, v := buildIndex(t)

	if v.Calls.Edges() <= v.StaticCalls.Edges() {
		t.Errorf("expected CHA to add edges: calls=%d static=%d",
			v.Calls.Edges(), v.StaticCalls.Edges())
	}

	// The interface dispatch must be in Calls and absent from StaticCalls.
	next, ok := v.StaticCalls.Index("example.com/sample/scheduler.(*Scheduler).Next")
	if !ok {
		t.Fatal("Next missing from the static graph")
	}
	for _, i := range v.StaticCalls.Out(next) {
		if id := v.StaticCalls.ID(i); id == "example.com/sample/scheduler.(realClock).Now" {
			t.Error("a CHA-resolved interface call leaked into the static graph")
		}
	}
}

// Symbols with no edges at all must still be present; "nothing depends on
// this" is a fact about a symbol, not grounds for erasing it.
func TestIsolatedSymbolsSurvive(t *testing.T) {
	_, v := buildIndex(t)

	if !v.Calls.Has("example.com/sample.(Interval).Describe") {
		t.Error("a symbol with no call edges vanished from the graph")
	}
	if v.Calls.N() != len(v.Symbols) {
		t.Errorf("graph has %d nodes for %d symbols", v.Calls.N(), len(v.Symbols))
	}
}

func TestBlastRadiusFromTheView(t *testing.T) {
	_, v := buildIndex(t)

	got := v.BlastRadius("example.com/sample.parseInterval", 0)
	for _, want := range []core.SymbolID{
		"example.com/sample.Parse",
		"example.com/sample/billing.Cycle",
		"example.com/sample/retry.Requeue",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("blast radius missing %s", want)
		}
	}
	if _, ok := got["example.com/sample.parseInterval"]; ok {
		t.Error("the seed appeared in its own blast radius")
	}
}

func TestReadableExcludesTests(t *testing.T) {
	_, v := buildIndex(t)

	for _, s := range v.Readable() {
		if s.IsTest() {
			t.Errorf("test symbol %s reached a reading path", s.ID)
		}
	}
	if _, ok := v.Symbols["example.com/sample.TestParse"]; !ok {
		t.Error("test symbols should still be indexed, just not readable")
	}
}

func TestReindexPreservesProbeHistory(t *testing.T) {
	s, _ := buildIndex(t)
	ctx := context.Background()
	root, _ := s.Meta(ctx, "root")

	if err := s.RecordProbe(ctx, store.ProbeRecord{
		Symbol: "example.com/sample.parseInterval", Kind: "blast",
		AskedAt: time.Now(), Outcome: store.OutcomeCorrect, Score: 1,
	}); err != nil {
		t.Fatalf("RecordProbe: %v", err)
	}

	if _, err := Build(ctx, s, golangadapter.New(), root, adapter.DefaultOptions()); err != nil {
		t.Fatalf("re-Build: %v", err)
	}

	e, err := s.Engagement(ctx, "example.com/sample.parseInterval")
	if err != nil {
		t.Fatalf("Engagement: %v", err)
	}
	if e.ProbesCorrect != 1 {
		t.Errorf("probe history lost across re-index: %+v", e)
	}
}

func TestBuildWithoutGitStillIndexes(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	// A plain directory: no git, so history-derived signals go quiet.
	src, _ := filepath.Abs(filepath.Join("..", "adapter", "golang", "testdata", "sample"))
	dst := t.TempDir()
	if out, err := exec.Command("cp", "-r", src+"/.", dst).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v\n%s", err, out)
	}

	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	res, err := Build(ctx, s, golangadapter.New(), dst, adapter.DefaultOptions())
	if err != nil {
		t.Fatalf("indexing a non-git directory should still work: %v", err)
	}
	if res.Stats.Symbols == 0 {
		t.Error("no symbols indexed")
	}
	if len(res.Warnings) == 0 {
		t.Error("missing history should produce a warning, not silence")
	}
}

// A test helper calling a symbol is a real call-graph dependent, but nobody
// answering "what breaks" enumerates test functions by name. Before this was
// filtered, the probe for cli.usage expected "cli.run" and "cli.runWithInput"
// — two test helpers — alongside cli.Main, and a correct answer graded 0%.
func TestBlastRadiusExcludesTestFunctions(t *testing.T) {
	_, v := buildIndex(t)

	got := v.BlastRadius("example.com/sample.Parse", 0)
	if len(got) == 0 {
		t.Fatal("no dependents found at all")
	}
	for id := range got {
		if sym, ok := v.Symbols[id]; ok && sym.IsTest() {
			t.Errorf("test symbol %s is in the graded answer set", id)
		}
	}

	// The production dependents must survive the filter.
	if _, ok := got["example.com/sample/billing.Cycle"]; !ok {
		t.Errorf("a production dependent was lost: %v", got)
	}

	// TestParse calls Parse, so it is a genuine dependent in the raw graph —
	// this asserts the filter is doing work rather than the edge being absent.
	deps := graph.Dependents(v.StaticCalls, "example.com/sample.Parse", 0)
	if _, ok := deps["example.com/sample.TestParse"]; !ok {
		t.Skip("fixture no longer has a test calling Parse; the filter is untested here")
	}
}

func TestReadableExcludesGeneratedCode(t *testing.T) {
	_, v := buildIndex(t)

	var sawGenerated bool
	for id, sym := range v.Symbols {
		if sym.Generated {
			sawGenerated = true
			break
		}
		_ = id
	}
	if !sawGenerated {
		t.Fatal("fixture has no generated symbols; the exclusion is untested")
	}

	for _, s := range v.Readable() {
		if s.Generated {
			t.Errorf("generated symbol %s reached a reading path", s.ID)
		}
	}
	// It must still be indexed: things really call it, and blast radius needs
	// to know that.
	if _, ok := v.Symbols["example.com/sample/generated.(*Request).GetSpec"]; !ok {
		t.Error("generated symbols should be indexed, just not recommended")
	}
}
