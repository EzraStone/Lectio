package cli

import (
	"context"
	"runtime"
	"runtime/debug"
)

func versionCmd() *Command {
	return &Command{
		Name:    "version",
		Summary: "print the version, and the Go release the analysis is capped by",
		Run:     runVersion,
	}
}

// runVersion prints the build, leading with the Go release.
//
// The Go version is not incidental detail here, it is the single most useful
// diagnostic this tool has. The type checker is compiled in, so a binary built
// with 1.24 cannot type-check a repository requiring 1.25 — it indexes anyway,
// warns, and produces a call graph missing every package in that module. On
// go-git that was 19,005 call edges against 36,615.
//
// Someone reporting "the reading path looks wrong" needs this line before
// anything else, so it is printed without being asked for rather than hidden
// behind a flag.
func runVersion(_ context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "version", "version")
	if err := fs.Parse(args); err != nil {
		return err
	}

	env.out("lectio %s", Version)
	env.out("  built with %s for %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	env.out("%s", env.dim("  the type checker is compiled in, so analysis of a repository requiring"))
	env.out("%s", env.dim("  a newer Go than this will be incomplete — lectio index says so when it happens"))

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				env.out("  revision %s", shortHash(s.Value))
			}
		}
	}
	return nil
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
