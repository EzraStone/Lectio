# Documents

## [gate-a-2026-08.md](gate-a-2026-08.md)

The go/no-go, run at scale six times and failed every time. Read this before
anything else in the repository: it is the reason the weights have not moved,
the reason phases 4 through 9 are not built, and the context for every design
decision in the harness.

The short version: once file size is held constant the ranking is
indistinguishable from sorting by size; grading declarations instead of file
paths relocates that prior rather than removing it; and hidden coupling, the
signal the product is staked on, measures a lift of 1.02 over 2,311 newcomer
corrective commits.

## Screenshots

Every image in `images/` is a real terminal capture, including the ones that
say FAIL. None are mockups, and none have been edited after capture.

| Image | What it shows |
| --- | --- |
| `path.png` | A reading path for go-git, with a reason per item |
| `explain.png` | `--explain`, decomposing each score into its signals |
| `deps.png` | A blast-radius answer, with its provenance stated |
| `probe.png` | Lectio probing itself, graded against the call graph |
| `backtest.png` | Gate A at file granularity — the FAIL, with size bands |
| `coupling.png` | The differentiator measured directly: pooled lift 1.00 |
| `symbols.png` | Gate A at declaration granularity — a pass, undercut by a control |
| `matched.png` | The same run with size removed by construction: everything scores chance |

Three are worth looking at together. `backtest.png` and `symbols.png` are the
same gate at two granularities, and the second is the more interesting: lectio
beats all four baselines there, and the verdict is printed in warning colour
because a control more than doubled it.

`matched.png` is the same run again with one extra column, and it is the one to
read last. Precision and matched accuracy disagree completely — everything the
first rewards, the second removes — because the first is measuring size and the
second cannot.

## The two corpora

`corpus/gate-a.json` is the thirty repositories every number in the write-up
was computed over. `corpus/gate-a-holdout.json` is thirty different ones,
disjoint from it, and it exists for a single purpose: judging a hypothesis on
cases that did not produce it.

That property is spent by using it. Once a weighting has been tuned against the
holdout, the holdout is measuring its own training set. See
[CONTRIBUTING.md](../CONTRIBUTING.md) for the procedure.

## Elsewhere in the repository

- [ARCHITECTURE.md](../ARCHITECTURE.md) — how the pieces fit, what is
  load-bearing, and which changes need the most thought.
- [CONTRIBUTING.md](../CONTRIBUTING.md) — what not to do, first. Tuning the
  weights against this corpus is the most likely well-meant mistake.
- [CHANGELOG.md](../CHANGELOG.md) — organized by the question each change
  answered rather than by release.
