package backtest

import (
	"fmt"
	"math"
	"testing"
)

func closeTo(t *testing.T, got, want, tol float64, what string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, want %.4f ± %.4f", what, got, want, tol)
	}
}

// The quantile is the one number here that can be checked against a table
// rather than against itself.
func TestZForMatchesPublishedQuantiles(t *testing.T) {
	for _, tc := range []struct {
		level, z float64
	}{
		{0.90, 1.644854},
		{0.95, 1.959964},
		{0.99, 2.575829},
	} {
		closeTo(t, zFor(tc.level), tc.z, 1e-5, fmt.Sprintf("zFor(%.2f)", tc.level))
	}
}

func TestZForRejectsImpossibleCoverage(t *testing.T) {
	for _, level := range []float64{0, 1, -0.5, 1.5} {
		if got := zFor(level); got != 0 {
			t.Errorf("zFor(%v) = %v, want 0 for a coverage that is not a probability", level, got)
		}
	}
}

// Wilson on a coin: the interval should sit on chance and shrink as the square
// root of the sample.
func TestWilsonNarrowsWithSampleSize(t *testing.T) {
	var prev float64 = 1
	for _, n := range []int{100, 400, 1600, 6400} {
		iv := WilsonInterval(float64(n)/2, n, DefaultLevel)
		closeTo(t, iv.Point, 0.5, 1e-9, fmt.Sprintf("point at n=%d", n))
		if iv.Width() >= prev {
			t.Errorf("width at n=%d is %.4f, not narrower than %.4f", n, iv.Width(), prev)
		}
		if !(iv.Lo < 0.5 && iv.Hi > 0.5) {
			t.Errorf("n=%d: [%.4f, %.4f] does not contain chance", n, iv.Lo, iv.Hi)
		}
		prev = iv.Width()
	}
}

// The half-width on the corpus sizes this project actually runs at. These are
// the numbers the document's "±2 points" was standing in for, and they are not
// two points.
func TestWilsonHalfWidthAtCorpusScale(t *testing.T) {
	for _, tc := range []struct {
		pairs int
		want  float64
	}{
		{426, 0.0475},  // file-level, 5 cases per repo
		{577, 0.0408},  // file-level, 12 cases per repo
		{762, 0.0355},  // the holdout
		{2891, 0.0182}, // symbol-level
	} {
		iv := WilsonInterval(float64(tc.pairs)/2, tc.pairs, DefaultLevel)
		closeTo(t, iv.HalfWidth(), tc.want, 0.001, fmt.Sprintf("half-width at %d pairs", tc.pairs))
	}
}

func TestWilsonStaysOnTheScale(t *testing.T) {
	for _, tc := range []struct{ wins, pairs float64 }{{0, 10}, {10, 10}, {0, 1}, {1, 1}} {
		iv := WilsonInterval(tc.wins, int(tc.pairs), DefaultLevel)
		if iv.Lo < 0 || iv.Hi > 1 {
			t.Errorf("%v/%v gave [%.4f, %.4f], outside [0,1]", tc.wins, tc.pairs, iv.Lo, iv.Hi)
		}
	}
}

func TestWilsonOnNoPairsIsEmpty(t *testing.T) {
	iv := WilsonInterval(0, 0, DefaultLevel)
	if iv.Point != 0 || iv.Units != 0 {
		t.Errorf("got %+v from no pairs, want the zero interval", iv)
	}
}

// obsFrom builds one observation per accuracy, split across nRepos
// repositories in contiguous blocks.
//
// Contiguous rather than round-robin, because round-robin deals every
// repository the same spread of accuracies and so manufactures clusters that
// are identical to each other — the opposite of what clustering means. Cases
// from one repository arrive together and resemble each other; the helper has
// to reproduce that or the tests below measure nothing.
func obsFrom(nRepos int, accuracies ...float64) []Observation {
	block := (len(accuracies) + nRepos - 1) / nRepos
	if block < 1 {
		block = 1
	}
	out := make([]Observation, 0, len(accuracies))
	for i, a := range accuracies {
		out = append(out, Observation{
			Repo:     fmt.Sprintf("repo%02d", i/block),
			Accuracy: a,
			Pairs:    10,
		})
	}
	return out
}

// The point estimate the bootstrap reports must be the number in the table,
// not a resampled approximation of it.
func TestBootstrapPointIsTheReportedMean(t *testing.T) {
	obs := obsFrom(8, 0.4, 0.5, 0.6, 0.55, 0.45, 0.7, 0.3, 0.52)
	iv := BootstrapInterval(obs, DefaultLevel, 500, 1)
	closeTo(t, iv.Point, meanAccuracy(obs), 1e-12, "bootstrap point")
}

func TestBootstrapIsDeterministic(t *testing.T) {
	obs := obsFrom(10, 0.4, 0.5, 0.6, 0.55, 0.45, 0.7, 0.3, 0.52, 0.61, 0.49)
	a := BootstrapInterval(obs, DefaultLevel, 500, 7)
	for i := 0; i < 5; i++ {
		b := BootstrapInterval(obs, DefaultLevel, 500, 7)
		if a != b {
			t.Fatalf("same seed gave %+v then %+v", a, b)
		}
	}
}

func TestBootstrapBracketsThePoint(t *testing.T) {
	obs := obsFrom(10, 0.51, 0.62, 0.44, 0.58, 0.49, 0.55, 0.60, 0.41, 0.53, 0.57)
	iv := BootstrapInterval(obs, DefaultLevel, 1000, 3)
	if iv.Lo > iv.Point || iv.Hi < iv.Point {
		t.Errorf("[%.4f, %.4f] does not bracket the point %.4f", iv.Lo, iv.Hi, iv.Point)
	}
}

// A wider nominal coverage must give a wider interval, or the level is not
// doing anything.
func TestBootstrapWidensWithCoverage(t *testing.T) {
	obs := obsFrom(12, 0.51, 0.62, 0.44, 0.58, 0.49, 0.55, 0.60, 0.41, 0.53, 0.57, 0.47, 0.66)
	narrow := BootstrapInterval(obs, 0.80, 2000, 11)
	wide := BootstrapInterval(obs, 0.99, 2000, 11)
	if wide.Width() <= narrow.Width() {
		t.Errorf("99%% interval %.4f wide, 80%% interval %.4f wide", wide.Width(), narrow.Width())
	}
}

// The reason this bootstrap resamples repositories: five cases from one
// repository are not five independent observations, and an interval that
// treats them as such is too narrow.
//
// The same twenty-four accuracies, once as twenty-four repositories and once
// as eight. Both clear MinClusters, so both produce real intervals; the
// eight-repository run has a third of the independent units and must report a
// wider one.
func TestBootstrapIsWiderWhenCasesClusterInFewRepos(t *testing.T) {
	var acc []float64
	for i := 0; i < 24; i++ {
		acc = append(acc, 0.40+0.02*float64(i%3)+0.10*float64(i/6))
	}
	spread := BootstrapInterval(obsFrom(24, acc...), DefaultLevel, 2000, 5)
	clumped := BootstrapInterval(obsFrom(8, acc...), DefaultLevel, 2000, 5)

	if spread.Width() >= 1 || clumped.Width() >= 1 {
		t.Fatalf("one of these fell below MinClusters: widths %.3f and %.3f",
			spread.Width(), clumped.Width())
	}
	if clumped.Width() <= spread.Width() {
		t.Errorf("24 cases in 8 repos gave a %.4f-wide interval, not wider than %.4f across 24 repos",
			clumped.Width(), spread.Width())
	}
	if clumped.Units != 8 || spread.Units != 24 {
		t.Errorf("units are %d and %d, want 8 and 24 repositories", clumped.Units, spread.Units)
	}
}

// A percentile bootstrap over four repositories has 35 distinct resamples. The
// interval it produces is arbitrary, and it can come out narrow — which is how
// a size strategy came to report 60.3% as clear of chance on 171 pairs where
// it cannot know anything.
func TestBootstrapClaimsNothingBelowMinClusters(t *testing.T) {
	// oneCasePerRepo gives exactly one repository per accuracy, which is what
	// lets the loop step the cluster count by one.
	acc := []float64{0.62, 0.61, 0.60, 0.59, 0.63, 0.60, 0.61, 0.62}
	for n := 1; n < MinClusters; n++ {
		obs := oneCasePerRepo(acc[:n]...)
		iv := BootstrapInterval(obs, DefaultLevel, 2000, 4)
		if iv.Lo != 0 || iv.Hi != 1 {
			t.Errorf("%d repositories gave [%.3f, %.3f], want the whole scale", n, iv.Lo, iv.Hi)
		}
		if iv.ExcludesChance() {
			t.Errorf("%d repositories reported a result clear of chance", n)
		}
		if iv.Units != n {
			t.Errorf("%d repositories reported %d units", n, iv.Units)
		}
		closeTo(t, iv.Point, meanAccuracy(obs), 1e-12, "the point estimate is still reported")
	}
	if iv := BootstrapInterval(oneCasePerRepo(acc...), DefaultLevel, 2000, 4); iv.Width() >= 1 {
		t.Errorf("exactly MinClusters repositories still claimed nothing: [%.3f, %.3f]", iv.Lo, iv.Hi)
	}
}

func TestBootstrapOnNothingIsEmpty(t *testing.T) {
	if iv := BootstrapInterval(nil, DefaultLevel, 1000, 1); iv != (Interval{Level: DefaultLevel}) {
		t.Errorf("got %+v from no observations", iv)
	}
	if iv := BootstrapInterval(obsFrom(9, 0.5), DefaultLevel, 0, 1); iv.Units != 0 {
		t.Errorf("got %+v from zero iterations", iv)
	}
}

func TestExcludesChanceReadsBothSides(t *testing.T) {
	for _, tc := range []struct {
		lo, hi float64
		want   bool
	}{
		{0.51, 0.58, true},  // clear above
		{0.40, 0.49, true},  // clear below
		{0.48, 0.55, false}, // straddles
		{0.50, 0.60, false}, // touches chance exactly
	} {
		iv := Interval{Lo: tc.lo, Hi: tc.hi}
		if got := iv.ExcludesChance(); got != tc.want {
			t.Errorf("[%.2f, %.2f].ExcludesChance() = %v, want %v", tc.lo, tc.hi, got, tc.want)
		}
	}
}

func TestPercentileOfReadsTheEnds(t *testing.T) {
	xs := []float64{0.1, 0.2, 0.3, 0.4, 0.5}
	closeTo(t, percentileOf(xs, 0), 0.1, 1e-12, "0th")
	closeTo(t, percentileOf(xs, 1), 0.5, 1e-12, "100th")
	closeTo(t, percentileOf(xs, 0.5), 0.3, 1e-12, "50th")
	if got := percentileOf(nil, 0.5); got != 0 {
		t.Errorf("percentile of nothing = %v, want 0", got)
	}
}
