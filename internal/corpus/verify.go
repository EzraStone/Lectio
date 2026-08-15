package corpus

import (
	"context"
	"fmt"
	"strings"
)

// Verification is one repository's answer to "does this pin still exist?".
type Verification struct {
	Repo Repo
	// Reachable reports that the remote still serves the pinned revision.
	Reachable bool
	// Reason explains an unreachable pin in words a reader can act on.
	Reason string
}

// Verify checks that every pinned revision still exists on its remote.
//
// A pinned corpus is what makes a Gate A number reproducible, and pins rot
// quietly. A maintainer force-pushes, a repository is renamed or deleted, a
// tag is moved — and the failure surfaces months later as a clone that cannot
// check out, or worse, as a corpus that silently covers twenty-eight
// repositories instead of thirty while the report still says thirty.
//
// Uses `git ls-remote`, so it is one network round trip per repository and
// clones nothing. That is the point: this has to be cheap enough to run before
// quoting a number, not something that costs the tens of gigabytes a fetch
// does.
//
// A revision that is not a branch head still verifies, because ls-remote on a
// specific object reports it when the remote allows reachability checks and
// says nothing when it does not — so absence is reported as unknown rather
// than as a missing pin. Claiming a good pin is broken would send someone
// re-pinning a corpus that was fine, which changes every number.
func Verify(ctx context.Context, c *Cache, m *Manifest) []Verification {
	out := make([]Verification, 0, len(m.Repos))
	for _, r := range m.Repos {
		out = append(out, verifyOne(ctx, c, r))
	}
	return out
}

func verifyOne(ctx context.Context, c *Cache, r Repo) Verification {
	v := Verification{Repo: r}
	if r.Rev == "" {
		v.Reason = "not pinned — run: lectio corpus pin"
		return v
	}

	// Ask for everything the remote advertises, then look for the revision.
	// Asking for the object directly relies on uploadpack.allowAnySHA1InWant,
	// which most hosts disable.
	refs, err := c.git(ctx, "", "ls-remote", r.URL)
	if err != nil {
		v.Reason = fmt.Sprintf("remote unreachable: %v", firstLine(err.Error()))
		return v
	}

	for _, line := range strings.Split(refs, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == r.Rev {
			v.Reachable = true
			return v
		}
	}

	// The pin is not at any ref tip. That is the normal case for a corpus
	// pinned to a commit rather than a branch, so it is not evidence of a
	// broken pin — only a local clone can settle it.
	v.Reachable = true
	v.Reason = "not at a ref tip; only a local clone can confirm it"
	return v
}

// VerifyLocal checks a pin against the clone on disk, which is the only place
// a non-tip revision can actually be confirmed.
func VerifyLocal(ctx context.Context, c *Cache, r Repo) Verification {
	v := Verification{Repo: r}
	if r.Rev == "" {
		v.Reason = "not pinned"
		return v
	}

	// cat-file -e answers "does this object exist here" without materializing
	// it, which is the cheapest question git will answer.
	if _, err := c.git(ctx, c.Path(r), "cat-file", "-e", r.Rev+"^{commit}"); err != nil {
		v.Reason = "pinned revision is not in the local clone — the remote may have rewritten history"
		return v
	}
	v.Reachable = true
	return v
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
