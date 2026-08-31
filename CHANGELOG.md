# Changelog

Notable changes, newest first. This project has no releases yet; the headings
are the questions the work was answering.

## Unreleased

### Seven product bugs, found by running the tool on itself

The measurement side has had fourteen runs of scrutiny. The commands people
actually run had not, and pointing `lectio path` at this repository turned up
three defects in a row.

- **A reading path of ten from one file.** All ten items came from
  `internal/cli/backtest.go` with the same reason on each. The cause is
  structural rather than a bug in any signal: four of the seven are computed
  per *file*, so every declaration in a heavily-worked file inherits four
  identical contributions and its symbols take the whole list on merit.
  `Path` now prefers not to draw more than a third of the list from one file —
  a preference, not a limit, so a repository whose interesting code genuinely
  is one file still gets a full list. `probe` had the same problem for a
  sharper reason: a quiz confined to one file measures familiarity with one
  file.

  This is a selection rule, not a scoring one. No weight moved, and the
  backtest reads `Result.Items` directly — re-running the declaration target
  reproduced case set `0d1e81666c55` with lectio at 10.53% and 49.52%,
  identical to run 11 to the decimal.

  The cap's floor was then raised from one to two per file, because at
  `--top 3` a third of the list rounds to one and forbids any two items from
  sharing a file — and "these two functions here, plus this one over there" is
  a perfectly good three-item path.

- **A flag after the path was silently ignored.** Go's flag package stops
  parsing at the first non-flag argument, so `lectio path . --top 3` returned
  ten items with nothing to say why. It is the one CLI mistake here that
  produced a wrong answer rather than an error.

- **"No data" was printed for signals that ran and found nothing.** The
  reporting's own comment says "we found no AI markers" and "this code is not
  AI-written" are different claims, and then printed the same line for both.
  A signal can now say whether the index carried its input, and the two
  silences render differently.

- **The read-only graph accessors panicked on a nil receiver.** `index.View`
  is a plain struct assembled by hand in tests and fixtures, and `rank.Rank`
  on one with no call graph crashed the process from `HiddenCoupling.Compute`.
  `Reverse` and `Subgraph` had the same problem, reached from `rank.Gather`.

- **The churn rationale said "this year" whatever `--months` was set to.**
  `lectio path --months 3` described a three-month count as a year's, in a tool
  whose stated premise is that every claim it makes is auditable. `Facts` now
  carries the phrase, derived from the same effective window the counts are
  taken over so the two cannot drift.

- **Non-positive counts were accepted and did something.** `--top 0` reported
  "Nothing scored above zero. That usually means the index is thin", sending
  someone to re-index a repository that was fine; `probe -n 0` exited silently
  with no output and no error; `deps --depth -1` walked a bounded depth it
  never stated. Zero depth legitimately means "follow every hop", so `deps`
  gets the guard its own semantics call for rather than the one the others get.

- **The `path --json` output still conflated the two silences** the text output
  had learned to separate, and said nothing about how many files the path
  spanned. Both are there now; `silent_signals` stays as the union so a
  consumer written against the older shape reads the same thing.

### The index summary shows what would otherwise fail silently

- **Import edges are printed, including when they are zero.** They are what
  decides whether two files changing together is already explained by a
  dependency, and hidden coupling is defined as the co-changes that
  explanation does not cover — so zero does not mean "no imports found", it
  means every co-change in the repository will read as hidden.
- Warnings for the three ways an index builds successfully and is still
  missing something a later command depends on: no import edges, a heavy
  type-check failure, no history at all.

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

### The differentiator's null has an error bar, and it is tight

"Lift 1.02, no relationship" was the most-quoted number in this project and the
only one that never carried one. *No relationship* and *not enough evidence to
see one* produce the same 1.02, so for ten runs this was a refutation resting on
a number that could equally have been a shrug.

- **Lift 1.02, 95% interval 0.98 to 1.07**, bootstrapped over the 24
  repositories that cleared both sample-size guards. A lift of 1.07 or more
  would have landed outside it, so the result is *no effect above seven
  percent* rather than *no effect we could see*.
- **`CouplingPower`** answers the question a null result has to and almost
  never does: what is the smallest departure from no-relationship this corpus
  could have detected? Printed whenever the interval contains 1.0.
- **Colour follows the interval.** A pooled lift of 1.6 on an interval running
  down to 0.8 is not a positive result, and this project has already made that
  mistake three times in a different table.
- A zero-width interval is an answer, not a refusal: a corpus whose
  repositories all agree has no between-repository variance, and that is the
  tightest result a run can produce.

### The one lead closed, and the correction that would have flagged it

Churn-only won 12 of 15 repositories on the holdout at p = 0.035 — the first
sign test in the project under 0.05. Taken back to the first corpus it is
**9 of 15, p = 0.607.**

- **Reports correct for having run ten tests.** Holm–Bonferroni across every
  strategy a report scores, since a table of ten produces about one nominal
  0.05 by construction. Holm rather than Bonferroni: uniformly more powerful,
  and neither assumes independence, which matters because these strategies are
  scored on identical cases.
- **The nominal-only row is marked, not promoted and not hidden.** A row that
  clears 0.05 on its own p and not after correction is the one a reader would
  otherwise quote, and it has now twice been the one that failed.
- **A replay recomputes the correction**, so reports written before it existed
  get it on being re-read. The family follows the restriction: a strategy
  absent from a subset is one fewer test.
- **Run 10's claim is corrected.** "Four strategies clear chance at the tight
  end, and that is the split surviving a second corpus" conflated a direction
  with a significance. The direction replicates on both corpora at every bound;
  the four rows clearing a threshold do not — gate-a has none at 1.10x.
- **The file-level measure has a structural ceiling.** Seventeen repositories
  produce pairs at five cases per repository and seventeen at twelve. More
  cases add pairs to repositories already counted, and both the bootstrap and
  the sign test resample repositories.

### What the re-render found

Run 10's report replayed through the current analysis, no corpus touched.

- **The binding limit on file-level coverage is the `MinPairs` floor, not the
  size bound.** Of the 33 cases the pairing did not score, 28 built pairs and
  fell below the floor and 5 found no twin at all. Loosening from 1.25x to
  2.00x buys six cases and eight points of leakage on a size control, so the
  obvious fix is the wrong one.
- **Churn-only clears the repository sign test at p = 0.035** — 12 of 15
  repositories, the first strategy in thirteen runs to come in under 0.05. It
  is one of ten strategies scored on the same data, where about one nominal
  0.05 is expected, and it does not clear a corrected threshold.

### Saved reports can be re-read

`lectio backtest --replay report.json` recomputes a stored report's aggregates
with the current code and re-renders it, touching no corpus. Every analysis
added this week — the intervals, the sign test, the leak check — cost a
forty-minute pass over a corpus to add a column to numbers already on disk.

- **A replay is a fixed point.** Everything derivable from the per-case scores
  is recomputed; everything describing the same population — failure counts,
  coverage, strata, the sweep — is carried, because a replay is not a subset.
- **The verdict is re-derived, not copied.** It is a reading of the aggregates,
  and the aggregates have just been recomputed.
- **A file whose per-case scores disagree with its header is refused.** The
  fingerprint is over the same case IDs both times, so a mismatch means the
  file was edited or truncated.
- Two things a restriction was quietly losing: recall and MRR were not carried
  per case, so a replay printed two columns of zeroes; and strategy order came
  from the sort rather than from the table being rebuilt.

### The declaration measure, doubled

Every declaration-level number came from 75 cases at five per repository. Run
12 is the same command at twelve — 142 cases, 3,823 pairs from 115, across 25
repositories.

- **Not one interval excludes chance, and not one sign test falls below
  p = 0.42.** The intervals narrow as they should and nothing crosses out of
  chance as they do, which is what a result hiding under the noise at 75 cases
  would have done.
- **The narrower run is a strict subset, and every shared case scores
  identically to the decimal.** Case selection is deterministic, a case's score
  does not depend on what else was in the run, and restricting the wider run to
  the shared cases reproduces the narrower one exactly.
- **`compare` distinguishes nested case sets from overlapping ones.** The
  write-up had been calling `--cases 5` against `--cases 12` "a larger sample
  rather than an independent one" for four runs without being able to check it.

### Coverage says which limit stopped the pairing

"33 of 77 cases" invites the reading that the corpus is too small, which is only
sometimes true and is the more expensive of the two things to fix. Reports now
split it: cases that built pairs but fell below `MinPairs` are a thin corpus,
cases whose touched files found no size-matched twin at all are a tight bound,
and `--size-ratio` moves only the second. The line also states coverage in
touched items rather than only in cases, which is the sharper denominator.

### Runs now check their own pairing

Run 10 found the pairing bound leaking by hand — sweeping the ratio and reading
a column. That check is now automatic.

- **`PairingLeak`** fires when a strategy whose entire input is how much code
  there is beats chance on size-matched pairs. It cannot, by construction, so
  the pairing is letting size through. One-sided: below chance is a different
  failure with different causes, and the calibrations already cover it.
- **`SweepLeak`** does better than warn. When a sweep ran, the report names the
  *loosest* ratio in the ladder at which no size control clears chance, and
  tells you the flag to re-read the table there.
- The warning prints after the tables, because it is a statement about how to
  read them rather than a reason to discard them: precision is unaffected and
  the ordering usually survives, but every size-correlated row is inflated by a
  couple of points — enough to turn a null into a finding.

### The sweep at declaration granularity, where the bound does nothing

Run 10 found the pairing bound leaking two points at file granularity. Run 11
asked the same question of the declaration targets, where run 7's "everything
is chance" conclusion had been computed at the same unexamined 1.25x.

- **No row moves more than a point across the whole ladder**, and coverage
  barely changes: tightening from 2.00x to 1.00x costs 18% of the pairs here
  against 81% at file level. Declarations are dense in size space and files are
  not, so the bound is doing all the work at one granularity and almost none at
  the other.
- **Nothing clears chance at any ratio.** The exact-match column is the
  strongest measurement the project has produced: 2,496 pairs between
  declarations of identical length, across 24 repositories, every strategy
  between 47.6% and 51.4%.
- **The two granularities do not deserve equal confidence.** The file-level
  numbers are sensitive to a constant nobody derived and to a case set that
  moves between runs; the declaration-level numbers are sensitive to neither.
- **The case-set instability was a file-target property, not a harness one.**
  The declaration targets have produced fingerprint `0d1e81666c55` on all three
  runs that used them, weeks apart and across a binary change. The write-up had
  generalized the drift to the whole harness since run 1.

### Two runs can now be compared even when they scored different cases

`compare` refused across two case sets, which was correct and a dead end: after
ten runs the same command had never twice produced the same one, so the tool
that exists to diff two runs could rarely diff any two runs anyone had.

- **Reports carry per-case numbers.** Every strategy's precision, matched
  accuracy and pair count on every scored case, keyed by repository, rewind
  point and contributor. It also makes a report re-analyzable without
  re-running the corpus, which is worth the kilobytes on its own.
- **Different case sets are intersected, not refused.** The comparison is
  rebuilt over the cases both runs reached, and says so above the table since
  its rows then match the totals in neither input file. Below twenty shared
  cases it still refuses.
- **A restriction drops what it cannot recompute.** Strata, the sweep and the
  verdict do not survive being restricted to a subset — a verdict computed over
  161 cases carried onto 74 would look exactly like a real claim about those 74.
- **`compare --cases`** lists which cases each run reached alone, grouped by
  repository and heaviest first, so drift concentrated in two repositories'
  dependencies reads differently from drift spread across the corpus.
- **`compare --json` emitted Go field names.** The `Comparison` type had no
  JSON tags.

### The pairing bound was leaking, and the sweep caught it

`--sweep-ratio` runs the matched column at every ratio in one pass. The first
sweep answered a question run 8 had left open and found a bug in the interval
code that had landed the same day.

- **Largest-files walks 50.5% → 52.9% → 54.0% → 58.7%** as the bound loosens
  from 1.10x to 2.00x. A size strategy converting a widening size gap into
  accuracy is exactly what should happen, and it means every matched-pair
  number computed at 1.25x is about two points generous to whatever correlates
  with size.
- **At 1.10x the history/size split is sharper, not weaker.** Four strategies
  clear chance and all four are history-derived; both size strategies sit on
  it. That is the one finding that has now survived a second corpus and a
  change of instrument.
- **`BootstrapInterval` refuses below eight repositories.** The exact-match
  column reached four and reported a size strategy at 60.3% as clear of chance,
  on pairs where it cannot know anything. A percentile bootstrap over four
  clusters draws from 35 distinct resamples; the interval is arbitrary rather
  than wide, and it can come out narrow.

### The error bars changed a conclusion

With the intervals in place, both corpora were re-run.

- **The orphaning result was never real.** +1.9 points on gate-a, not +3.0,
  with intervals overlapping along almost their whole length; −0.1 on the
  holdout. The holdout caught it because a second corpus was cheap to fetch.
  The interval would have caught it without one.
- **Exactly one row's interval clears chance**, and its repositories disagree.
  Churn scores 55.9% with an interval of 50.4 – 61.1 and a sign test of 9 of 14
  repositories at p = 0.42. The bootstrap weights a repository by how many
  cases it produced; the sign test gives each one vote. When they disagree the
  sign test is the reading to take.
- **The margin is roughly ±5 points at file granularity, not ±2.** The old
  figure was measured on precision and does not transfer: a matched-pair
  accuracy averages over as few as five pairs per case, and the file-level
  pairing reaches only about a third of the attempted cases.
- **`--sweep-ratio`** runs the matched column at 1.0x, 1.1x, 1.25x, 1.5x and
  2.0x in a single pass. Indexing is the expensive half and does not depend on
  the ratio, so the whole ladder costs one run plus rounding.
- **`Aggregate.Wins` is gone.** Nothing had ever written it, so every JSON
  report published so far carried `"wins": 0` for every strategy — an absent
  measurement that read as a real one.

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

*(Runs 8 through 10, above, added a second corpus and error bars. The "±2
points" in this section is the eyeballed figure they replaced.)*

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
