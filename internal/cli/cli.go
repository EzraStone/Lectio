// Package cli implements the lectio command line.
//
// Subcommands are dispatched by hand rather than through a framework. The
// surface is four commands with a handful of flags each, and the spec is
// explicit that v1 is one command and then nothing else for a while — a
// dependency that exists to manage growth this design has not committed to
// would be paying now for an option nobody has taken.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Command is one subcommand.
type Command struct {
	Name    string
	Summary string
	// Run receives the arguments after the subcommand name.
	Run func(ctx context.Context, env *Env, args []string) error
}

// Env carries process-level wiring, so commands stay testable without
// capturing global state.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
	// Color is set when output is going somewhere that can render it.
	Color bool
}

// DefaultEnv returns the environment for a real run.
func DefaultEnv() *Env {
	return &Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		Color:  useColor(),
	}
}

// commands is the registry, in the order help lists them.
func commands() []*Command {
	return []*Command{
		indexCmd(),
		pathCmd(),
		depsCmd(),
		probeCmd(),
		backtestCmd(),
		corpusCmd(),
		compareCmd(),
		versionCmd(),
	}
}

// Main runs the CLI and returns a process exit code.
func Main(ctx context.Context, env *Env, args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage(env.Stdout)
		return 0
	}
	// The flag spellings are aliases for the subcommand rather than a second
	// implementation. They used to print the version and nothing else, which
	// left out the Go release — the one line anyone diagnosing a wrong-looking
	// reading path needs first.
	if args[0] == "-version" || args[0] == "--version" {
		args[0] = "version"
	}

	name := args[0]
	for _, cmd := range commands() {
		if cmd.Name != name {
			continue
		}
		if err := cmd.Run(ctx, env, args[1:]); err != nil {
			if err == flag.ErrHelp {
				return 2
			}
			fmt.Fprintf(env.Stderr, "lectio %s: %v\n", name, err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(env.Stderr, "lectio: unknown command %q\n\n", name)
	usage(env.Stderr)
	return 2
}

// Version is the build version, overridable at link time.
var Version = "0.1.0-dev"

func usage(w io.Writer) {
	fmt.Fprint(w, `lectio — codebase orientation for new hires

Point it at a repo you just joined. It tells you what to read, in what
order, and why.

usage:
  lectio <command> [flags] [repo]

commands:
`)
	cmds := commands()
	width := 0
	for _, c := range cmds {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	for _, c := range cmds {
		fmt.Fprintf(w, "  %-*s  %s\n", width, c.Name, c.Summary)
	}
	fmt.Fprint(w, `
Run "lectio <command> -h" for the flags a command takes.

The index lives in .lectio/ inside the repo. Nothing leaves your machine.
`)
}

// repoArg resolves the positional repository argument, defaulting to the
// working directory.
func repoArg(args []string) (string, error) {
	if err := checkTrailingFlags(args); err != nil {
		return "", err
	}
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return abs, nil
}

// checkTrailingFlags rejects flags that appear after a positional argument.
//
// Go's flag package stops parsing at the first non-flag argument, so
// `lectio path . --top 3` parses `.` as the repository and then treats
// `--top` and `3` as two more positional arguments — silently. The user asked
// for three items and got ten, with nothing to say why.
//
// Permuting the arguments instead would be friendlier and is what most modern
// CLIs do, but it means knowing which flags take a value in order to keep a
// flag and its value together, and getting that wrong turns a silent wrong
// answer into a silently *different* wrong answer. An error costs the user one
// retry and cannot misparse anything.
//
// Everything after a bare "--" is positional by convention and is left alone.
func checkTrailingFlags(args []string) error {
	for _, arg := range args {
		if arg == "--" {
			return nil
		}
		if len(arg) > 1 && arg[0] == '-' {
			return fmt.Errorf("%s looks like a flag but came after a positional argument, "+
				"where it would be ignored — put flags first: lectio <command> %s <path>", arg, arg)
		}
	}
	return nil
}

// newFlagSet returns a flag set that prints to the command's own stderr and
// reports usage errors without the standard library's default noise.
func newFlagSet(env *Env, name, synopsis string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, "usage: lectio %s\n\nflags:\n", synopsis)
		fs.PrintDefaults()
	}
	return fs
}

// useColor reports whether to emit ANSI styling.
//
// Honors NO_COLOR, which exists precisely so tools stop each inventing their
// own opt-out, and skips styling when stdout is not a terminal so piped output
// stays clean.
func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
