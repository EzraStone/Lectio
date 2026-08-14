package cli

import "fmt"

// ANSI codes, applied only when Env.Color is set.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
)

func (e *Env) style(code, s string) string {
	if !e.Color || s == "" {
		return s
	}
	return code + s + ansiReset
}

func (e *Env) bold(s string) string   { return e.style(ansiBold, s) }
func (e *Env) dim(s string) string    { return e.style(ansiDim, s) }
func (e *Env) accent(s string) string { return e.style(ansiCyan, s) }
func (e *Env) warn(s string) string   { return e.style(ansiYellow, s) }
func (e *Env) bad(s string) string    { return e.style(ansiRed, s) }
func (e *Env) good(s string) string   { return e.style(ansiGreen, s) }

// out writes a formatted line to stdout.
func (e *Env) out(format string, args ...any) {
	fmt.Fprintf(e.Stdout, format+"\n", args...)
}

// note writes an advisory line to stderr, so it never contaminates piped
// output that another program is parsing.
func (e *Env) note(format string, args ...any) {
	fmt.Fprintf(e.Stderr, format+"\n", args...)
}
