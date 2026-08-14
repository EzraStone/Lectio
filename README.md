# Lectio

**Codebase orientation for new hires.** Point it at a repo you just joined. It
tells you what to read, in what order, and why — and later, where your
understanding is actually thin.

> Status: **unvalidated**. Gate A has not been run. Every claim about ranking
> quality is a hypothesis until the backtest returns a number.

## The one load-bearing rule

**The LLM never grades.** It may write question stems and one-line rationales.
Every correctness judgment comes from the call graph, the test suite, or git
history. That trust property is what separates this from a chatbot quizzing
you, and it constrains the whole design.

## What it is not

Not a metric product. A comprehension score exists internally to drive the
reading path. It is never the headline, and individual scores never leave the
machine.

## Quick start

```sh
go build ./cmd/lectio

./lectio index  ~/src/some-go-repo          # build the index
./lectio path   ~/src/some-go-repo --top 10 # get a reading path
./lectio probe  ~/src/some-go-repo          # answer a blast-radius probe
```

## Layout

| Path                       | What lives there                                     |
| -------------------------- | ---------------------------------------------------- |
| `internal/adapter`         | `LanguageAdapter` interface — the language seam       |
| `internal/adapter/golang`  | Go implementation: symbols, call edges, coverage      |
| `internal/vcs/git`         | Commit log, blame, author activity, AI-authorship     |
| `internal/store`           | SQLite index + local-only user state                  |
| `internal/graph`           | PageRank, reachability, topological ordering          |
| `internal/rank`            | The seven signals, normalization, scoring, tiering    |
| `internal/probe`           | Probe generation and ground-truth grading             |
| `internal/backtest`        | Gate A: retroactive onboarding prediction             |
| `cmd/lectio`               | The single binary                                     |

## License

MIT
