# Architecture

How the pieces fit, and why they are shaped this way. This is the document to
read before changing anything load-bearing.

## The dependency spine

```
                    ┌─────────────┐
                    │    core     │   domain model, no dependencies
                    └──────┬──────┘
             ┌─────────────┼─────────────┬──────────────┐
             ▼             ▼             ▼              ▼
        ┌─────────┐  ┌──────────┐  ┌─────────┐   ┌──────────┐
        │ adapter │  │   vcs    │  │  store  │   │  graph   │
        └────┬────┘  └────┬─────┘  └────┬────┘   └────┬─────┘
             │            │             │             │
        ┌────▼────┐       │             │             │
        │ adapter/│◄──────┘             │             │
        │ golang  │                     │             │
        └────┬────┘                     │             │
             └──────────┬───────────────┘             │
                        ▼                             │
                  ┌───────────┐                       │
                  │   index   │◄──────────────────────┘
                  └─────┬─────┘
             ┌──────────┼──────────┐
             ▼          ▼          ▼
         ┌──────┐  ┌───────┐  ┌──────────┐
         │ rank │  │ probe │  │ backtest │
         └───┬──┘  └───┬───┘  └────┬─────┘
             └─────────┼───────────┘
                       ▼
                    ┌─────┐
                    │ cli │
                    └─────┘
```

Nothing points back up. `core` knows about nothing; `rank` knows nothing about
Go, git, or SQL. Changing how symbols are extracted cannot break how they are
ranked, and that separation is what makes "is our ground truth correct?"
answerable independently of "is our ranking any good?".

## The three constraints everything else follows from

### 1. Nothing grades by judgment

Every correctness decision comes from the call graph, the test suite, or git
history. Two mechanisms enforce it rather than merely documenting it.

**`probe.StemWriter`** is the only place a language model can be wired in:

```go
type StemWriter interface {
    Stem(ctx context.Context, kind Kind, subject core.Symbol) (string, error)
}
```

It receives a symbol and returns a string. It never sees `Probe.Expected`,
cannot modify it, and is called before grading exists. Widening this interface
so a stem writer can see or shape the answer set is the single change that
would break the product's central claim.

**Two call graphs** carry the same discipline through ranking:

| Graph | Contains | Drives |
| --- | --- | --- |
| `View.Calls` | Static + CHA-resolved dynamic edges | Ranking |
| `View.StaticCalls` | Type-checker-proven edges only | Grading |

CHA is sound and imprecise: an interface with four implementations yields four
edges where execution takes one. That over-approximation is *useful* for
ranking — whoever maintains an interface has to understand every implementation
behind it — and *fatal* for grading, where it would mark a correct answer wrong.

### 2. Index and state have opposite lifetimes

Both live in one SQLite file (`.lectio/index.db`), per the spec's "no backend
until something forces one". But:

- **Index tables** are disposable, rebuilt from source on every run.
- **State tables** (`engagement`, `probe_log`) are the user's own history, never
  reconstructable, never synced anywhere.

`store.Reindex` truncates only the first group and runs the caller's work in
the same transaction, so a failed index cannot half-replace a good one. Both
properties have tests, because silently wiping weeks of someone's probe history
on a routine re-index is unrecoverable and would be noticed far too late.

### 3. Results are reproducible

Every ordering breaks ties deterministically. Every time-dependent signal reads
`Params.Now` rather than the wall clock. `adapter.Options.AsOf` anchors the
history window, which is what lets the backtest stand at an arbitrary date.

A reading path that reshuffles between runs is not a path anyone trusts, and a
backtest whose number moves when you run it again is not evidence.

## Data flow

```
repo ──► LanguageAdapter ──► core types ──► store ──► index.View ──► rank ──► CLI
              │                                           │
              │ Symbols                                   ├──► probe
              │ CallEdges      ┌── vcs.History            └──► backtest
              │ TestCoverage ──┘
              └ FileHistory
```

`index.Build` is the only place that knows about both the adapter seam and the
store. `index.Load` produces the `View` that everything above consumes; nothing
downstream touches SQL.

## The language seam

```go
type LanguageAdapter interface {
    Name() string
    Detect(root string) (ok bool, confidence float64)
    Symbols(ctx, root) ([]core.Symbol, error)
    CallEdges(ctx, root) ([]core.CallEdge, []core.ImportEdge, error)
    TestCoverage(ctx, root) ([]core.CoverageEdge, error)
    FileHistory(ctx, root, since) ([]core.Commit, error)
}
```

Four extraction methods, and every one returns facts rather than judgments. No
adapter decides what is worth reading, and no adapter grades an answer.

Adding a language is one implementation of this interface plus an `init()` that
registers it. Ranking, probes, ordering and state are all language-agnostic and
sit above the seam — the Go-first bet costs one adapter, not the product.

### Why Go first

Not the biggest market. But v1 hinges on ground-truth grading, and ground truth
means the call graph has to be right:

- `golang.org/x/tools/go/callgraph` is first-party and maintained.
- Static types and explicit imports; no DI container rewiring dispatch at runtime.
- `go test -coverprofile` gives the test-to-function mapping for free.

The alternatives fail on that specific axis. Python's dynamic dispatch makes
static call graphs unreliable enough to undermine the premise. Tree-sitter
gives syntax without semantics, so you get name references rather than resolved
calls. Java is analyzable until Spring, which is present in exactly the
codebases worth testing.

## Call graph construction

Two passes, in `internal/adapter/golang/callgraph.go`:

1. **Static** — walks the AST with `go/types`, resolving each `*ast.CallExpr` to
   a `*types.Func`. Every edge is one the type checker proved. Interface method
   calls are explicitly *not* resolved here.
2. **Dynamic** — builds SSA for the repository's own packages
   (`ssautil.Packages`, not `AllPackages`) and runs CHA. Only `IsInvoke()` call
   sites are recorded, marked `EdgeDynamic`.

Details that matter:

- Closures are attributed to the enclosing named function. A closure is not
  something you can open on its own, and separate anonymous nodes would
  fragment both the graph and anyone's probe history.
- Generic functions normalize to their origin, so `Stack[int]` and
  `Stack[string]` are one symbol. Otherwise the symbol set changes whenever a
  caller changes, and per-user history silently detaches from the code it was
  about.
- Edges leaving the repository are dropped. A reading path should not send
  someone to `strconv.Atoi`, and stdlib nodes would swamp centrality.
- A panic in SSA construction degrades to zero dynamic edges rather than killing
  the index — it costs precision on a signal that grades nothing.

## Ranking

`Rank()` computes seven signals, normalizes each, and combines them by weight.

**Percentile normalization, not min-max.** Every distribution here is
heavy-tailed: one utility has four hundred dependents while the median has two.
Min-max maps that median to 0.005 and turns a seven-signal ranking into a
one-hot indicator for whichever symbol tops each distribution. Percentile rank
keeps the ordering, spreads the mass, and makes a weight of 0.2 contribute at
most 0.2. Zeros stay at zero, because ranking the zeros of a sparse signal
manufactures a gradient out of an absence of evidence.

**Weights renormalize over signals that fired.** Otherwise a repo with no git
history scores everything near zero — the ordering survives but the numbers
become meaningless, and they are shown to people.

**Silent signals are reported, not counted as zero.** "We found no AI markers"
and "this code was not written by a machine" are different claims.

The weights in `DefaultWeights()` are a hypothesis. Nothing has been fit to
anything. Whatever Gate A says should replace them.

## Where to be careful

Changes to these are the ones that need the most thought:

| Area | Why |
| --- | --- |
| `probe.StemWriter`'s signature | Widening it breaks the central trust claim |
| `View.StaticCalls` usage | Grading against over-approximated edges marks correct answers wrong |
| `store.indexTables` | Adding a state table here wipes user history on re-index |
| `core.SymbolID` format | It is the join key between a disposable index and non-disposable state |
| Edge orientation (caller → callee) | Reversing it silently inverts centrality and blast radius |
| `HiddenCoupling` guards | Removing the commit-size cap lets reformats become the entire signal |
| `Params.Now` / `Options.AsOf` | Wall-clock reads make backtests report numbers they cannot repeat |
| `backtest.Collapse` | The symbol-to-file rule is a size prior; max quietly reinstates the bug Gate A was measuring |
| `SymbolBaselines` translation | Restating it after seeing a score is how the gate gets argued past |
| `Baselines()` vs `Controls()` | A control that could fail the gate is a fifth baseline added after the fact |

Several of these have tests written specifically as tripwires — a test named
after the failure it prevents rather than the function it exercises. Those are
deliberate; if one starts failing, read the comment above it before changing
the assertion.

## Development order

Following the spec's phases:

| Phase | Status |
| --- | --- |
| 0 · Adapter interface and Go index | Built |
| 1 · Ranking, all seven signals | Built |
| **Gate A · Beat four baselines** | **Run at scale six times. Failed all six.** |
| 2 · Reading path CLI | Built |
| 3 · Probe engine | Built |
| Gate B · Week-two return rate | Needs users |
| 4 · Coverage state and progress view | Partial (familiarity feeds ranking) |
| 5 · Task scoping | Built (`--task`) |
| 6 · VS Code extension | Not started |
| 7 · TypeScript adapter | Not started |
| 8 · GitHub App | Not started |
| 9 · Team aggregate view | Not started |

Gate A is a hard stop, and it has now returned its number six times: with the
harness's own size bias removed, against a size-controlled metric, against the
spec's tiebreaker target, with the differentiator measured directly, and twice
graded in declarations rather than file paths. It failed all six. See
[docs/gate-a-2026-08.md](docs/gate-a-2026-08.md). Nothing below the gate should
be built on the current ranking.

## Running Gate A

```sh
make corpus            # clone corpus/gate-a.json, pinned (slow, once)
make gate-a            # backtest with per-signal ablation
make gate-a-corrected  # graded against files newcomers had to fix or revert
make gate-a-symbols    # graded in declarations rather than file paths
make coupling          # the second backtest, as lift rather than precision
```

Three properties of the harness are worth knowing before trusting its output.

**Degraded cases are discarded, not scored.** When a rewound revision fails to
type-check, the damage lands on one side only: lectio loses centrality (a
failed go-git load gave 19,005 call edges where a clean one gave 36,615) and
hidden coupling is corrupted rather than weakened, because `relatedness` uses
call edges to decide whether a co-change is already explained. All four
baselines are untouched — three are pure git, and largest-files reads symbol
counts, which survive. Scoring such a case biases the gate toward abandoning a
ranking that might be fine, so anything above `MaxDegraded` is thrown out and
counted separately in the report.

**Dependencies are resolved per revision first**, so most degradation is
repaired rather than merely detected. Failure there is ignored; the health
check decides what is still worth scoring.

**Ablation is free.** Variants score against the same index, so an eight-way
leave-one-out costs what a plain run costs. Only the four baselines decide the
verdict — a variant outscoring the default is the diagnosis the ablation exists
to produce, not a gate failure.

### The symbol-to-file collapse

The ranker works in symbols; Gate A is scored in files. The rule bridging them
is a size prior, and it is not optional — something has to be chosen.

| Rule | Effect |
| --- | --- |
| `max` | A file scores as its best symbol. Sixty symbols means sixty chances to place high. |
| `mean` | A file scores as its typical symbol. Adding an unremarkable one lowers the score. |
| `sum` | Size-weighting stated outright. |

The first run at scale used `max`, implicitly, by walking the score-sorted
symbol list and taking each file at its first appearance. Across five corpus
repositories that put lectio's top ten at 3.5× the repository's median symbol
count, overlapping the largest-files baseline 6/10 — so the harness was scoring
a partial reimplementation of the baseline lectio lost to.

`mean` is the default because it is the only one of the three that adds no size
prior of its own, not because it is obviously right. On repositories whose
signals are themselves size-correlated it barely moves the ranking, and that is
the point: what survives it is a fact about the signals rather than about the
harness. `Report.Collapse` carries the rule up from the cases that ran, so a
number cannot be quoted without it.

### Controls are not baselines

`Baselines()` returns the four the spec names, and beating all four is the gate.
`Controls()` returns strategies that are scored and reported but never decide
it. The size-proportional draw is a control: it exists to explain a result, and
adding a fifth strategy to the pass condition after seeing the score would be
moving the goalposts. `decide()` reads only `Baselines()`, and a test asserts a
control outscoring lectio still passes.

### Symbol granularity

Every file-path measure carries file size as a prior. Grading declarations
removes it, and it is also what the ranker natively produces — collapsing to
files was only ever the harness's requirement.

Three pieces make it work, and the middle one is the interesting choice:

1. `vcs.Git.Hunks` reads changed line ranges from `git diff -U0`, in
   post-commit coordinates.
2. `golang.ResolveSpans` parses the changed file *at the revision where the
   change landed* — syntax only, no type checking — and maps each range to its
   enclosing declaration. Microseconds per file, and it works on revisions that
   do not build, which is most of a historical corpus.
3. `backtest.AttributeSymbols` matches those declaration **names** against the
   symbol table.

Names rather than positions, because by the time a newcomer commits, the file
has moved under them — often hundreds of lines. A regression test puts `Parse`
at lines 3–5 in the index and 103–105 at commit time; any position-based match
attributes that change to whatever now occupies lines 3–5. The known undercount
is symbol renames, which biases toward reporting less evidence than exists.

**The baseline translation was fixed before any symbol-level number existed.**
Each file baseline inherits its own signal: "the biggest files" becomes "the
symbols in the biggest files, in source order" — what the file-level baseline
actually recommended — not "the biggest symbols". Choosing that afterwards is
how a gate gets argued past. `LargestSymbols` exists as a *control* for exactly
that reason, and it is what beat the ranking two to one.

### Size-stratified precision

The overall table cannot tell "chose better files" from "chose bigger files".
The stratified table splits the same cases into size quartiles — by rank, not by
value, since the distributions are heavy-tailed enough that equal-width bands
would be a histogram of the tail — and scores each band at its own cutoff, at
most half the band. A wider cutoff stops measuring which files were chosen and
starts measuring how many exist, because any ten of twelve files cover the same
ground.

`ReadStrata` names the pattern that decides the fork. Largest-files ahead
overall while behind in every band is Simpson's paradox: the ground truth is
itself size-weighted, so overall precision scores the composition of a top ten
rather than the choices in it. Largest-files ahead band by band means the
opposite, and is reported in the same words — a check that can only exonerate
the ranking is not a check.
