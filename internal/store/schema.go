package store

// schemaVersion is bumped whenever migrations are appended. It is stored in
// meta so an index built by an older binary is detected rather than
// misinterpreted.
const schemaVersion = 1

// migrations are applied in order, each exactly once. Never edit a migration
// that has shipped; append a new one.
//
// One layout decision runs through the whole schema. Index tables (symbols,
// call_edges, coverage) are disposable: they are dropped and rebuilt from
// source on every `lectio index`. State tables (engagement, probe_log) are
// not — they are the user's history, they never sync anywhere, and losing them
// loses something no re-index can reconstruct. They share a file for the
// reason the spec gives (no server until something forces one), but Reindex
// only ever truncates the first group.
var migrations = []string{
	`
CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) WITHOUT ROWID;

-- ---------------------------------------------------------------- index ----

CREATE TABLE files (
    id      INTEGER PRIMARY KEY,
    path    TEXT NOT NULL UNIQUE,
    loc     INTEGER NOT NULL DEFAULT 0,
    is_test INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE symbols (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL,
    package    TEXT NOT NULL,
    file_id    INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    start_line INTEGER NOT NULL DEFAULT 0,
    end_line   INTEGER NOT NULL DEFAULT 0,
    exported   INTEGER NOT NULL DEFAULT 0,
    doc        TEXT NOT NULL DEFAULT ''
) WITHOUT ROWID;

CREATE INDEX idx_symbols_file    ON symbols(file_id);
CREATE INDEX idx_symbols_package ON symbols(package);
CREATE INDEX idx_symbols_name    ON symbols(name);

CREATE TABLE call_edges (
    caller TEXT NOT NULL,
    callee TEXT NOT NULL,
    kind   TEXT NOT NULL,
    file   TEXT NOT NULL DEFAULT '',
    line   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (caller, callee, kind, file, line)
) WITHOUT ROWID;

CREATE INDEX idx_call_edges_callee ON call_edges(callee);
CREATE INDEX idx_call_edges_caller ON call_edges(caller);

CREATE TABLE import_edges (
    from_pkg TEXT NOT NULL,
    to_pkg   TEXT NOT NULL,
    PRIMARY KEY (from_pkg, to_pkg)
) WITHOUT ROWID;

CREATE TABLE coverage (
    test     TEXT NOT NULL,
    symbol   TEXT NOT NULL,
    fraction REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (test, symbol)
) WITHOUT ROWID;

CREATE INDEX idx_coverage_symbol ON coverage(symbol);

CREATE TABLE commits (
    hash         TEXT PRIMARY KEY,
    author_name  TEXT NOT NULL DEFAULT '',
    author_email TEXT NOT NULL DEFAULT '',
    ts           INTEGER NOT NULL,
    subject      TEXT NOT NULL DEFAULT '',
    is_fix       INTEGER NOT NULL DEFAULT 0,
    is_revert    INTEGER NOT NULL DEFAULT 0,
    ai_assisted  INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;

CREATE INDEX idx_commits_ts     ON commits(ts);
CREATE INDEX idx_commits_author ON commits(author_email);

-- commit_files keys on path, not on files.id, on purpose. History includes
-- files that were deleted or renamed away and no longer carry symbols;
-- forcing them through the files table would either lose that history or
-- pollute the current file set with ghosts.
CREATE TABLE commit_files (
    commit_hash TEXT NOT NULL REFERENCES commits(hash) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    added       INTEGER NOT NULL DEFAULT 0,
    deleted     INTEGER NOT NULL DEFAULT 0,
    renamed     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (commit_hash, path)
) WITHOUT ROWID;

CREATE INDEX idx_commit_files_path ON commit_files(path);

CREATE TABLE authorship (
    path        TEXT NOT NULL,
    author      TEXT NOT NULL,
    lines       INTEGER NOT NULL DEFAULT 0,
    last_active INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (path, author)
) WITHOUT ROWID;

-- ---------------------------------------------------------------- state ----
-- Per user, local only. Never dropped by a re-index, never synced anywhere.

CREATE TABLE engagement (
    symbol         TEXT PRIMARY KEY,
    edits          INTEGER NOT NULL DEFAULT 0,
    probes_seen    INTEGER NOT NULL DEFAULT 0,
    probes_correct INTEGER NOT NULL DEFAULT 0,
    probes_skipped INTEGER NOT NULL DEFAULT 0,
    first_seen     INTEGER NOT NULL DEFAULT 0,
    last_seen      INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;

CREATE TABLE probe_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol      TEXT NOT NULL,
    kind        TEXT NOT NULL,
    asked_at    INTEGER NOT NULL,
    answered_at INTEGER NOT NULL DEFAULT 0,
    outcome     TEXT NOT NULL,
    score       REAL NOT NULL DEFAULT 0,
    elapsed_ms  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_probe_log_symbol ON probe_log(symbol);
CREATE INDEX idx_probe_log_asked  ON probe_log(asked_at);
`,
}

// indexTables are truncated by Reindex. The state tables are conspicuously
// absent, and that is the point.
var indexTables = []string{
	"authorship",
	"commit_files",
	"commits",
	"coverage",
	"import_edges",
	"call_edges",
	"symbols",
	"files",
}
