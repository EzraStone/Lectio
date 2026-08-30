package cli

import (
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/store"
)

func healthyIndex() index.Result {
	return index.Result{
		PackagesLoaded: 26,
		Stats: store.Stats{
			Symbols: 1616, Files: 172, CallEdges: 3953, ImportEdges: 40, Commits: 286,
		},
	}
}

// Zero import edges is the failure this warning exists for, and it is the one
// that looks most like success: the index builds, the symbol count is right,
// and every co-change in the repository quietly becomes "hidden".
func TestNoImportEdgesIsWarnedAbout(t *testing.T) {
	r := healthyIndex()
	r.Stats.ImportEdges = 0

	got := strings.Join(indexWarnings(r), " ")
	if got == "" {
		t.Fatal("an index with no import edges produced no warning")
	}
	for _, want := range []string{
		"No import edges were recorded",
		"every co-change in this repository will look hidden",
		"did not type-check",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the warning is missing %q:\n%s", want, got)
		}
	}
}

// An index with no symbols at all has a different problem, and warning about
// its import edges would bury the real one.
func TestNoImportEdgesIsNotWarnedAboutOnAnEmptyIndex(t *testing.T) {
	r := index.Result{}
	for _, w := range indexWarnings(r) {
		if strings.Contains(w, "import edges") {
			t.Errorf("an empty index was warned about its import edges:\n%s", w)
		}
	}
}

// The asymmetry is the point: a degraded type-check takes the call graph and
// leaves the history signals, so the ranking looks worse than it is.
func TestHeavyPackageFailureIsWarnedAbout(t *testing.T) {
	r := healthyIndex()
	r.PackagesLoaded, r.PackagesFailed = 20, 9

	got := strings.Join(indexWarnings(r), " ")
	for _, want := range []string{
		"9 of 20 packages failed to type-check",
		"centrality and hidden coupling degrade while the history signals do not",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the warning is missing %q:\n%s", want, got)
		}
	}
}

// A couple of failed packages in a large repository is normal and does not
// need a warning; warning on every index would make the row furniture.
func TestALittlePackageFailureIsNotWarnedAbout(t *testing.T) {
	r := healthyIndex()
	r.PackagesLoaded, r.PackagesFailed = 26, 2

	for _, w := range indexWarnings(r) {
		if strings.Contains(w, "type-check") {
			t.Errorf("2 of 26 failed packages produced a warning:\n%s", w)
		}
	}
}

func TestNoCommitsIsWarnedAbout(t *testing.T) {
	r := healthyIndex()
	r.Stats.Commits = 0

	got := strings.Join(indexWarnings(r), " ")
	if !strings.Contains(got, "Four of the seven signals are history-derived") {
		t.Errorf("an index with no history produced no warning:\n%s", got)
	}
}

// A healthy index is silent. A warning that fires on every run is one nobody
// reads on the run that matters.
func TestAHealthyIndexWarnsAboutNothing(t *testing.T) {
	if got := indexWarnings(healthyIndex()); len(got) != 0 {
		t.Errorf("a healthy index produced %d warnings: %v", len(got), got)
	}
}

// Several problems at once produce several warnings, not the first one.
func TestWarningsAccumulate(t *testing.T) {
	r := healthyIndex()
	r.Stats.ImportEdges, r.Stats.Commits = 0, 0
	r.PackagesLoaded, r.PackagesFailed = 20, 15

	if got := indexWarnings(r); len(got) != 3 {
		t.Errorf("got %d warnings for three problems: %v", len(got), got)
	}
}
