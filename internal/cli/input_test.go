package cli

import (
	"bufio"
	"strings"
	"testing"
)

func readerFor(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }

// A skip is neutral and never touches the accuracy counters, so anything
// ambiguous has to become a skip rather than a wrong answer. Grading a typo as
// wrong puts a mark against someone's comprehension record for mistyping,
// which is how a score becomes worth ignoring.
func TestReadChoiceTreatsAnythingUnclearAsASkip(t *testing.T) {
	for _, in := range []string{
		"",       // bare enter
		"\n",     //
		"skip\n", //
		"SKIP\n", // case-insensitive
		"  \n",   // whitespace
		"0\n",    // below range
		"5\n",    // above range
		"two\n",  // not a number
		"1.5\n",  // not an integer
		"-1\n",   //
		"1 2\n",  // ambiguous
	} {
		env, _, _ := testEnv()
		got := readChoice(env, readerFor(in), 3)
		if !got.Skipped {
			t.Errorf("readChoice(%q) = %+v, want a skip", in, got)
		}
	}
}

func TestReadChoiceAcceptsAnOfferedNumber(t *testing.T) {
	for in, want := range map[string]int{"1\n": 0, "2\n": 1, "3\n": 2, " 2 \n": 1} {
		env, _, _ := testEnv()
		got := readChoice(env, readerFor(in), 3)
		if got.Skipped {
			t.Errorf("readChoice(%q) skipped a valid answer", in)
			continue
		}
		if got.Choice != want {
			t.Errorf("readChoice(%q).Choice = %d, want %d (zero-based)", in, got.Choice, want)
		}
	}
}

// Out-of-range input says so rather than failing silently, because a user who
// typed 5 into a three-option question needs to know it was not recorded.
func TestReadChoiceExplainsAnOutOfRangeAnswer(t *testing.T) {
	env, _, errOut := testEnv()
	readChoice(env, readerFor("9\n"), 3)
	if !strings.Contains(errOut.String(), "not one of the options") {
		t.Errorf("no explanation for an out-of-range answer: %q", errOut.String())
	}
}

// People separate names however they like. Accepting one separator would make
// a correct answer read as wrong on punctuation.
func TestReadNamesAcceptsAnySensibleSeparator(t *testing.T) {
	for _, in := range []string{
		"Parse, Cycle, Requeue\n",
		"Parse Cycle Requeue\n",
		"Parse,Cycle,Requeue\n",
		"Parse; Cycle;Requeue\n",
		"Parse,  Cycle\tRequeue\n",
	} {
		env, _, _ := testEnv()
		got := readNames(env, readerFor(in))
		if got.Skipped {
			t.Fatalf("readNames(%q) skipped", in)
		}
		if len(got.Names) != 3 {
			t.Errorf("readNames(%q) = %v, want three names", in, got.Names)
		}
	}
}

func TestReadNamesSkipsOnNothingUsable(t *testing.T) {
	for _, in := range []string{"", "\n", "skip\n", "  \n", ",,,\n", " ; , \n"} {
		env, _, _ := testEnv()
		if got := readNames(env, readerFor(in)); !got.Skipped {
			t.Errorf("readNames(%q) = %+v, want a skip", in, got)
		}
	}
}

// Choice must be -1 on a names answer, or a grader could read it as a
// selection of the first option.
func TestReadNamesDoesNotLookLikeAChoice(t *testing.T) {
	env, _, _ := testEnv()
	got := readNames(env, readerFor("Parse\n"))
	if got.Choice != -1 {
		t.Errorf("Choice = %d on a names answer, want -1", got.Choice)
	}
}
