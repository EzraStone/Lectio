package core

import "testing"

func TestSymbolIDPackage(t *testing.T) {
	cases := []struct {
		id   SymbolID
		want string
	}{
		{"github.com/x/y/pkg.Parse", "github.com/x/y/pkg"},
		{"github.com/x/y/pkg.(*Scheduler).Next", "github.com/x/y/pkg"},
		{"github.com/x/y/pkg.(Reader).Read", "github.com/x/y/pkg"},
		{"gopkg.in/yaml.v3.Marshal", "gopkg.in/yaml.v3"},
		{"main.main", "main"},
		{"bare", ""},
	}
	for _, c := range cases {
		if got := c.id.Package(); got != c.want {
			t.Errorf("SymbolID(%q).Package() = %q, want %q", c.id, got, c.want)
		}
	}
}

func TestSymbolIDShort(t *testing.T) {
	cases := []struct {
		id   SymbolID
		want string
	}{
		{"github.com/x/y/scheduler.Next", "scheduler.Next"},
		{"github.com/x/y/pkg.(*Scheduler).Next", "pkg.(*Scheduler).Next"},
		{"main.main", "main.main"},
		{"bare", "bare"},
	}
	for _, c := range cases {
		if got := c.id.Short(); got != c.want {
			t.Errorf("SymbolID(%q).Short() = %q, want %q", c.id, got, c.want)
		}
	}
}

func TestCommitClassification(t *testing.T) {
	cases := []struct {
		subject string
		fix     bool
		revert  bool
	}{
		{"fix: off-by-one in parseInterval", true, false},
		{"Fixes panic on empty input", true, false},
		{"Revert \"add caching layer\"", false, true},
		{"reverted the scheduler change", false, true},
		{"add scheduler backoff", false, false},
		{"prefix handling", false, false}, // must not match on "fix" inside a word
	}
	for _, c := range cases {
		got := Commit{Subject: c.subject}
		if got.IsFix() != c.fix {
			t.Errorf("IsFix(%q) = %v, want %v", c.subject, got.IsFix(), c.fix)
		}
		if got.IsRevert() != c.revert {
			t.Errorf("IsRevert(%q) = %v, want %v", c.subject, got.IsRevert(), c.revert)
		}
	}
}

func TestSymbolLines(t *testing.T) {
	if got := (Symbol{StartLine: 10, EndLine: 20}).Lines(); got != 11 {
		t.Errorf("Lines() = %d, want 11", got)
	}
	if got := (Symbol{StartLine: 10, EndLine: 0}).Lines(); got != 1 {
		t.Errorf("Lines() on unknown end = %d, want 1", got)
	}
}

func TestIsTestFile(t *testing.T) {
	for _, p := range []string{"a/b_test.go", "testdata/x.go", "pkg/testdata/y.go"} {
		if !IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"a/b.go", "internal/rank/score.go"} {
		if IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = true, want false", p)
		}
	}
}
