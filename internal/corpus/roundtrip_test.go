package corpus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Pin rewrites the manifest by marshalling the struct, so any field the struct
// does not model is silently dropped. That is how the holdout corpus lost its
// name and note the first time it was pinned: the fields were in the file, the
// struct had no place for them, and the rewrite quietly discarded both.
//
// The failure is invisible at the time — the pin succeeds and the revisions
// are correct — and only shows up later as a report that cannot say which
// corpus it ran against.
func TestManifestRoundTripKeepsItsMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.json")
	original := `{
  "version": 1,
  "name": "gate-a-holdout",
  "note": "why this corpus exists",
  "repos": [
    {"name": "a/b", "url": "https://example.com/a/b.git", "rev": "0123456789abcdef0123456789abcdef01234567", "note": "why this repo"}
  ]
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "gate-a-holdout" || m.Note == "" {
		t.Fatalf("Load dropped metadata: name=%q note=%q", m.Name, m.Note)
	}

	// Marshal it back the way Pin does.
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"version", "name", "note", "repos"} {
		if _, ok := back[key]; !ok {
			t.Errorf("a round trip dropped %q:\n%s", key, out)
		}
	}
	if got := back["name"]; got != "gate-a-holdout" {
		t.Errorf("name became %v", got)
	}
}

// A corpus with no name still has to render as something a reader can use.
func TestLabelFallsBack(t *testing.T) {
	if got := (&Manifest{}).Label(); got == "" {
		t.Error("an unnamed corpus has no label at all")
	}
	if got := (&Manifest{Name: "gate-a"}).Label(); got != "gate-a" {
		t.Errorf("Label = %q, want gate-a", got)
	}
}

// The two shipped manifests must stay disjoint, or the holdout stops being a
// holdout and the confirmation it exists for means nothing.
func TestShippedCorporaAreDisjoint(t *testing.T) {
	root := repoRootForCorpus(t)
	a, err := Load(filepath.Join(root, "corpus", "gate-a.json"))
	if err != nil {
		t.Skipf("gate-a.json unavailable: %v", err)
	}
	b, err := Load(filepath.Join(root, "corpus", "gate-a-holdout.json"))
	if err != nil {
		t.Skipf("holdout unavailable: %v", err)
	}

	in := make(map[string]bool, len(a.Repos))
	for _, r := range a.Repos {
		in[r.Name] = true
	}
	for _, r := range b.Repos {
		if in[r.Name] {
			t.Errorf("%s is in both corpora — the holdout is not a holdout", r.Name)
		}
	}
}

func repoRootForCorpus(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("module root not found")
	return ""
}
