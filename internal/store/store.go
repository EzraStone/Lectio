// Package store persists the repository index and the local user state.
//
// Both live in one SQLite file beside the repo, per the spec: no backend until
// something forces one. The two halves have opposite lifetimes — the index is
// rebuilt from source whenever it goes stale, the state is the user's own
// history and is never rebuildable — so the split is enforced here rather than
// left to convention.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	_ "modernc.org/sqlite" // pure-Go driver; keeps the artifact a single static binary
)

// DirName is the directory created inside a repo to hold the index.
const DirName = ".lectio"

// FileName is the database file inside DirName.
const FileName = "index.db"

// Store is a handle on one repository's index and state.
type Store struct {
	db   *sql.DB
	path string
	root string
}

// DefaultPath returns the database location for a repo root.
func DefaultPath(root string) string {
	return filepath.Join(root, DirName, FileName)
}

// Open opens or creates the database at path and brings the schema up to date.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create index directory: %w", err)
	}

	// _txlock=immediate makes write transactions take the reserved lock up
	// front. Without it, a transaction that starts by reading and later writes
	// can fail to upgrade and returns SQLITE_BUSY partway through a batch
	// insert, which is a miserable failure mode to debug.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	// One connection. This is a single-user CLI writing batches of tens of
	// thousands of rows; concurrency buys nothing and costs lock contention
	// against ourselves.
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	s := &Store{db: db, path: path}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// OpenRepo opens the index for a repository root.
func OpenRepo(ctx context.Context, root string) (*Store, error) {
	s, err := Open(ctx, DefaultPath(root))
	if err != nil {
		return nil, err
	}
	s.root = root
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// Path returns the database file location.
func (s *Store) Path() string { return s.path }

// DB exposes the handle for packages that need their own queries.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate(ctx context.Context) error {
	var current int
	row := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'schema_version'`)
	var raw string
	switch err := row.Scan(&raw); {
	case err == nil:
		current, _ = strconv.Atoi(raw)
	case errors.Is(err, sql.ErrNoRows):
		current = 0
	default:
		// meta itself is missing: a fresh database.
		current = 0
	}

	if current > schemaVersion {
		return fmt.Errorf("index at %s was written by a newer lectio (schema %d, this binary understands %d); delete it and re-index",
			s.path, current, schemaVersion)
	}
	if current == schemaVersion {
		return nil
	}

	for v := current; v < len(migrations); v++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO meta(key, value) VALUES('schema_version', ?)
             ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			strconv.Itoa(v+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: record version: %w", v+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d: commit: %w", v+1, err)
		}
	}
	return nil
}

// SetMeta records a key/value pair in the index metadata.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta(key, value) VALUES(?, ?)
         ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Meta reads a metadata value; missing keys return "" with no error.
func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// Reindex clears every index table, leaving user state untouched, and runs fn
// inside the same transaction so a failed index never half-replaces a good one.
func (s *Store) Reindex(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, t := range indexTables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return fmt.Errorf("clear %s: %w", t, err)
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Vacuum reclaims space after a re-index drops a large previous generation.
func (s *Store) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "VACUUM")
	return err
}
