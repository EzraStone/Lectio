package corpus

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Cache materializes a manifest on disk.
type Cache struct {
	// Dir holds one clone per repository.
	Dir string
	// Timeout bounds a single git operation.
	Timeout time.Duration
}

// DefaultCacheDir returns the cache location, honoring XDG_CACHE_HOME.
//
// Outside the repository on purpose: the corpus is tens of gigabytes of other
// people's code, and it has no business living in a working tree someone might
// grep, index, or accidentally commit.
func DefaultCacheDir() string {
	if dir := os.Getenv("LECTIO_CORPUS_DIR"); dir != "" {
		return dir
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "lectio-corpus")
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "lectio", "corpus")
}

// NewCache returns a cache at dir, defaulting when empty.
func NewCache(dir string) *Cache {
	if dir == "" {
		dir = DefaultCacheDir()
	}
	return &Cache{Dir: dir, Timeout: 20 * time.Minute}
}

// Path is where a repository is cloned.
func (c *Cache) Path(r Repo) string { return filepath.Join(c.Dir, r.Dir()) }

// State describes one repository's presence in the cache.
type State struct {
	Repo    Repo
	Present bool
	// At is the revision currently checked out.
	At string
	// Ready reports the clone exists and sits at the pinned revision.
	Ready bool
	Err   error
}

// Status reports the cache state for every repository, without fetching.
func (c *Cache) Status(ctx context.Context, m *Manifest) []State {
	out := make([]State, 0, len(m.Repos))
	for _, r := range m.Repos {
		s := State{Repo: r}
		dir := c.Path(r)
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			s.Present = true
			if head, err := c.git(ctx, dir, "rev-parse", "HEAD"); err == nil {
				s.At = strings.TrimSpace(head)
			}
		}
		s.Ready = s.Present && r.Rev != "" && s.At == r.Rev
		out = append(out, s)
	}
	return out
}

// Pin resolves the default-branch head for every unpinned repository.
//
// Uses `git ls-remote`, which is a single network round trip per repository
// and clones nothing. Pinning thirty repositories takes seconds rather than
// the tens of gigabytes and many minutes a clone-first approach would cost —
// which matters, because pinning is the step someone will want to redo
// whenever they refresh the corpus.
func (c *Cache) Pin(ctx context.Context, m *Manifest) (changed int, err error) {
	for i := range m.Repos {
		r := &m.Repos[i]
		out, err := c.git(ctx, "", "ls-remote", r.URL, "HEAD")
		if err != nil {
			return changed, fmt.Errorf("resolve %s: %w", r.Name, err)
		}
		fields := strings.Fields(out)
		if len(fields) == 0 || !revRE.MatchString(fields[0]) {
			return changed, fmt.Errorf("resolve %s: unexpected ls-remote output %q", r.Name, out)
		}
		if r.Rev != fields[0] {
			r.Rev = fields[0]
			changed++
		}
	}
	return changed, nil
}

// Ensure clones or updates one repository and checks out its pinned revision.
//
// Full history, never shallow. The backtest rewinds to the commit before a
// contributor's first, which is by construction deep in the past; a shallow
// clone would make most of the corpus unusable in exactly the way that is
// hardest to notice, since the failure looks like "no mid-history
// contributors" rather than an error.
func (c *Cache) Ensure(ctx context.Context, r Repo) (string, error) {
	if r.Rev == "" {
		return "", fmt.Errorf("%s is unpinned; run: lectio corpus pin", r.Name)
	}
	dir := c.Path(r)

	if info, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !info.IsDir() {
		if err := os.MkdirAll(c.Dir, 0o755); err != nil {
			return "", err
		}
		// Remove a partial clone from an interrupted run rather than trying to
		// repair it.
		os.RemoveAll(dir)
		if _, err := c.git(ctx, "", "clone", "--quiet", r.URL, dir); err != nil {
			return "", fmt.Errorf("clone %s: %w", r.Name, err)
		}
	}

	if head, err := c.git(ctx, dir, "rev-parse", "HEAD"); err == nil && strings.TrimSpace(head) == r.Rev {
		return dir, nil
	}
	if _, err := c.git(ctx, dir, "fetch", "--quiet", "origin"); err != nil {
		return "", fmt.Errorf("fetch %s: %w", r.Name, err)
	}
	if _, err := c.git(ctx, dir, "checkout", "--quiet", "--detach", r.Rev); err != nil {
		return "", fmt.Errorf("checkout %s at %s: %w", r.Name, r.Short(), err)
	}
	return dir, nil
}

// EnsureAll materializes the whole corpus, reporting progress.
//
// Sequential rather than parallel. Each clone is hundreds of megabytes and the
// bottleneck is bandwidth, so concurrency mostly buys interleaved progress
// output and a harder failure to diagnose.
func (c *Cache) EnsureAll(ctx context.Context, m *Manifest, progress func(Repo, error)) error {
	for _, r := range m.Repos {
		_, err := c.Ensure(ctx, r)
		if progress != nil {
			progress(r, err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// git runs a git command, optionally inside dir.
func (c *Cache) git(ctx context.Context, dir string, args ...string) (string, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir = dir
	// Never prompt. A corpus run is unattended, and a credential prompt on
	// repository nineteen is a hang rather than a failure.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GCM_INTERACTIVE=never")

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
