// Command lectio builds a reading path for a codebase you just joined.
//
// Everything it knows lives in .lectio/ inside the repository you point it at.
// Nothing is uploaded, and there is no account.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/EzraStone/Lectio/internal/cli"
)

// version is set at link time by the release build. Left empty otherwise, so
// an unreleased binary keeps the package default rather than claiming a
// version number nobody can map back to a commit.
var version string

func main() {
	if version != "" {
		cli.Version = version
	}

	// Indexing a large repository takes minutes and spawns child processes.
	// Cancelling on interrupt lets the in-flight transaction roll back, so a
	// Ctrl-C leaves the previous index intact rather than half-replaced.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(cli.Main(ctx, cli.DefaultEnv(), os.Args[1:]))
}
