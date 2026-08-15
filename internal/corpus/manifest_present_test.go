package corpus_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EzraStone/Lectio/internal/corpus"
)

// repoRoot walks up to the module root so this test does not depend on where
// `go test` was invoked from.
func repoRoot(t *testing.T) string {
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

// The default manifest was ignored by .gitignore and never committed, so every
// command quoting it — make gate-a, make coupling, the CI job, and every
// command line in the Gate A write-up — was broken for anyone cloning the
// repository. Nothing failed, because the file was present locally.
func TestDefaultManifestIsTrackedByGit(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, corpus.DefaultPath)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s is missing from the working tree: %v", corpus.DefaultPath, err)
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	cmd := exec.Command("git", "ls-files", "--error-unmatch", corpus.DefaultPath)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s exists on disk but is not tracked by git — a clone of this "+
			"repository cannot run the gate:\n%s", corpus.DefaultPath, out)
	}
}

// A tracked-but-empty manifest would pass the check above and fail everything
// downstream, so the contents are asserted too.
func TestDefaultManifestIsFullyPinned(t *testing.T) {
	m, err := corpus.Load(filepath.Join(repoRoot(t), corpus.DefaultPath))
	if err != nil {
		t.Fatalf("load %s: %v", corpus.DefaultPath, err)
	}

	if len(m.Repos) < 25 {
		t.Errorf("manifest has %d repositories; the spec calls for roughly thirty", len(m.Repos))
	}
	for _, r := range m.Repos {
		if r.Rev == "" {
			t.Errorf("%s is unpinned — a corpus following HEAD gives a different answer every week", r.Name)
		}
		if !strings.HasPrefix(r.URL, "https://") {
			t.Errorf("%s has a non-https URL: %s", r.Name, r.URL)
		}
		if r.Note == "" {
			t.Errorf("%s has no note saying why it is in the corpus", r.Name)
		}
	}
}
