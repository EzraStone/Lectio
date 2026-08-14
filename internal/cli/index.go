package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/EzraStone/Lectio/internal/adapter"
	_ "github.com/EzraStone/Lectio/internal/adapter/golang" // registers the Go adapter
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/store"
)

func indexCmd() *Command {
	return &Command{
		Name:    "index",
		Summary: "analyze a repository and build its index",
		Run:     runIndex,
	}
}

func runIndex(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "index", "index [flags] [repo]")
	var (
		months     = fs.Int("months", 12, "months of history to read")
		runTests   = fs.Bool("run-tests", false, "run the test suite to map tests to symbols (executes repo code)")
		noDynamic  = fs.Bool("no-dynamic", false, "skip interface-dispatch resolution (faster, less complete)")
		adapterArg = fs.String("adapter", "", "force a language adapter instead of detecting one")
		quiet      = fs.Bool("quiet", false, "print only errors")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, err := repoArg(fs.Args())
	if err != nil {
		return err
	}

	a, err := pickAdapter(*adapterArg, root)
	if err != nil {
		return err
	}

	opts := adapter.DefaultOptions()
	opts.HistoryWindow = time.Duration(*months) * 30 * 24 * time.Hour
	opts.RunTests = *runTests
	opts.ResolveDynamic = !*noDynamic

	s, err := store.OpenRepo(ctx, root)
	if err != nil {
		return err
	}
	defer s.Close()

	if !*quiet {
		env.note("%s %s with the %s adapter…", env.dim("indexing"), root, a.Name())
		if *runTests {
			env.note("%s running the repository's test suite, as asked", env.warn("note:"))
		}
	}

	res, err := index.Build(ctx, s, a, root, opts)
	if err != nil {
		return err
	}

	// Warnings go to stderr regardless of -quiet. Someone about to trust an
	// answer needs to know the graph behind it is partial, and suppressing
	// that on request would make -quiet a way to hide a broken index.
	for _, w := range res.Warnings {
		env.note("%s %s", env.warn("warning:"), w)
	}
	if *quiet {
		return nil
	}

	env.out("")
	env.out("%s", env.bold("index built"))
	env.out("  %-14s %s", "repository", root)
	env.out("  %-14s %s", "database", s.Path())
	if res.Head != "" {
		env.out("  %-14s %s", "at commit", short(res.Head))
	}
	env.out("")
	env.out("  %-14s %d", "symbols", res.Stats.Symbols)
	env.out("  %-14s %d", "files", res.Stats.Files)
	env.out("  %-14s %d", "call edges", res.Stats.CallEdges)
	if res.Stats.Coverage > 0 {
		env.out("  %-14s %d", "test links", res.Stats.Coverage)
	}
	env.out("  %-14s %d", "commits", res.Stats.Commits)
	if res.PackagesLoaded > 0 {
		env.out("  %-14s %d loaded, %d with errors", "packages", res.PackagesLoaded, res.PackagesFailed)
	}
	env.out("  %-14s %s", "took", res.Duration.Round(time.Millisecond))
	env.out("")
	env.out("%s lectio path %s", env.dim("next:"), root)
	return nil
}

// pickAdapter honors an explicit choice, otherwise detects.
func pickAdapter(name, root string) (adapter.LanguageAdapter, error) {
	if name != "" {
		return adapter.ByName(name)
	}
	a, err := adapter.Select(root)
	if err != nil {
		return nil, fmt.Errorf("%w\n\nlectio indexes Go repositories today. The LanguageAdapter seam exists so "+
			"a second language is one implementation, not a rewrite", err)
	}
	return a, nil
}

func short(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
