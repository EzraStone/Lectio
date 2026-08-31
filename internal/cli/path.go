package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
	"github.com/EzraStone/Lectio/internal/store"
)

func pathCmd() *Command {
	return &Command{
		Name:    "path",
		Summary: "print a reading path: what to read, in what order, and why",
		Run:     runPath,
	}
}

func runPath(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "path", "path [flags] [repo]")
	var (
		top      = fs.Int("top", 10, "how many items to return")
		task     = fs.String("task", "", "the area you are working in: a package, directory, file, or symbol")
		asJSON   = fs.Bool("json", false, "emit JSON instead of formatted text")
		explain  = fs.Bool("explain", false, "show every signal's contribution per item")
		months   = fs.Int("months", 12, "months of history the signals consider")
		noRecall = fs.Bool("fresh", false, "ignore what you have already engaged with")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, err := repoArg(fs.Args())
	if err != nil {
		return err
	}

	s, err := store.OpenRepo(ctx, root)
	if err != nil {
		return err
	}
	defer s.Close()

	v, err := index.Load(ctx, s)
	if err != nil {
		return err
	}
	if len(v.Symbols) == 0 {
		return fmt.Errorf("no index for %s yet — run: lectio index %s", root, root)
	}

	p := rank.DefaultParams()
	p.ChurnWindow = time.Duration(*months) * 30 * 24 * time.Hour

	var taskLabel string
	if *task != "" {
		seeds, matched := rank.ResolveTask(v, *task)
		if len(seeds) == 0 {
			return fmt.Errorf("nothing in this repo matches %q — try a package path, a directory, or a function name", *task)
		}
		p.Task, taskLabel = seeds, matched
	}

	// Ranking is discounted by what the user has already demonstrated they
	// understand. This is the only place the comprehension score reaches
	// output, and it moves the order rather than being displayed — the spec is
	// firm that it is never the headline.
	if !*noRecall {
		if fam, err := familiarity(ctx, s, p.Now); err == nil {
			p.Familiarity = fam
		}
	}

	res := rank.Rank(v, p, rank.DefaultWeights())
	res.TaskMatch = taskLabel

	facts := rank.Gather(v, p)
	items := rank.Annotate(res.Path(*top), facts, taskLabel)

	if *asJSON {
		return emitJSON(env, v, res, items)
	}
	renderPath(env, v, res, items, *explain, taskLabel)
	return nil
}

// familiarity reads the per-user engagement state into ranking input.
func familiarity(ctx context.Context, s *store.Store, now time.Time) (map[core.SymbolID]float64, error) {
	all, err := s.AllEngagement(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[core.SymbolID]float64, len(all))
	for id, e := range all {
		out[id] = e.Familiarity(now)
	}
	return out, nil
}

func renderPath(env *Env, v *index.View, res *rank.Result, items []rank.Item, explain bool, taskLabel string) {
	if len(items) == 0 {
		env.out("Nothing scored above zero. That usually means the index is thin —")
		env.out("no git history, or a repo small enough that no signal has anything to say.")
		return
	}

	env.out("")
	header := fmt.Sprintf("Reading path · %d of %d symbols", len(items), len(v.Symbols))
	if taskLabel != "" {
		header += " · scoped to " + taskLabel
	}
	env.out("%s", env.bold(header))
	env.out("")

	lastTier := 0
	for i, item := range items {
		if item.Tier != lastTier {
			env.out("%s", env.dim(strings.ToUpper(rank.TierLabel(item.Tier))))
			lastTier = item.Tier
		}

		env.out("  %2d. %s", i+1, env.accent(item.Symbol.ID.Short()))
		env.out("      %s%s", env.dim(item.Symbol.File), location(item.Symbol.StartLine))
		env.out("      %s", item.Rationale)

		if explain {
			env.out("      %s", env.dim(contributions(item)))
		}
		env.out("")
	}

	renderSignalStatus(env, res)
}

// renderSignalStatus reports which signals had anything to say.
//
// A signal with no data is named rather than silently counted as zero. "We
// found no AI-authorship markers in this repo" and "this code was not written
// by a machine" are different claims, and a tool whose entire premise is
// trustworthy grading does not get to blur them.
//
// Which means the two have to be printed differently, and for a long time they
// were not: a repository with a year of authorship and nobody gone quiet was
// told "no data for: orphaning" when the answer was that nothing is orphaned.
func renderSignalStatus(env *Env, res *rank.Result) {
	if len(res.Unavailable) > 0 {
		env.note("%s no data for: %s", env.dim("signals:"), signalNames(res.Unavailable))
	}
	if len(res.Empty) > 0 {
		env.note("%s ran and found nothing: %s", env.dim("signals:"), signalNames(res.Empty))
	}
}

func signalNames(sigs []rank.Signal) string {
	names := make([]string, 0, len(sigs))
	for _, s := range sigs {
		names = append(names, string(s))
	}
	return strings.Join(names, ", ")
}

func contributions(item rank.Item) string {
	parts := make([]string, 0, len(item.Contributions))
	for _, sig := range rank.AllSignals {
		if v, ok := item.Contributions[sig]; ok && v > 0 {
			parts = append(parts, fmt.Sprintf("%s %.2f", sig, v))
		}
	}
	if len(parts) == 0 {
		return "no signal contributed"
	}
	return strings.Join(parts, "  ")
}

func location(line int) string {
	if line <= 0 {
		return ""
	}
	return fmt.Sprintf(":%d", line)
}

// jsonItem is the machine-readable shape. It is a separate type on purpose:
// the internal Item can change freely, and anything piping lectio into another
// tool should not break because a field was renamed.
type jsonItem struct {
	Symbol    string             `json:"symbol"`
	Name      string             `json:"name"`
	Kind      string             `json:"kind"`
	File      string             `json:"file"`
	Line      int                `json:"line"`
	Tier      int                `json:"tier"`
	Score     float64            `json:"score"`
	Rationale string             `json:"rationale"`
	Signals   map[string]float64 `json:"signals,omitempty"`
}

type jsonOutput struct {
	Repo   string   `json:"repo"`
	Head   string   `json:"head,omitempty"`
	Task   string   `json:"task,omitempty"`
	Active []string `json:"active_signals"`
	// Silent is the union, kept so a consumer written against the older shape
	// still reads the same thing.
	Silent []string `json:"silent_signals,omitempty"`
	// Unavailable and Empty split it the way the text output does. A machine
	// reading only silent_signals gets the conflation the human output no
	// longer has, which is the wrong half of the interface to leave behind.
	Unavailable []string `json:"unavailable_signals,omitempty"`
	Empty       []string `json:"empty_signals,omitempty"`
	Symbols     int      `json:"symbols_indexed"`
	// Files is how many distinct files the path spans. A path of ten across
	// one file and one across seven are different objects, and nothing else in
	// this output tells them apart.
	Files int        `json:"files_spanned"`
	Items []jsonItem `json:"path"`
}

func emitJSON(env *Env, v *index.View, res *rank.Result, items []rank.Item) error {
	out := jsonOutput{
		Repo:    v.Root,
		Head:    v.Head,
		Task:    res.TaskMatch,
		Symbols: len(v.Symbols),
		Items:   make([]jsonItem, 0, len(items)),
	}
	for _, s := range res.Active {
		out.Active = append(out.Active, string(s))
	}
	for _, s := range res.Silent {
		out.Silent = append(out.Silent, string(s))
	}
	for _, s := range res.Unavailable {
		out.Unavailable = append(out.Unavailable, string(s))
	}
	for _, s := range res.Empty {
		out.Empty = append(out.Empty, string(s))
	}
	out.Files = rank.Files(items)
	for _, item := range items {
		ji := jsonItem{
			Symbol:    string(item.Symbol.ID),
			Name:      item.Symbol.Name,
			Kind:      string(item.Symbol.Kind),
			File:      item.Symbol.File,
			Line:      item.Symbol.StartLine,
			Tier:      item.Tier,
			Score:     round4(item.Score),
			Rationale: item.Rationale,
			Signals:   map[string]float64{},
		}
		for sig, val := range item.Contributions {
			ji.Signals[string(sig)] = round4(val)
		}
		out.Items = append(out.Items, ji)
	}

	enc := json.NewEncoder(env.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func round4(v float64) float64 {
	return float64(int(v*10000+0.5)) / 10000
}
