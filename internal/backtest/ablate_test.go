package backtest

import (
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/rank"
)

func TestAblationsCoverEverySignalThatFires(t *testing.T) {
	got := Ablations()

	if got[0].Name != DefaultVariant {
		t.Errorf("first variant = %q, want the default weighting", got[0].Name)
	}

	names := map[string]rank.Weights{}
	for _, v := range got {
		names[v.Name] = v.Weights
	}

	for _, sig := range rank.AllSignals {
		name := "lectio −" + string(sig)
		w, ok := names[name]
		if sig == rank.SignalProximity {
			// A backtest names no task, so ablating proximity would add a
			// column identical to the baseline in every row.
			if ok {
				t.Errorf("proximity should not be ablated; it never fires in a backtest")
			}
			continue
		}
		if !ok {
			t.Errorf("no ablation for %s", sig)
			continue
		}
		if w[sig] != 0 {
			t.Errorf("%s: weight for %s = %v, want 0", name, sig, w[sig])
		}
		// Every other signal must keep its default weight, or the variant
		// measures two changes at once.
		for _, other := range rank.AllSignals {
			if other == sig {
				continue
			}
			if w[other] != rank.DefaultWeights()[other] {
				t.Errorf("%s changed %s as well: %v", name, other, w[other])
			}
		}
	}
}

func TestAblationVariantNamesFitTheReportColumn(t *testing.T) {
	for _, v := range Ablations() {
		if n := len([]rune(v.Name)); n > 26 {
			t.Errorf("variant %q is %d runes; the report column is 26", v.Name, n)
		}
	}
}

// "Lectio scored 0.31" is not actionable. "0.31, and 0.44 without churn" is.
func TestContributionsMeasureEachSignal(t *testing.T) {
	rep := Report{Aggregates: []Aggregate{
		{Strategy: "lectio", PrecisionA: 0.40},
		{Strategy: "lectio −centrality", PrecisionA: 0.25},
		{Strategy: "lectio −churn", PrecisionA: 0.46},
		{Strategy: "most churned, 12mo", PrecisionA: 0.30},
	}}

	got := Contributions(rep)
	if len(got) != 2 {
		t.Fatalf("contributions = %+v, want 2", got)
	}

	by := map[rank.Signal]Contribution{}
	for _, c := range got {
		by[c.Signal] = c
	}
	if d := by[rank.SignalCentrality].Delta; d < 0.14 || d > 0.16 {
		t.Errorf("centrality delta = %v, want ~0.15", d)
	}
	if d := by[rank.SignalChurn].Delta; d > -0.05 {
		t.Errorf("churn delta = %v, want negative — removing it improved the score", d)
	}
}

// The spec's named failure mode is the ranking becoming a churn proxy. A
// negative delta is direct evidence, and should not need spotting a minus sign
// in a table of near-identical numbers.
func TestHarmfulSurfacesSignalsThatHurt(t *testing.T) {
	cs := []Contribution{
		{Signal: rank.SignalCentrality, Delta: 0.15},
		{Signal: rank.SignalChurn, Delta: -0.06},
		{Signal: rank.SignalAIDensity, Delta: 0},
	}
	got := Harmful(cs)
	if len(got) != 1 || got[0].Signal != rank.SignalChurn {
		t.Errorf("Harmful = %+v, want just churn", got)
	}
}

// A plain run must be distinguishable from one where every signal measured
// zero effect.
func TestContributionsNilWithoutAblations(t *testing.T) {
	plain := Report{Aggregates: []Aggregate{
		{Strategy: "lectio", PrecisionA: 0.4},
		{Strategy: "largest files", PrecisionA: 0.1},
	}}
	if got := Contributions(plain); got != nil {
		t.Errorf("Contributions on a plain run = %+v, want nil", got)
	}

	noLectio := Report{Aggregates: []Aggregate{{Strategy: "largest files", PrecisionA: 0.1}}}
	if got := Contributions(noLectio); got != nil {
		t.Errorf("Contributions with no baseline variant = %+v, want nil", got)
	}
}

// Variants exist so an ablation costs one index per case rather than eight.
func TestLectioVariantsAreDistinctStrategies(t *testing.T) {
	plain := Lectio{}
	labelled := Lectio{Label: "lectio −churn"}

	if plain.Name() != "lectio" {
		t.Errorf("unlabelled name = %q", plain.Name())
	}
	if labelled.Name() != "lectio −churn" {
		t.Errorf("labelled name = %q", labelled.Name())
	}
	if !strings.HasPrefix(labelled.Name(), "lectio") {
		t.Error("variants should stay recognizable as lectio in a report")
	}
}

// The gate is "beat the four baselines". An ablation variant scoring higher
// says a signal is hurting — diagnostic, not a gate failure. Before this was
// fixed, an --ablate run produced a FAIL note listing "lectio
// −hidden_coupling" among the things lectio had failed to beat.
func TestVerdictIgnoresAblationVariants(t *testing.T) {
	rep := Report{
		Cases: 30, K: 10,
		Aggregates: []Aggregate{
			{Strategy: "lectio", PrecisionA: 0.50},
			{Strategy: "lectio −hidden_coupling", PrecisionA: 0.50},
			{Strategy: "lectio −fix_density", PrecisionA: 0.55},
			{Strategy: "largest files", PrecisionA: 0.10},
			{Strategy: "most churned, 12mo", PrecisionA: 0.20},
			{Strategy: "most recently modified", PrecisionA: 0.15},
			{Strategy: "most distinct authors", PrecisionA: 0.25},
		},
	}
	v := decide(rep)

	if !v.Passed {
		t.Errorf("beating all four baselines should pass: %+v", v)
	}
	for _, name := range append(append([]string{}, v.Lost...), v.Beaten...) {
		if strings.HasPrefix(name, "lectio") {
			t.Errorf("verdict counted an ablation variant %q as a baseline", name)
		}
	}
	if len(v.Beaten) != 4 {
		t.Errorf("beaten = %v, want exactly the four baselines", v.Beaten)
	}
}

// And an ablation run that genuinely loses to a baseline must still fail.
func TestVerdictStillFailsAgainstBaselinesDuringAblation(t *testing.T) {
	rep := Report{
		Cases: 30, K: 10,
		Aggregates: []Aggregate{
			{Strategy: "lectio", PrecisionA: 0.20},
			{Strategy: "lectio −churn", PrecisionA: 0.60},
			{Strategy: "largest files", PrecisionA: 0.10},
			{Strategy: "most churned, 12mo", PrecisionA: 0.40},
			{Strategy: "most recently modified", PrecisionA: 0.15},
			{Strategy: "most distinct authors", PrecisionA: 0.12},
		},
	}
	v := decide(rep)
	if v.Passed {
		t.Error("losing to churn must still fail the gate")
	}
	if len(v.Lost) != 1 || v.Lost[0] != "most churned, 12mo" {
		t.Errorf("lost = %v, want just the churn baseline", v.Lost)
	}
}

// A candidates run includes one variant named "lectio −orphaning", which looks
// exactly like an ablation row. Reading it as one produced a table headed
// "what each signal is worth" for a run that ablated nothing — six signals
// missing, one present, and no way for a reader to tell.
//
// The report says which experiment ran rather than leaving this to be guessed
// from names, which is what the guess got wrong.
func TestContributionsRejectsACandidatesRun(t *testing.T) {
	rep := Report{
		Cases:    20,
		Variants: VariantsCandidates,
		Aggregates: []Aggregate{
			{Strategy: DefaultVariant, PrecisionA: 0.40},
			{Strategy: "lectio −orphaning", PrecisionA: 0.42},
			{Strategy: "churn only", PrecisionA: 0.38},
		},
	}
	if got := Contributions(rep); got != nil {
		t.Errorf("a candidates run produced %d contributions: %+v", len(got), got)
	}
}

// A run labelled as an ablation still reads as one.
func TestContributionsAcceptsALabelledAblation(t *testing.T) {
	rep := Report{Cases: 20, Variants: VariantsAblation,
		Aggregates: []Aggregate{{Strategy: DefaultVariant, PrecisionA: 0.40}}}
	for _, v := range Ablations() {
		if v.Name == DefaultVariant {
			continue
		}
		rep.Aggregates = append(rep.Aggregates, Aggregate{Strategy: v.Name, PrecisionA: 0.39})
	}

	got := Contributions(rep)
	if len(got) == 0 {
		t.Fatal("a labelled ablation produced no contributions")
	}
	for _, c := range got {
		if c.Delta < 0.009 || c.Delta > 0.011 {
			t.Errorf("%s delta = %v, want ~0.01", c.Signal, c.Delta)
		}
	}
}

// An unlabelled report is the old shape, from before the kind was recorded.
// It has to keep working, or every stored report becomes unreadable.
func TestContributionsStillReadsAnUnlabelledReport(t *testing.T) {
	rep := Report{Aggregates: []Aggregate{
		{Strategy: DefaultVariant, PrecisionA: 0.40},
		{Strategy: "lectio −churn", PrecisionA: 0.46},
	}}
	if got := Contributions(rep); len(got) != 1 {
		t.Errorf("got %+v, want one contribution", got)
	}
}
