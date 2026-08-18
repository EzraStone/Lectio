# Contributing

## Before anything else: the gate has failed

Gate A has been run at scale six times and failed every time. See
[docs/gate-a-2026-08.md](docs/gate-a-2026-08.md). That is not a disclaimer, it
is the most important context for any change you might make here:

- **Do not tune the weights against the corpus.** Every strategy that beat
  lectio beat it by being more size-correlated. Fitting to this metric produces
  a tool that recommends the biggest thing available, which is the failure mode
  the product exists to prevent. `DefaultWeights()` is unchanged from before all
  six runs and should stay that way until something other than Gate A justifies
  moving it.
- **Do not build phases 4 through 9.** The gate is a hard stop.
- Work that *is* wanted: a different definition of hidden coupling proposed
  before it is scored, a target that holds size constant by construction, or
  evidence that what people touched is too weak a stand-in for what they needed
  to understand.

## The rules that are easy to break by accident

### The LLM never grades

`probe.StemWriter` takes a symbol and returns a string. It cannot see
`Probe.Expected`, cannot modify it, and runs before grading exists. **Widening
that interface is the single change that would break the product's central
claim.** If you find yourself wanting the stem writer to know the answer, the
feature needs a different shape.

### Two call graphs, on purpose

`View.Calls` includes CHA-resolved edges and drives ranking. `View.StaticCalls`
holds only type-checker-proven edges and drives grading. Grading against
over-approximated edges marks correct answers wrong. They are not
interchangeable and a helper that picks "the call graph" is a bug waiting.

### Decide the measurement before you see the number

Several things here were deliberately fixed in advance, and the commits say so:
the size-reading rules, the symbol-level baseline translation, the
baseline/control split. Changing any of them after seeing a score turns a
failing gate into a passing one without a line of ranking code being wrong.

If a run disappoints and you want to adjust a threshold, that is exactly the
moment not to. Write down the new rule, say why it is better on its own terms,
and re-run everything under it.

### The matched-pair calibrations are part of the result

`TestSizeOnlyRankingScoresChance` and its three siblings are not test hygiene.
They are the only reason any number in the matched column can be believed.

A leak in the pairing does not produce implausible output. The first version
put every strategy between 28.8% and 46.8% — all below the 50% that a
size-matched pair makes chance by construction — and it reads like a finding
until you notice nothing can be below chance there. The cause was that twins
were consumed in ID order while every strategy breaks ties by ID.

If one of those four starts failing, the matched column is meaningless until it
passes again, and it will go on printing plausible numbers throughout. Do not
adjust the calibration to accommodate a change to the pairing; adjust the
pairing.

### Controls are not baselines

`Baselines()` decides the gate — the four the spec names. `Controls()` is
scored and reported and never gates. A control outscoring lectio is a finding,
not a failure, and a test asserts the gate still passes when it happens.

## Commits

Authored solely by Ezra Stone. No AI co-authorship trailers, session links, or
generated-by footers — see [CLAUDE.md](CLAUDE.md). Note that
`internal/vcs/ai.go` deliberately *detects* those markers; that is product code
reading other people's commits and is unrelated.

Messages here explain **why**, not what. The diff already says what changed.
The useful ones name the failure the change prevents, and several are the only
record of a bug that a test alone would not explain.

## Tests

```sh
make ci     # what CI checks: gofmt, vet, tests, build
```

Many tests are tripwires named after the failure they prevent rather than the
function they exercise — `TestHistoryWindowIsAnchoredAtAsOf`,
`TestZeroScoredSymbolsDoNotDragTheMeanDown`,
`TestCouplingParamsAnchorOnTheRepositoryNotTheClock`. **If one starts failing,
read the comment above it before changing the assertion.** Each is there
because the bug it describes already happened at least once.

The wall-clock trap has appeared three times in three different functions. If
you write anything that reads `time.Now()` inside analysis, assume it is the
fourth.

**Check a clone, not your working tree.** Unanchored `.gitignore` rules have
twice excluded real source — the main package and the corpus manifests — and
neither was visible locally, because the files were there and everything
worked. `make ci` runs against your tree; only `git clone` runs against the
repository.

## Running the gate

```sh
make corpus            # clone the thirty pinned repositories (slow, once)
make gate-a            # the gate, with per-signal ablation
make gate-a-symbols    # graded in declarations rather than file paths
make coupling          # the differentiator, measured directly
```

Build with a Go release at least as new as the corpus requires — the type
checker is compiled in, and an older binary silently drops packages. `lectio
version` prints which one you have.

Two numbers are only comparable when their **case set fingerprints match**. Which
cases survive depends on whether each rewound revision's dependencies resolved,
so the same command can score 85 cases one day and 77 the next.
