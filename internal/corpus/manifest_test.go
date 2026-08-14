package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func valid() *Manifest {
	return &Manifest{Version: 1, Repos: []Repo{
		{Name: "gorilla/mux", URL: "https://github.com/gorilla/mux.git"},
		{Name: "spf13/cobra", URL: "https://github.com/spf13/cobra.git",
			Rev: "0123456789abcdef0123456789abcdef01234567"},
	}}
}

func TestValidateAcceptsAGoodManifest(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsBadEntries(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Manifest)
		want string
	}{
		{"wrong version", func(m *Manifest) { m.Version = 99 }, "version"},
		{"empty", func(m *Manifest) { m.Repos = nil }, "no repositories"},
		{"bad name", func(m *Manifest) { m.Repos[0].Name = "notownerrepo" }, "owner/repo"},
		{"duplicate", func(m *Manifest) { m.Repos[1].Name = m.Repos[0].Name }, "twice"},
		{"no url", func(m *Manifest) { m.Repos[0].URL = "" }, "no url"},
		{"short rev", func(m *Manifest) { m.Repos[1].Rev = "abc123" }, "40-character"},
	}
	for _, c := range cases {
		m := valid()
		c.mut(m)
		err := m.Validate()
		if err == nil {
			t.Errorf("%s: expected an error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q should mention %q", c.name, err, c.want)
		}
	}
}

// The corpus drives git clone and go mod download on whatever it names, so a
// manifest is a list of things this machine will fetch and analyze.
func TestValidateRejectsNonHTTPSRemotes(t *testing.T) {
	for _, bad := range []string{
		"file:///etc",
		"ssh://git@github.com/x/y.git",
		"git://github.com/x/y.git",
		"http://github.com/x/y.git",
	} {
		m := valid()
		m.Repos[0].URL = bad
		if err := m.Validate(); err == nil {
			t.Errorf("URL %q was accepted", bad)
		}
	}
}

// The cache key must not be able to escape the cache directory.
func TestDirIsFlatAndContained(t *testing.T) {
	r := Repo{Name: "go-git/go-git"}
	if got := r.Dir(); got != "go-git__go-git" {
		t.Errorf("Dir() = %q", got)
	}
	if strings.ContainsAny(r.Dir(), `/\`) {
		t.Errorf("Dir() %q is not a single path element", r.Dir())
	}
}

func TestPinnedAndUnpinned(t *testing.T) {
	m := valid()
	if m.Pinned() {
		t.Error("Pinned() = true with an unpinned entry")
	}
	if got := m.Unpinned(); len(got) != 1 || got[0].Name != "gorilla/mux" {
		t.Errorf("Unpinned() = %+v", got)
	}

	m.Repos[0].Rev = "89abcdef0123456789abcdef0123456789abcdef"
	if !m.Pinned() {
		t.Error("Pinned() = false once everything has a rev")
	}
}

func TestSaveRoundTripsAndSorts(t *testing.T) {
	m := &Manifest{Version: 1, Repos: []Repo{
		{Name: "zzz/last", URL: "https://example.com/z.git"},
		{Name: "aaa/first", URL: "https://example.com/a.git"},
	}}

	dest := filepath.Join(t.TempDir(), "corpus.json")
	if err := m.Save(dest); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	// A pin run should produce a diff of only what moved, which needs stable
	// ordering and a trailing newline.
	if !strings.HasSuffix(string(raw), "}\n") {
		t.Error("file does not end in a newline")
	}
	if strings.Index(string(raw), "aaa/first") > strings.Index(string(raw), "zzz/last") {
		t.Error("repos were not sorted by name")
	}

	back, err := Load(dest)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(back.Repos) != 2 || back.Repos[0].Name != "aaa/first" {
		t.Errorf("round trip = %+v", back.Repos)
	}
}

func TestLoadReportsBadFiles(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o644)
	if _, err := Load(bad); err == nil {
		t.Error("malformed JSON was accepted")
	}
	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("a missing file was accepted")
	}
}

// The shipped corpus must be valid, or Gate A cannot run out of the box.
func TestShippedCorpusIsValid(t *testing.T) {
	m, err := Load(filepath.Join("..", "..", "corpus", "gate-a.json"))
	if err != nil {
		t.Fatalf("shipped corpus: %v", err)
	}
	if len(m.Repos) < 30 {
		t.Errorf("corpus has %d repositories; the spec calls for roughly thirty", len(m.Repos))
	}
	for _, r := range m.Repos {
		if r.Note == "" {
			t.Errorf("%s has no note saying why it is in the corpus", r.Name)
		}
	}
}
