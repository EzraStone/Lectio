package golang

import (
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/core"
)

const spanFixture = `package sample

import "fmt"

// Interval is a parsed duration.
type Interval struct {
	Unit string
}

const (
	Daily   = "daily"
	Weekly  = "weekly"
)

// Parse reads an interval.
func Parse(s string) (Interval, error) {
	return Interval{Unit: s}, nil
}

func (i Interval) Describe() string {
	return fmt.Sprint(i.Unit)
}

func (s *Scheduler) Next() int {
	return 0
}

type Scheduler struct{}

func (s Stack[T]) Push(v T) {}
`

func resolve(t *testing.T, ranges ...core.LineRange) []string {
	t.Helper()
	got, err := New().ResolveSpans([]byte(spanFixture), ranges)
	if err != nil {
		t.Fatalf("ResolveSpans: %v", err)
	}
	return got
}

func lineOf(t *testing.T, needle string) int {
	t.Helper()
	for i, l := range strings.Split(spanFixture, "\n") {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	t.Fatalf("fixture has no line containing %q", needle)
	return 0
}

func TestResolveSpansNamesTheEnclosingDeclaration(t *testing.T) {
	got := resolve(t, core.LineRange{Start: lineOf(t, "return Interval{Unit: s}"), Count: 1})
	if len(got) != 1 || got[0] != "Parse" {
		t.Errorf("got %v, want [Parse]", got)
	}
}

// Method names must match how symbolID renders them, or nothing will join
// against the symbol table.
func TestResolveSpansRendersMethodsLikeSymbolIDs(t *testing.T) {
	got := resolve(t, core.LineRange{Start: lineOf(t, "return fmt.Sprint(i.Unit)"), Count: 1})
	if len(got) != 1 || got[0] != "(Interval).Describe" {
		t.Errorf("value receiver: got %v, want [(Interval).Describe]", got)
	}

	got = resolve(t, core.LineRange{Start: lineOf(t, "func (s *Scheduler) Next"), Count: 1})
	if len(got) != 1 || got[0] != "(*Scheduler).Next" {
		t.Errorf("pointer receiver: got %v, want [(*Scheduler).Next]", got)
	}
}

// Generic receivers normalize to their origin, matching symbolID — otherwise
// Stack[int] and Stack[string] would be different symbols.
func TestResolveSpansDropsTypeParameters(t *testing.T) {
	got := resolve(t, core.LineRange{Start: lineOf(t, "Push(v T)"), Count: 1})
	if len(got) != 1 || got[0] != "(Stack).Push" {
		t.Errorf("got %v, want [(Stack).Push]", got)
	}
}

// Editing a function's doc comment is editing that function. Attributing it to
// whatever precedes it would be worse than not attributing it at all.
func TestResolveSpansCountsDocComments(t *testing.T) {
	got := resolve(t, core.LineRange{Start: lineOf(t, "// Parse reads an interval."), Count: 1})
	if len(got) != 1 || got[0] != "Parse" {
		t.Errorf("got %v, want [Parse] — a doc comment edit attributed elsewhere", got)
	}
}

// A grouped declaration is not one symbol. Losing this distinction would give
// symbol-level grading the coarseness it exists to escape.
func TestResolveSpansSeparatesGroupedConstants(t *testing.T) {
	got := resolve(t, core.LineRange{Start: lineOf(t, `Weekly  = "weekly"`), Count: 1})
	if len(got) != 1 || got[0] != "Weekly" {
		t.Errorf("got %v, want [Weekly] only — the group was attributed as a unit", got)
	}
}

func TestResolveSpansHandlesMultipleRanges(t *testing.T) {
	got := resolve(t,
		core.LineRange{Start: lineOf(t, "Unit string"), Count: 1},
		core.LineRange{Start: lineOf(t, "return 0"), Count: 1},
	)
	if len(got) != 2 {
		t.Fatalf("got %v, want two declarations", got)
	}
	// Source order, not range order.
	if got[0] != "Interval" || got[1] != "(*Scheduler).Next" {
		t.Errorf("got %v, want [Interval (*Scheduler).Next]", got)
	}
}

func TestResolveSpansDoesNotRepeatNames(t *testing.T) {
	start := lineOf(t, "func Parse(s string)")
	got := resolve(t,
		core.LineRange{Start: start, Count: 1},
		core.LineRange{Start: start + 1, Count: 1},
	)
	if len(got) != 1 {
		t.Errorf("got %v, want Parse once", got)
	}
}

func TestResolveSpansIgnoresImportsAndBlankLines(t *testing.T) {
	if got := resolve(t, core.LineRange{Start: lineOf(t, `import "fmt"`), Count: 1}); len(got) != 0 {
		t.Errorf("an import edit named %v", got)
	}
}

// The revisions that fail to parse are disproportionately the old ones a
// backtest most wants to reach, and Go's parser recovers into a partial AST. A
// partial answer beats none.
func TestResolveSpansReturnsWhatItCanFromBrokenSource(t *testing.T) {
	broken := "package p\n\nfunc Good() int { return 1 }\n\nfunc Bad( {\n"
	got, err := New().ResolveSpans([]byte(broken), []core.LineRange{{Start: 3, Count: 1}})
	if err == nil {
		t.Error("a syntax error should still be reported")
	}
	if len(got) != 1 || got[0] != "Good" {
		t.Errorf("got %v, want [Good] recovered from a file that does not parse", got)
	}
}

func TestResolveSpansEmptyInputs(t *testing.T) {
	if got, _ := New().ResolveSpans([]byte(spanFixture), nil); got != nil {
		t.Errorf("no ranges should resolve to nothing, got %v", got)
	}
	if got, _ := New().ResolveSpans(nil, []core.LineRange{{Start: 1, Count: 1}}); got != nil {
		t.Errorf("no source should resolve to nothing, got %v", got)
	}
}

func TestLocalNameMirrorsSymbolID(t *testing.T) {
	for _, tc := range []struct {
		sym  core.Symbol
		want string
	}{
		{core.Symbol{ID: "example.com/sample.Parse", Package: "example.com/sample"}, "Parse"},
		{core.Symbol{ID: "example.com/s.(*Scheduler).Next", Package: "example.com/s"}, "(*Scheduler).Next"},
		{core.Symbol{ID: "error", Package: ""}, "error"},
		// A package that is a prefix of the symbol but not its package must
		// not be stripped.
		{core.Symbol{ID: "a/b.Thing", Package: "a"}, "a/b.Thing"},
	} {
		if got := LocalName(tc.sym); got != tc.want {
			t.Errorf("LocalName(%q, pkg %q) = %q, want %q", tc.sym.ID, tc.sym.Package, got, tc.want)
		}
	}
}
