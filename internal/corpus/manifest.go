// Package corpus defines and materializes the set of repositories Gate A runs
// against.
//
// The corpus is a file in the repository rather than a flag someone types,
// because a go/no-go number is only worth having if a second person can
// reproduce it. Revisions are pinned for the same reason: a corpus that
// follows upstream HEAD produces a different number every week, and then
// nobody can tell whether a change in the score came from a change in the
// ranking.
package corpus

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Repo is one corpus entry.
type Repo struct {
	// Name is the canonical owner/repo, used as the cache key.
	Name string `json:"name"`
	// URL is the clone source.
	URL string `json:"url"`
	// Rev is the pinned commit. Empty means unpinned, which `lectio corpus
	// pin` resolves and writes back.
	Rev string `json:"rev,omitempty"`
	// Note records why this repository is in the corpus, or a known caveat.
	Note string `json:"note,omitempty"`
	// MinGo records the Go version the repository needs, when it is newer than
	// the project's floor. Lectio's type checker is compiled in, so a binary
	// built with an older Go silently loses most of the call graph here.
	MinGo string `json:"min_go,omitempty"`
}

// Manifest is the corpus definition.
type Manifest struct {
	Version int `json:"version"`
	// Name distinguishes one corpus from another in a report.
	//
	// There is more than one now, and a number computed over a holdout means
	// something different from the same number over the corpus that produced
	// the hypothesis. A report that cannot say which it ran against invites
	// exactly the confusion the holdout exists to prevent.
	Name string `json:"name,omitempty"`
	// Note says why this corpus exists and what it is for.
	Note  string `json:"note,omitempty"`
	Repos []Repo `json:"repos"`
}

// Label names the corpus for a report, falling back to something usable.
func (m *Manifest) Label() string {
	if m.Name != "" {
		return m.Name
	}
	return "unnamed corpus"
}

// CurrentVersion is bumped when the manifest shape changes.
const CurrentVersion = 1

// Load reads a manifest from disk.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse corpus %s: %w", path, err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("corpus %s: %w", path, err)
	}
	return &m, nil
}

// Save writes a manifest back, formatted for review in a diff.
//
// Repos are sorted by name and the file ends in a newline, so `corpus pin`
// produces a diff showing only the revisions that actually moved.
func (m *Manifest) Save(dest string) error {
	sorted := *m
	sorted.Repos = append([]Repo(nil), m.Repos...)
	sort.Slice(sorted.Repos, func(i, j int) bool { return sorted.Repos[i].Name < sorted.Repos[j].Name })

	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dest, append(data, '\n'), 0o644)
}

var (
	nameRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	revRE  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Validate checks a manifest is usable before anything spends minutes acting
// on it.
func (m *Manifest) Validate() error {
	if m.Version != CurrentVersion {
		return fmt.Errorf("version %d, this binary understands %d", m.Version, CurrentVersion)
	}
	if len(m.Repos) == 0 {
		return fmt.Errorf("no repositories listed")
	}

	seen := make(map[string]bool, len(m.Repos))
	for i, r := range m.Repos {
		switch {
		case !nameRE.MatchString(r.Name):
			return fmt.Errorf("repo %d: name %q is not owner/repo", i, r.Name)
		case seen[r.Name]:
			return fmt.Errorf("repo %q listed twice", r.Name)
		case r.URL == "":
			return fmt.Errorf("repo %q: no url", r.Name)
		}
		if err := checkURL(r.URL); err != nil {
			return fmt.Errorf("repo %q: %w", r.Name, err)
		}
		if r.Rev != "" && !revRE.MatchString(r.Rev) {
			return fmt.Errorf("repo %q: rev %q is not a full 40-character sha", r.Name, r.Rev)
		}
		seen[r.Name] = true
	}
	return nil
}

// checkURL rejects anything that is not an https git remote.
//
// The corpus drives `git clone` and `go mod download` on whatever it names, so
// a manifest is a list of things this machine will fetch and analyze. Refusing
// non-https schemes keeps a stray file:// or ssh:// entry from turning a
// review of a data file into a review of what it can reach.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("unparseable url %q: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("url %q must be https", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("url %q has no host", raw)
	}
	return nil
}

// Pinned reports whether every repository has a revision.
func (m *Manifest) Pinned() bool {
	for _, r := range m.Repos {
		if r.Rev == "" {
			return false
		}
	}
	return true
}

// Unpinned lists repositories still following their default branch.
func (m *Manifest) Unpinned() []Repo {
	var out []Repo
	for _, r := range m.Repos {
		if r.Rev == "" {
			out = append(out, r)
		}
	}
	return out
}

// Dir is the cache subdirectory for a repository: owner__repo, so the cache
// stays one level deep and a name cannot escape it.
func (r Repo) Dir() string {
	return strings.ReplaceAll(r.Name, "/", "__")
}

// Short renders the revision for display.
func (r Repo) Short() string {
	if len(r.Rev) > 10 {
		return r.Rev[:10]
	}
	if r.Rev == "" {
		return "unpinned"
	}
	return r.Rev
}

// DefaultPath is where the shipped corpus lives, relative to the repo root.
var DefaultPath = path.Join("corpus", "gate-a.json")
