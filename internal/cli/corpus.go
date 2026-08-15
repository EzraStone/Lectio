package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/EzraStone/Lectio/internal/corpus"
)

func corpusCmd() *Command {
	return &Command{
		Name:    "corpus",
		Summary: "manage the repository set Gate A runs against",
		Run:     runCorpus,
	}
}

func runCorpus(ctx context.Context, env *Env, args []string) error {
	// The subcommand comes off the front before flag parsing. Go's flag package
	// stops at the first non-flag argument, so parsing first would make
	// "corpus fetch --manifest x" silently ignore the flag — and silently
	// using the wrong corpus is precisely the failure this command exists to
	// prevent.
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}

	fs := newFlagSet(env, "corpus", "corpus <status|pin|fetch|verify> [flags]")
	var (
		manifest = fs.String("manifest", corpus.DefaultPath, "corpus manifest to read")
		cacheDir = fs.String("cache", "", "where to clone repositories (default: XDG cache)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	m, err := corpus.Load(*manifest)
	if err != nil {
		return err
	}
	cache := corpus.NewCache(*cacheDir)

	switch sub {
	case "status":
		return corpusStatus(ctx, env, cache, m)
	case "pin":
		return corpusPin(ctx, env, cache, m, *manifest)
	case "fetch":
		return corpusFetch(ctx, env, cache, m)
	case "verify":
		return corpusVerify(ctx, env, cache, m)
	default:
		fs.Usage()
		return fmt.Errorf("unknown subcommand %q", sub)
	}
}

func corpusStatus(ctx context.Context, env *Env, cache *corpus.Cache, m *corpus.Manifest) error {
	states := cache.Status(ctx, m)

	var ready, present, unpinned int
	for _, s := range states {
		if s.Ready {
			ready++
		} else if s.Present {
			present++
		}
		if s.Repo.Rev == "" {
			unpinned++
		}
	}

	env.out("")
	env.out("%s", env.bold(fmt.Sprintf("Corpus · %d repositories", len(m.Repos))))
	env.out("%s", env.dim("  cache: "+cache.Dir))
	env.out("")

	for _, s := range states {
		var mark, detail string
		switch {
		case s.Ready:
			mark, detail = env.good("ready"), s.Repo.Short()
		case s.Present:
			mark, detail = env.warn("stale"), "at "+shortRev(s.At)+", want "+s.Repo.Short()
		case s.Repo.Rev == "":
			mark, detail = env.warn("unpinned"), "run: lectio corpus pin"
		default:
			mark, detail = env.dim("absent"), s.Repo.Short()
		}
		env.out("  %-28s %-18s %s", s.Repo.Name, mark, env.dim(detail))
	}

	env.out("")
	env.out("  %d ready, %d stale, %d unpinned", ready, present, unpinned)
	if ready < len(m.Repos) {
		env.out("%s lectio corpus fetch", env.dim("next:"))
	}
	return nil
}

// corpusPin resolves every default branch and writes the manifest back.
func corpusPin(ctx context.Context, env *Env, cache *corpus.Cache, m *corpus.Manifest, dest string) error {
	env.note("%s resolving %d remotes…", env.dim("pinning"), len(m.Repos))

	changed, err := cache.Pin(ctx, m)
	if err != nil {
		return err
	}
	if changed == 0 {
		env.out("Already pinned to current heads; nothing changed.")
		return nil
	}
	if err := m.Save(dest); err != nil {
		return err
	}

	env.out("Pinned %s in %s.", pluralize(changed, "repository"), dest)
	env.out("%s", env.dim("Commit the manifest so a Gate A number can be reproduced against it."))
	return nil
}

func corpusFetch(ctx context.Context, env *Env, cache *corpus.Cache, m *corpus.Manifest) error {
	if unpinned := m.Unpinned(); len(unpinned) > 0 {
		return fmt.Errorf("%s still unpinned — run: lectio corpus pin", pluralize(len(unpinned), "repository"))
	}

	start := time.Now()
	var done, failed int
	err := cache.EnsureAll(ctx, m, func(r corpus.Repo, err error) {
		if err != nil {
			failed++
			env.note("  %s %s: %v", env.bad("failed"), r.Name, err)
			return
		}
		done++
		env.note("  %s %-28s %s", env.good("ok"), r.Name, env.dim(r.Short()))
	})
	if err != nil {
		return err
	}

	env.out("")
	env.out("Fetched %d of %d repositories in %s.", done, len(m.Repos), time.Since(start).Round(time.Second))
	if failed > 0 {
		// Not an error: a corpus where two remotes moved is still a corpus, and
		// the backtest reports how many cases it actually scored.
		env.out("%s %s could not be fetched; Gate A will run on the rest.",
			env.warn("note:"), pluralize(failed, "repository"))
	}
	return nil
}

func shortRev(rev string) string {
	if len(rev) > 10 {
		return rev[:10]
	}
	if rev == "" {
		return "unknown"
	}
	return rev
}

// pluralize renders a count with its noun, handling the -y plural the corpus
// output needs.
func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	if len(noun) > 1 && noun[len(noun)-1] == 'y' {
		return fmt.Sprintf("%d %sies", n, noun[:len(noun)-1])
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// corpusVerify checks that every pinned revision still exists.
//
// A pinned corpus is what makes a Gate A number reproducible, and pins rot
// quietly: a force-push, a rename, a deleted repository. The failure surfaces
// months later as a corpus silently covering twenty-eight repositories while
// the report still says thirty — so this is cheap enough to run before quoting
// a number.
func corpusVerify(ctx context.Context, env *Env, cache *corpus.Cache, m *corpus.Manifest) error {
	env.out("")
	env.out("%s", env.bold(fmt.Sprintf("Verifying %d pins", len(m.Repos))))
	env.out("%s", env.dim("  one ls-remote each; nothing is cloned"))
	env.out("")

	var broken, unconfirmed int
	for _, v := range corpus.Verify(ctx, cache, m) {
		// Prefer the local clone when there is one: a corpus pinned to a
		// commit rather than a branch head cannot be confirmed from the
		// remote alone, and the clone can settle it exactly.
		if local := corpus.VerifyLocal(ctx, cache, v.Repo); local.Reachable {
			v = local
		}

		switch {
		case !v.Reachable:
			broken++
			env.out("  %-30s %s", truncateName(v.Repo.Name, 30), env.bad("broken"))
			env.out("      %s", env.dim(v.Reason))
		case v.Reason != "":
			unconfirmed++
			env.out("  %-30s %s", truncateName(v.Repo.Name, 30), env.warn("unconfirmed"))
			env.out("      %s", env.dim(v.Reason))
		default:
			env.out("  %-30s %s", truncateName(v.Repo.Name, 30), env.good("ok"))
		}
	}

	env.out("")
	switch {
	case broken > 0:
		env.out("%s %d of %d pins no longer resolve — a Gate A number over this corpus",
			env.bad("broken:"), broken, len(m.Repos))
		env.out("%s", env.dim("  is not the number the manifest describes"))
		return fmt.Errorf("%d pins are unreachable", broken)
	case unconfirmed > 0:
		env.out("%s %d pins could not be confirmed from the remote alone.",
			env.warn("note:"), unconfirmed)
		env.out("%s", env.dim("  Fetch the corpus and re-run to settle them against local clones."))
	default:
		env.out("%s all %d pins resolve", env.good("ok:"), len(m.Repos))
	}
	return nil
}
