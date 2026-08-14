package backtest

import (
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/rank"
)

func item(file string, score float64) rank.Item {
	return rank.Item{Symbol: core.Symbol{File: file}, Score: score}
}

func TestParseCollapse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Collapse
		ok   bool
	}{
		{"max", CollapseMax, true},
		{"mean", CollapseMean, true},
		{"sum", CollapseSum, true},
		{"", DefaultCollapse, true},
		{"median", "", false},
	} {
		got, err := ParseCollapse(tc.in)
		if tc.ok != (err == nil) {
			t.Errorf("ParseCollapse(%q) err = %v", tc.in, err)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("ParseCollapse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The bug this whole change exists to remove: under max, a file wins by
// holding one strong symbol among many weak ones, so accumulating symbols is
// itself a way to rank. big.go has a single 0.9 and nine 0.1s; small.go has one
// 0.8. Max prefers big.go, mean prefers small.go, and mean is the honest answer
// — big.go's typical symbol is unremarkable.
func TestCollapseRuleDecidesTheSizeBias(t *testing.T) {
	items := []rank.Item{item("big.go", 0.9), item("small.go", 0.8)}
	for i := 0; i < 9; i++ {
		items = append(items, item("big.go", 0.1))
	}

	if got := rankFloat(fileScores(items, CollapseMax)); got[0] != "big.go" {
		t.Errorf("max should favour the file with the single best symbol, got %v", got)
	}
	if got := rankFloat(fileScores(items, CollapseMean)); got[0] != "small.go" {
		t.Errorf("mean should favour the file whose typical symbol is strong, got %v", got)
	}
	if got := rankFloat(fileScores(items, CollapseSum)); got[0] != "big.go" {
		t.Errorf("sum should favour the larger file, got %v", got)
	}
}

// Mean must not become a different bias. Zero-scored symbols mean "no signal
// had anything to say", and averaging those in would penalize a file for
// containing accessors — turning a size bonus into a size penalty rather than
// removing one.
func TestZeroScoredSymbolsDoNotDragTheMeanDown(t *testing.T) {
	withTrivia := []rank.Item{item("a.go", 0.6), item("a.go", 0), item("a.go", 0)}
	without := []rank.Item{item("a.go", 0.6)}

	got := fileScores(withTrivia, CollapseMean)["a.go"]
	want := fileScores(without, CollapseMean)["a.go"]
	if got != want {
		t.Errorf("zero-scored symbols changed the mean: %v vs %v", got, want)
	}
}

func TestRankFloatIsDeterministicOnTies(t *testing.T) {
	items := []rank.Item{item("b.go", 0.5), item("a.go", 0.5), item("c.go", 0.5)}
	first := rankFloat(fileScores(items, CollapseMean))
	for i := 0; i < 50; i++ {
		got := rankFloat(fileScores(items, CollapseMean))
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("tie order moved between runs: %v then %v", first, got)
			}
		}
	}
	if first[0] != "a.go" {
		t.Errorf("ties should break on path, got %v", first)
	}
}

// A file scoring zero everywhere is not a recommendation, and padding a
// ranking with them would put files the tool has no reason to name into the
// top ten.
func TestFilesWithNoSignalAreNotRanked(t *testing.T) {
	items := []rank.Item{item("a.go", 0.5), item("dead.go", 0)}
	got := rankFloat(fileScores(items, CollapseMean))
	if len(got) != 1 || got[0] != "a.go" {
		t.Errorf("unscored file reached the ranking: %v", got)
	}
}

// The zero value has to be the unbiased rule, because Lectio is constructed in
// several places and a forgotten field should not silently reinstate max.
func TestLectioZeroValueUsesTheDefaultCollapse(t *testing.T) {
	if DefaultCollapse != CollapseMean {
		t.Fatalf("DefaultCollapse = %q, want mean", DefaultCollapse)
	}
	var l Lectio
	if l.Collapse != "" {
		t.Fatalf("zero value should be empty, got %q", l.Collapse)
	}
	got, err := ParseCollapse(string(l.Collapse))
	if err != nil || got != CollapseMean {
		t.Errorf("zero value did not resolve to mean: %q %v", got, err)
	}
}
