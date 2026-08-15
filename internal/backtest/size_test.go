package backtest

import (
	"fmt"
	"strings"
	"testing"
)

func sizeMap(n int) map[string]int {
	out := make(map[string]int, n)
	for i := 0; i < n; i++ {
		out[fmt.Sprintf("f%02d.go", i)] = (i + 1) * 10
	}
	return out
}

func TestStrataSplitByRankNotByValue(t *testing.T) {
	// Heavy-tailed on purpose: equal-width value bands would put every file
	// but the last into the bottom bucket.
	sizes := map[string]int{
		"a.go": 1, "b.go": 2, "c.go": 3, "d.go": 4,
		"e.go": 5, "f.go": 6, "g.go": 7, "h.go": 5000,
	}
	got := Strata(sizes)

	counts := make([]int, NumStrata)
	for _, q := range got {
		counts[q]++
	}
	for q, n := range counts {
		if n != 2 {
			t.Errorf("band %d holds %d of 8 files, want 2 — bands are not equal by count: %v", q, n, counts)
		}
	}
	if got["h.go"] != NumStrata-1 {
		t.Errorf("the largest file landed in band %d, want %d", got["h.go"], NumStrata-1)
	}
	if got["a.go"] != 0 {
		t.Errorf("the smallest file landed in band %d, want 0", got["a.go"])
	}
}

func TestStrataIsDeterministicWhenSizesTie(t *testing.T) {
	sizes := map[string]int{"a.go": 5, "b.go": 5, "c.go": 5, "d.go": 5}
	first := Strata(sizes)
	for i := 0; i < 50; i++ {
		for path, q := range Strata(sizes) {
			if first[path] != q {
				t.Fatalf("%s moved between bands across runs: %d then %d", path, first[path], q)
			}
		}
	}
}

// The halving rule is what keeps a stratum measuring choice rather than
// availability. Any ten of twelve files cover the same ground, so precision@10
// inside a twelve-file band is nearly the same number for every strategy.
func TestStratumKLeavesHeadroomForOrderingToMatter(t *testing.T) {
	for _, tc := range []struct{ candidates, k, want int }{
		{40, 10, 10}, // plenty: use the run's k
		{12, 10, 6},  // half, so six of twelve can be wrong
		{7, 10, 3},   // half, rounded down
		{5, 10, 0},   // too small to say anything
		{0, 10, 0},
		{40, 4, 4}, // never exceeds the run's k
	} {
		if got := stratumK(tc.candidates, tc.k); got != tc.want {
			t.Errorf("stratumK(%d, %d) = %d, want %d", tc.candidates, tc.k, got, tc.want)
		}
	}
}

// A band nobody touched is not a miss. Scoring it zero would drag every
// strategy's average toward zero in proportion to how many bands were empty,
// which is a fact about the contributor, not about the ranking.
func TestStrataSkipsBandsWithNoGroundTruth(t *testing.T) {
	sizes := sizeMap(40)
	// Everything touched is in the largest band.
	actual := []string{"f36.go", "f37.go", "f38.go"}
	predicted := make([]string, 0, 40)
	for i := 39; i >= 0; i-- {
		predicted = append(predicted, fmt.Sprintf("f%02d.go", i))
	}

	got := scoreStrata(predicted, actual, sizes, 10)
	if len(got) != 1 {
		t.Fatalf("scored %d bands, want 1 — only the largest band has support: %+v", len(got), got)
	}
	if got[0].Stratum != NumStrata-1 {
		t.Errorf("scored band %d, want the largest", got[0].Stratum)
	}
	if got[0].Support != 3 {
		t.Errorf("support = %d, want 3", got[0].Support)
	}
}

// Precision inside a band divides by the band's cutoff, not by how many files
// the strategy happened to name there. Otherwise a strategy naming two files
// in a band and hitting both scores 100%.
func TestStratumPrecisionDividesByTheCutoff(t *testing.T) {
	// Eighty files put twenty in each band, so the cutoff is the run's k
	// rather than the halving rule, and the arithmetic is easy to read.
	sizes := sizeMap(80)
	actual := []string{"f60.go", "f61.go"}
	// Names only two files in the largest band, both correct.
	predicted := []string{"f60.go", "f61.go"}

	got := scoreStrata(predicted, actual, sizes, 10)
	if len(got) != 1 {
		t.Fatalf("want one scored band, got %+v", got)
	}
	if got[0].K != 10 {
		t.Fatalf("cutoff = %d, want 10", got[0].K)
	}
	if got[0].Precision != 0.2 {
		t.Errorf("precision = %v, want 0.2 (2 hits over a cutoff of 10)", got[0].Precision)
	}
}

// Reproducibility is the property that lets a Gate A number be quoted. The
// draw is seeded from the file set, and map iteration order must not leak in.
func TestSizeProportionalDrawRepeats(t *testing.T) {
	sizes := sizeMap(30)
	first := weightedShuffle(sizes)
	for i := 0; i < 100; i++ {
		got := weightedShuffle(sizes)
		if len(got) != len(first) {
			t.Fatalf("length moved: %d then %d", len(first), len(got))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("draw %d differed at position %d: %s then %s", i, j, first[j], got[j])
			}
		}
	}
}

// It has to actually be size-weighted, or it is not a size control.
func TestSizeProportionalFavoursLargeFiles(t *testing.T) {
	sizes := sizeMap(40) // f39 is 400 lines, f00 is 10
	got := weightedShuffle(sizes)
	if len(got) != 40 {
		t.Fatalf("drew %d of 40 files", len(got))
	}

	var topHalf int
	for _, f := range got[:10] {
		if sizes[f] > 200 {
			topHalf++
		}
	}
	if topHalf < 5 {
		t.Errorf("only %d of the first 10 draws came from the larger half; the draw is not size-weighted", topHalf)
	}

	// Every file must appear exactly once — it is a shuffle, not a sample.
	seen := map[string]bool{}
	for _, f := range got {
		if seen[f] {
			t.Fatalf("%s drawn twice", f)
		}
		seen[f] = true
	}
}

func stratReport(lectio, largest float64, bands [][2]float64) Report {
	rep := Report{
		Cases: 20,
		Aggregates: []Aggregate{
			{Strategy: "lectio", PrecisionA: lectio},
			{Strategy: "largest files", PrecisionA: largest},
		},
	}
	for q, b := range bands {
		rep.Strata = append(rep.Strata,
			StratumAggregate{Strategy: "lectio", Stratum: q, Label: StratumLabels[q], Cases: 20, Precision: b[0]},
			StratumAggregate{Strategy: "largest files", Stratum: q, Label: StratumLabels[q], Cases: 20, Precision: b[1]},
		)
	}
	return rep
}

// The pattern the stratified table exists to catch: behind overall, ahead
// everywhere. It means the ground truth is size-weighted, so overall precision
// scores the composition of a top ten rather than the choices in it.
func TestReadStrataNamesSimpsonsParadox(t *testing.T) {
	got := ReadStrata(stratReport(0.43, 0.48, [][2]float64{
		{0.20, 0.10}, {0.30, 0.25}, {0.40, 0.38}, {0.50, 0.45},
	}))

	if got.LectioWins != 4 {
		t.Fatalf("LectioWins = %d, want 4", got.LectioWins)
	}
	if !strings.Contains(got.Note, "rewarding size") {
		t.Errorf("the note did not name the paradox: %q", got.Note)
	}
}

// The opposite pattern has to be reported just as plainly, or the check is
// only capable of exonerating the ranking.
func TestReadStrataNamesAGenuineLoss(t *testing.T) {
	got := ReadStrata(stratReport(0.43, 0.48, [][2]float64{
		{0.10, 0.20}, {0.25, 0.30}, {0.38, 0.40}, {0.45, 0.50},
	}))

	if got.LectioWins != 0 {
		t.Fatalf("LectioWins = %d, want 0", got.LectioWins)
	}
	if !strings.Contains(got.Note, "not an artifact") {
		t.Errorf("a real loss was not reported as one: %q", got.Note)
	}
}

func TestReadStrataRefusesToSettleAMixedResult(t *testing.T) {
	got := ReadStrata(stratReport(0.43, 0.48, [][2]float64{
		{0.20, 0.10}, {0.25, 0.30}, {0.40, 0.38}, {0.45, 0.50},
	}))

	if got.LectioWins != 2 {
		t.Fatalf("LectioWins = %d, want 2", got.LectioWins)
	}
	if !strings.Contains(got.Note, "not") || !strings.Contains(got.Note, "settle") {
		t.Errorf("a mixed result should say so: %q", got.Note)
	}
}

// A band missing from one strategy cannot be compared, and pairing it against
// a zero would invent a win.
func TestReadStrataSkipsUnpairedBands(t *testing.T) {
	rep := stratReport(0.43, 0.48, [][2]float64{{0.20, 0.10}, {0.30, 0.25}})
	rep.Strata = append(rep.Strata, StratumAggregate{
		Strategy: "lectio", Stratum: 3, Label: StratumLabels[3], Cases: 20, Precision: 0.9,
	})

	got := ReadStrata(rep)
	if len(got.Bands) != 2 {
		t.Errorf("compared %d bands, want 2 — the unpaired band should be dropped: %+v", len(got.Bands), got.Bands)
	}
}

// Controls explain a result; they must never decide it. The spec names four
// baselines and adding a fifth to the pass condition would be moving the
// goalposts after seeing the score.
func TestControlsDoNotDecideTheGate(t *testing.T) {
	names := map[string]bool{}
	for _, b := range Baselines() {
		names[b.Name()] = true
	}
	for _, c := range Controls() {
		if names[c.Name()] {
			t.Errorf("%s is both a baseline and a control", c.Name())
		}
	}

	rep := Report{
		Cases: 10,
		Aggregates: []Aggregate{
			{Strategy: "lectio", PrecisionA: 0.5},
			{Strategy: "largest files", PrecisionA: 0.4},
			{Strategy: "most churned, 12mo", PrecisionA: 0.4},
			{Strategy: "most recently modified", PrecisionA: 0.4},
			{Strategy: "most distinct authors", PrecisionA: 0.4},
			{Strategy: SizeProportional{}.Name(), PrecisionA: 0.9},
		},
	}
	if v := decide(rep); !v.Passed {
		t.Errorf("a control outscoring lectio failed the gate: %+v", v)
	}
}

// The symbol-granularity run passed the gate while a control scored more than
// double. A report that prints that in green is technically true and actively
// misleading, which is the one thing a go/no-go must never be.
func TestVerdictFlagsAPassAControlUndercuts(t *testing.T) {
	rep := Report{
		Cases: 75,
		Aggregates: []Aggregate{
			{Strategy: "lectio", PrecisionA: 0.105},
			{Strategy: "largest files", PrecisionA: 0.069},
			{Strategy: "most churned, 12mo", PrecisionA: 0.083},
			{Strategy: "most recently modified", PrecisionA: 0.047},
			{Strategy: "most distinct authors", PrecisionA: 0.065},
			{Strategy: "largest symbols", PrecisionA: 0.228},
		},
	}
	v := decide(rep)

	if !v.Passed {
		t.Fatal("lectio beat all four baselines and should pass the gate as written")
	}
	if !v.Hollow() {
		t.Fatal("a control scoring more than double did not register")
	}
	if len(v.OutscoredByControl) != 1 || v.OutscoredByControl[0] != "largest symbols" {
		t.Errorf("OutscoredByControl = %v", v.OutscoredByControl)
	}
	if !strings.Contains(v.Note, "control") {
		t.Errorf("the note does not mention the control: %q", v.Note)
	}
}

// A clean pass must stay a clean pass — the warning is for a real undercut,
// not a permanent asterisk on every positive result.
func TestVerdictDoesNotCryWolfOnACleanPass(t *testing.T) {
	rep := Report{
		Cases: 75,
		Aggregates: []Aggregate{
			{Strategy: "lectio", PrecisionA: 0.50},
			{Strategy: "largest files", PrecisionA: 0.40},
			{Strategy: "most churned, 12mo", PrecisionA: 0.40},
			{Strategy: "most recently modified", PrecisionA: 0.40},
			{Strategy: "most distinct authors", PrecisionA: 0.40},
			{Strategy: "largest symbols", PrecisionA: 0.30},
		},
	}
	if v := decide(rep); !v.Passed || v.Hollow() {
		t.Errorf("a clean pass was flagged: %+v", v)
	}
}

// At symbol granularity the strategy to beat is largest-symbols, not
// largest-files. Comparing against the wrong one produced a reassuring
// "lectio leads overall and in 3 of 4 size bands" while the control that
// actually outscored it by two to one went unmentioned.
func TestReadStrataComparesAgainstTheRightSizeStrategy(t *testing.T) {
	rep := Report{
		Cases:  75,
		Target: TargetSymbols,
		Aggregates: []Aggregate{
			{Strategy: "lectio", PrecisionA: 0.105},
			{Strategy: "largest files", PrecisionA: 0.069},
			{Strategy: "largest symbols", PrecisionA: 0.228},
		},
	}
	for q, p := range [][3]float64{
		{0.110, 0.082, 0.103}, // lectio, largest files, largest symbols
		{0.075, 0.075, 0.141},
		{0.083, 0.076, 0.119},
		{0.137, 0.146, 0.237},
	} {
		rep.Strata = append(rep.Strata,
			StratumAggregate{Strategy: "lectio", Stratum: q, Cases: 75, Precision: p[0], Spread: 3},
			StratumAggregate{Strategy: "largest files", Stratum: q, Cases: 75, Precision: p[1], Spread: 3},
			StratumAggregate{Strategy: "largest symbols", Stratum: q, Cases: 75, Precision: p[2], Spread: 3},
		)
	}

	got := ReadStrata(rep)
	if got.Incumbent != "largest symbols" {
		t.Fatalf("Incumbent = %q, want largest symbols", got.Incumbent)
	}
	// Lectio wins only the smallest band against largest symbols.
	if got.LectioWins != 1 {
		t.Errorf("LectioWins = %d against largest symbols, want 1", got.LectioWins)
	}
	if !strings.Contains(got.Note, "largest symbols") {
		t.Errorf("the note names the wrong strategy: %q", got.Note)
	}
}

// File targets must keep comparing against largest-files, which is the
// strategy that beat lectio there.
func TestReadStrataKeepsLargestFilesForFileTargets(t *testing.T) {
	got := ReadStrata(stratReport(0.43, 0.48, [][2]float64{{0.20, 0.10}, {0.30, 0.25}}))
	if got.Incumbent != "largest files" {
		t.Errorf("Incumbent = %q, want largest files", got.Incumbent)
	}
}
