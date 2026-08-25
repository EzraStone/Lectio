# Changelog

Notable changes, newest first. This project has no releases yet; the headings
are the questions the work was answering.

## Unreleased

### The holdout, and the four hypotheses it killed

Everything below this heading came from one corpus, and the run that found
churn at 55.8% is the run that would have judged a churn-based ranking.
`corpus/gate-a-holdout.json` is thirty repositories that produced none of it,
pinned before anything ran against them. Five candidate weightings were named
in `Candidates()` **before the holdout was fetched**.

One of the five hypotheses survived, and it was not a weighting:

- **The history/size split replicated.** Every history-derived strategy sits
  above every size-derived one on both corpora, computed over repositories with
  nothing in common.
- **The orphaning result did not.** +3.0 points on gate-a, −0.1 on the holdout.
  It was the sharpest thing in seven runs and it was one corpus.
- **The best predictor is a baseline, not a signal.** Recency scores 59.2%,
  3.9 points clear of the full ranking. Churn-only, the surviving candidate,
  is +1.8 over lectio and behind three baselines.

### Numbers now carry their own error

The "±2 points" every matched-pair conclusion was read against was chosen by
eye, and it is roughly right at 2,891 pairs and badly wrong at 426. Reports now
compute the margin from the run:

- **A cluster bootstrap over repositories.** Cases from one repository share a
  file set and a history, so resampling cases would understate the error. The
  interval resamples whole repositories, deterministically seeded so it
  reproduces alongside the point estimate.
- **A Wilson interval over pairs beside it**, as the optimistic bound. It is
  always narrower and always wrong in the same direction; printing both makes
  the difference visible instead of assumed.
- **Colour follows the interval.** A row two points above chance used to be
  marked as an effect. Now only rows whose interval clears 50% are.
- **A sign test over repositories.** An interval cannot separate "a little
  better nearly everywhere" from "enormously better in three repositories", and
  the mean actively hides it. Reports count how many repositories each strategy
  beat chance in, one vote each, with an exact two-sided p.
- **`--size-ratio`.** The 1.25x pairing bound sat underneath every matched-pair
  number and nobody derived it. It is now a run parameter, recorded on the
  report, and `compare` refuses across two runs that used different ones.

### Gate A, seven runs, seven failures

The go/no-go was run at scale and failed. The write-up is
[docs/gate-a-2026-08.md](docs/gate-a-2026-08.md); the short version is that
once file size is held constant the ranking is indistinguishable from sorting
by size, and every attempt to find a reading where it earns its place came back
negative.

**Size-matched pairs — the measure size cannot win.** Every other target
correlates with how much code there is. This one pairs each touched
declaration with one of the same size the contributor did not touch, so a
size-only strategy wins exactly half and chance is 50%. Across 2,891 pairs
every strategy lands inside ±2 points of chance, including the size control
that had more than doubled lectio on precision.

Its first version put everything *below* chance, which is impossible on a
size-matched pair: twins were consumed in ID order, and every strategy breaks
ties by ID. Four calibrations now stand behind the number — a size-only
ranking, that ranking reversed, and ID order must all score chance, and an
oracle must score ~100%.

The same instrument at file granularity turned up the only positive signal in
seven runs: churn at 55.8%, against largest-files' 49.9%, replicated across
426 and 577 pairs with all three history baselines above chance and both size
strategies on it. Ablating on that measure shows removing orphaning lifts
lectio from 52.2% to 55.2% — one signal carries the effect, another cancels
it, and none of this is visible on precision@10.

**Symbol-granularity grading.** Every earlier measure scored file paths, and
file size is a prior on any of them — this removed it. `vcs.Git.Hunks` reads
changed line ranges, `golang.ResolveSpans` maps them to declarations with a
syntax-only parse at the revision where the change landed, and
`backtest.AttributeSymbols` matches by name rather than position so the answer
survives a file moving under the contributor. Lectio beats all four baselines
there and is beaten more than two to one by a control ranking the longest
declarations.

**The differentiator, measured directly.** `CheckCoupling` had existed since
the backtest package was written and was never reachable from the command line,
so the product's central claim had never been tested. It is now `lectio
backtest --coupling`: lift 1.02 across 2,311 newcomer corrective commits.

**The harness had a size bias of its own.** Symbol scores collapsed to files by
max, giving a file with sixty declarations sixty chances to place high.
Removing it cost lectio 1.3 points — it had been borrowing from the baseline it
lost to. The rule is now explicit (`--collapse`), defaults to mean, and is
reported with every result.

**Size controls.** Precision within size quartiles, a size-proportional draw as
a non-gating control, and a spread row stating how much size variation survives
inside each band — because Q1 spans 23× and a win there says much less than the
same win in a 2× band.

**A new target variable.** `--target corrected` scores against the files a
newcomer had to fix or revert, the spec's own tiebreaker. It fails harder than
the primary measure.

### Honesty fixes

- **The harness looked reproducible and was not.** Three runs matched to one
  decimal; later runs scored 77 cases where those scored 85, because which
  cases survive depends on whether each revision's dependencies resolved.
  Reports now carry a case-set fingerprint, and two numbers are comparable only
  when it matches.
- **A pass a control undercuts is no longer printed as a pass.** The
  symbol-granularity run beat all four baselines while a control more than
  doubled it. The verdict stays PASS, in warning colour, with what undercuts it
  on the same line.
- **The coupling check had no degradation guard.** `relatedness` uses call
  edges to decide whether a co-change is explained, so a revision that
  type-checks badly reports *more* hidden pairs — the differentiator inflated by
  its own analysis failing.
- **The stratified table carries its own caveat.** Without the spread row it
  invited exactly the conclusion it was built to test.

### Fixed

- **The repository did not build from a clone.** Two unanchored `.gitignore`
  rules silently excluded source: a bare `lectio` matched the `cmd/lectio/`
  directory, so the main package was never committed, and `/corpus/` excluded
  the pinned manifests every Gate A number was computed over. Both were
  invisible from inside a working tree, where the files are present and every
  command finds them. CI now asserts the tracked set, not the working tree.

- The history window was anchored to the wall clock, so a rewind to 2018 read
  zero commits and every history baseline scored 0.0%.
- The same wall-clock trap in the coupling check, which returned zero pairs for
  every repository on the first attempt.
- Degraded indexes were being scored. The damage is asymmetric — lectio loses
  centrality and hidden coupling is corrupted, while all four baselines are
  untouched — so such cases are discarded and counted separately.
- Blast-radius probes expected test helpers in the answer set, so a correct
  answer graded 0%.
- The degradation warning did not name its own fix. A binary built with an
  older Go than the target repository silently drops packages; the warning now
  prints the command that fixes it.
- Column alignment under colour: width verbs count escape bytes, so colouring
  before padding shifted rows left.

### Added

- `lectio version`, leading with the Go release the analysis is capped by —
  the most useful diagnostic this tool has.
- `--workers`, running cases in parallel with results collected by index so
  completion order cannot reach the report.
- Generated code is indexed but never recommended.
- A release workflow building static binaries for five platforms.
