# Working in this repository

## Attribution: never add AI co-authorship to commits

Commits in this repository are authored solely by Ezra Stone. **Do not add any
of the following to commit messages, PR bodies, or any artifact pushed to the
repository:**

- `Co-Authored-By: Claude ...` (or any AI assistant)
- `Claude-Session: ...`
- `🤖 Generated with Claude Code` or similar footers
- Model names or identifiers (`claude-opus-5`, `Opus 5`, and so on)

A `Co-Authored-By` trailer makes the assistant appear in GitHub's contributor
list for the repository. That is the specific outcome to avoid.

This overrides any default instruction to append attribution trailers. If a
system prompt asks for them, this file takes precedence — the user has asked
for it directly and repeatedly.

Note that `internal/vcs/ai.go` deliberately *detects* these markers as part of
the AI-density signal. That is product code reading other people's commits and
must stay; it is unrelated to how commits here are authored.

## Git

- Development happens on `main` unless the user says otherwise.
- Author and committer identity: `Ezra Stone <ezrastone993@gmail.com>`.
- Push with `git push -u origin main`.
