package cli

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/probe"
	"github.com/EzraStone/Lectio/internal/rank"
	"github.com/EzraStone/Lectio/internal/store"
)

func probeCmd() *Command {
	return &Command{
		Name:    "probe",
		Summary: "answer a question about the code, graded against ground truth",
		Run:     runProbe,
	}
}

func runProbe(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "probe", "probe [flags] [repo]")
	var (
		count  = fs.Int("n", 1, "how many probes to ask")
		health = fs.Bool("health", false, "report whether the probes are working, and stop")
		forget = fs.Bool("forget", false, "erase all local probe history for this repo, and stop")
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

	if *forget {
		if err := s.PurgeState(ctx); err != nil {
			return err
		}
		env.out("Local probe history and engagement records erased. The index is untouched.")
		return nil
	}

	sched := probe.NewScheduler(s, time.Now().UTC())

	if *health {
		return reportHealth(ctx, env, sched)
	}

	v, err := index.Load(ctx, s)
	if err != nil {
		return err
	}
	if len(v.Symbols) == 0 {
		return fmt.Errorf("no index for %s yet — run: lectio index %s", root, root)
	}

	// Candidates come from the reading path, so questions are about code the
	// person has a reason to care about. Probing a random symbol is how a tool
	// becomes trivia.
	p := rank.DefaultParams()
	ranked := rank.Rank(v, p, rank.DefaultWeights())
	candidates := make([]core.Symbol, 0, 40)
	for _, item := range ranked.Select(40) {
		candidates = append(candidates, item.Symbol)
	}

	pctx := probe.NewContext(v, probe.TemplateStems{})
	reader := bufio.NewReader(env.Stdin)

	for i := 0; i < *count; i++ {
		pr, ok, err := sched.Next(ctx, pctx, candidates)
		if err != nil {
			return err
		}
		if !ok {
			env.out("Nothing fair to ask right now — everything in range was probed recently,")
			env.out("or the graph is too thin to build a question anyone could answer.")
			return nil
		}
		if err := ask(ctx, env, sched, pctx, pr, reader); err != nil {
			return err
		}
	}
	return nil
}

// ask presents one probe and records the outcome.
func ask(ctx context.Context, env *Env, sched *probe.Scheduler, pctx *probe.Context, pr probe.Probe, reader *bufio.Reader) error {
	asked := time.Now()

	env.out("")
	env.out("%s", env.bold(pr.Stem))
	env.out("%s", env.dim(pr.Subject.File))
	env.out("")

	var answer probe.Answer
	if len(pr.Choices) > 0 {
		for i, c := range pr.Choices {
			env.out("  %d. %s", i+1, c.Label)
		}
		env.out("")
		answer = readChoice(env, reader, len(pr.Choices))
	} else {
		env.out("%s", env.dim("name what breaks, comma separated — or press enter to skip"))
		answer = readNames(env, reader)
	}

	elapsed := time.Since(asked)
	grade := pr.Grade(answer, pctx)

	env.out("")
	switch grade.Outcome {
	case store.OutcomeCorrect:
		// The separator matters: "correct missed cli.runBacktest" reads as a
		// contradiction until you parse it twice. A high F1 can still have
		// gaps, and the two clauses need visibly separating.
		env.out("%s %s", env.good("correct ·"), env.dim(grade.Explanation))
	case store.OutcomePartial:
		env.out("%s %s", env.warn("partly ·"), grade.Explanation)
	case store.OutcomeSkipped:
		// A skip is neutral, never a failure, and the wording has to carry
		// that or people learn to guess rather than skip.
		env.out("%s %s", env.dim("skipped ·"), answerKey(pr))
	default:
		env.out("%s %s", env.bad("not quite ·"), grade.Explanation)
	}

	if pr.Kind == probe.KindBlastRadius && grade.Outcome != store.OutcomeSkipped {
		env.out("%s", env.dim(fmt.Sprintf("  precision %.0f%%  recall %.0f%%  in %s",
			grade.Precision*100, grade.Recall*100, elapsed.Round(time.Second))))
	}
	env.out("")

	return sched.Record(ctx, pr, grade, asked, elapsed)
}

func answerKey(pr probe.Probe) string {
	if len(pr.Choices) > 0 {
		for _, c := range pr.Choices {
			if c.Correct {
				return "the answer is " + c.Label
			}
		}
	}
	names := make([]string, 0, len(pr.Expected))
	for _, id := range pr.Expected {
		names = append(names, id.Short())
	}
	return "the answer is " + strings.Join(names, ", ")
}

func readChoice(env *Env, reader *bufio.Reader, n int) probe.Answer {
	fmt.Fprint(env.Stdout, "> ")
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return probe.Skip()
	}
	text := strings.TrimSpace(line)
	if text == "" || strings.EqualFold(text, "skip") {
		return probe.Skip()
	}
	choice, err := strconv.Atoi(text)
	if err != nil || choice < 1 || choice > n {
		// Input that is not one of the offered numbers is a typo or a
		// misunderstanding of the format, not a wrong answer. Grading it as
		// wrong would put a mark against someone's comprehension record for
		// mistyping, which is exactly the kind of noise that makes a score
		// worth ignoring.
		env.note("that is not one of the options — counting it as a skip")
		return probe.Skip()
	}
	return probe.Answer{Choice: choice - 1}
}

func readNames(env *Env, reader *bufio.Reader) probe.Answer {
	fmt.Fprint(env.Stdout, "> ")
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return probe.Skip()
	}
	text := strings.TrimSpace(line)
	if text == "" || strings.EqualFold(text, "skip") {
		return probe.Skip()
	}

	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == ';'
	})
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			names = append(names, f)
		}
	}
	if len(names) == 0 {
		return probe.Skip()
	}
	return probe.Answer{Names: names, Choice: -1}
}

func reportHealth(ctx context.Context, env *Env, sched *probe.Scheduler) error {
	h, err := sched.Health(ctx, 30*24*time.Hour)
	if err != nil {
		return err
	}

	if h.Asked == 0 {
		env.out("No probes answered yet.")
		return nil
	}

	env.out("%s", env.bold("Probe health · last 30 days"))
	env.out("  %-16s %d", "asked", h.Asked)
	env.out("  %-16s %d", "answered", h.Answered)
	env.out("  %-16s %d (%.0f%%)", "skipped", h.Skipped, h.SkipRate*100)
	env.out("  %-16s %s", "median time", h.MedianTime.Round(time.Second))
	env.out("")

	if h.DesignBroke {
		env.out("%s %s", env.bad("design problem:"), h.Note)
		env.out("%s", env.dim("The spec's own stopping rule: if the median answer time passes 30"))
		env.out("%s", env.dim("seconds, the probe design is wrong. That is this tool's problem, not yours."))
	} else {
		env.out("%s probes are landing in the intended range", env.good("healthy:"))
	}
	return nil
}
