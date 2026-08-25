package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/EzraStone/Lectio/internal/backtest"
)

func compareCmd() *Command {
	return &Command{
		Name:    "compare",
		Summary: "set two backtest reports side by side, if they are comparable",
		Run:     runCompare,
	}
}

// runCompare reads two --json reports and reports what moved.
//
// The first thing it answers is whether the comparison is legitimate. Two runs
// of the same command can measure different populations — which cases survive
// depends on whether each rewound revision's dependencies resolved — and a
// delta across two populations is not a measurement. When that happens this
// prints why and stops, rather than printing the deltas with a caveat nobody
// carries forward.
func runCompare(_ context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "compare", "compare <before.json> <after.json>")
	asJSON := fs.Bool("json", false, "emit JSON instead of formatted text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return fmt.Errorf("compare needs two report files")
	}

	before, err := readReport(fs.Arg(0))
	if err != nil {
		return err
	}
	after, err := readReport(fs.Arg(1))
	if err != nil {
		return err
	}

	c := backtest.Compare(before, after)
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(c)
	}
	renderComparison(env, c, fs.Arg(0), fs.Arg(1))
	if !c.Comparable {
		return fmt.Errorf("the two runs are not comparable")
	}
	return nil
}

func readReport(path string) (backtest.Report, error) {
	var r backtest.Report
	data, err := os.ReadFile(path)
	if err != nil {
		return r, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("parse %s: %w — it should be the output of `lectio backtest --json`", path, err)
	}
	return r, nil
}

func renderComparison(env *Env, c backtest.Comparison, aPath, bPath string) {
	env.out("")
	env.out("%s", env.bold("Comparing two runs"))
	env.out("%s", env.dim("  before: "+aPath))
	env.out("%s", env.dim("  after:  "+bPath))
	env.out("")

	if !c.Comparable {
		env.out("%s", env.bad("not comparable"))
		for _, l := range wrap(c.Why, 70) {
			env.out("  %s", l)
		}
		return
	}

	env.out("  %-26s %10s %10s %9s %9s", "strategy", "prec before", "after", "Δ", "Δ matched")
	env.out("  %s", dashes(70))
	for _, row := range c.Rows {
		label := fmt.Sprintf("%-26s", row.Strategy)
		if row.Strategy == "lectio" {
			label = env.accent(label)
		}
		delta := fmt.Sprintf("%+8.1f", row.PrecisionDelta*100)
		matched := "        —"
		if row.MatchedA > 0 || row.MatchedB > 0 {
			matched = fmt.Sprintf("%+8.1f", row.MatchedDelta*100)
		}
		if row.SignFlip {
			// Crossing chance is a change of claim, not a change of degree.
			matched = env.bad(matched)
		}
		env.out("  %s %9.1f%% %9.1f%% %s %s",
			label, row.PrecisionA*100, row.PrecisionB*100, delta, matched)
	}

	for _, only := range []struct {
		names []string
		where string
	}{{c.OnlyInA, "only in the first run"}, {c.OnlyInB, "only in the second"}} {
		for _, n := range only.names {
			env.out("  %s %s", env.dim(fmt.Sprintf("%-26s", n)), env.dim(only.where))
		}
	}

	var flips int
	for _, row := range c.Rows {
		if row.SignFlip {
			flips++
		}
	}
	if flips > 0 {
		env.out("")
		for _, l := range wrap(fmt.Sprintf(
			"%d strategies crossed chance on size-matched pairs. That is a change in what "+
				"they claim, not a change in degree, and it is the kind of movement a "+
				"percentage delta hides.", flips), 70) {
			env.out("%s", env.warn("  "+l))
		}
	}
}
