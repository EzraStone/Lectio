package rank

import (
	"strings"
	"testing"
	"time"
)

// The churn rationale said "this year" whatever --months was set to, so
// `lectio path --months 3` described a three-month count as a year's. A tool
// whose premise is that every claim is auditable does not get to do that.
func TestWindowPhraseFollowsTheWindow(t *testing.T) {
	month := 30 * 24 * time.Hour
	for _, tc := range []struct {
		months int
		want   string
	}{
		{1, "in the last month"},
		{2, "in the last 2 months"},
		{3, "in the last 3 months"},
		{6, "in the last 6 months"},
		{12, "this year"},
		{24, "in the last 24 months"},
	} {
		if got := windowPhrase(time.Duration(tc.months) * month); got != tc.want {
			t.Errorf("windowPhrase(%d months) = %q, want %q", tc.months, got, tc.want)
		}
	}
}

// A window nobody can describe should not be described confidently.
func TestWindowPhraseOnANonWindow(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Hour, -365 * 24 * time.Hour} {
		if got := windowPhrase(d); got != "in the window" {
			t.Errorf("windowPhrase(%v) = %q, want the neutral phrase", d, got)
		}
	}
	// But Gather never passes one: a non-positive window falls back to a year,
	// and the phrase is derived from the same value so the two cannot drift.
	if got := windowPhrase(effectiveWindow(0)); got != "this year" {
		t.Errorf("an unset window describes itself as %q, want the year it counts over", got)
	}
}

// Facts carries the phrase so the rationale cannot drift from the window the
// counts were taken over.
func TestFactsCarryTheirWindow(t *testing.T) {
	now := time.Now().UTC()
	p := DefaultParams()
	p.Now = now
	p.ChurnWindow = 90 * 24 * time.Hour

	f := Gather(activeRepo(now), p)
	if f.Window != "in the last 3 months" {
		t.Errorf("Facts.Window = %q, want the three-month phrase", f.Window)
	}
}

// And the default window still reads the way it always did, so nothing about
// an ordinary run changes.
func TestTheDefaultWindowStillReadsAsThisYear(t *testing.T) {
	now := time.Now().UTC()
	p := DefaultParams()
	p.Now = now

	if got := Gather(activeRepo(now), p).Window; got != "this year" {
		t.Errorf("the default window reads %q, want \"this year\"", got)
	}
}

// The rationale uses it rather than the hardcoded phrase.
func TestChurnRationaleUsesTheWindow(t *testing.T) {
	now := time.Now().UTC()
	p := DefaultParams()
	p.Now = now
	p.ChurnWindow = 60 * 24 * time.Hour

	v := activeRepo(now)
	f := Gather(v, p)
	item := Item{
		Symbol:        v.Symbols["p.A"],
		Contributions: map[Signal]float64{SignalChurn: 1},
	}
	got := f.Explain(item, "")
	if got == "" {
		t.Fatal("the churn rationale is empty on a file with commits")
	}
	if want := "in the last 2 months"; !strings.Contains(got, want) {
		t.Errorf("rationale is %q, want it to end with %q", got, want)
	}
	if strings.Contains(got, "this year") {
		t.Errorf("rationale is %q, which still claims a year", got)
	}
}
