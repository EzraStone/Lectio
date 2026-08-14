package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesSchema(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	var n int
	err := s.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN
         ('meta','files','symbols','call_edges','commits','engagement','probe_log')`).Scan(&n)
	if err != nil {
		t.Fatalf("query schema: %v", err)
	}
	if n != 7 {
		t.Errorf("found %d of 7 expected tables", n)
	}

	if v, err := s.Meta(ctx, "schema_version"); err != nil || v != "1" {
		t.Errorf("schema_version = %q (err %v), want \"1\"", v, err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.db")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		s, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		s.Close()
	}
}

func TestMetaRoundTrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if v, err := s.Meta(ctx, "absent"); err != nil || v != "" {
		t.Errorf("missing key returned %q, %v; want \"\", nil", v, err)
	}
	if err := s.SetMeta(ctx, "adapter", "go"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := s.SetMeta(ctx, "adapter", "go2"); err != nil {
		t.Fatalf("SetMeta overwrite: %v", err)
	}
	if v, _ := s.Meta(ctx, "adapter"); v != "go2" {
		t.Errorf("adapter = %q, want go2", v)
	}
}

// The one property the two-lifetimes design exists to guarantee.
func TestReindexPreservesUserState(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	mustExec(t, s.DB(), `INSERT INTO files(id, path) VALUES(1, 'a.go')`)
	mustExec(t, s.DB(), `INSERT INTO symbols(id,name,kind,package,file_id) VALUES('pkg.A','A','func','pkg',1)`)
	mustExec(t, s.DB(), `INSERT INTO engagement(symbol, edits) VALUES('pkg.A', 7)`)
	mustExec(t, s.DB(), `INSERT INTO probe_log(symbol,kind,asked_at,outcome) VALUES('pkg.A','blast',1,'correct')`)

	err := s.Reindex(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO files(id, path) VALUES(1, 'b.go')`)
		return err
	})
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	if got := count(t, s.DB(), "symbols"); got != 0 {
		t.Errorf("symbols survived re-index: %d rows", got)
	}
	if got := count(t, s.DB(), "engagement"); got != 1 {
		t.Errorf("engagement rows = %d, want 1 — user state must survive re-index", got)
	}
	if got := count(t, s.DB(), "probe_log"); got != 1 {
		t.Errorf("probe_log rows = %d, want 1 — user state must survive re-index", got)
	}

	var path string
	if err := s.DB().QueryRow(`SELECT path FROM files WHERE id=1`).Scan(&path); err != nil {
		t.Fatalf("read files: %v", err)
	}
	if path != "b.go" {
		t.Errorf("files.path = %q, want b.go", path)
	}
}

// A failed index must not leave a half-replaced one behind.
func TestReindexRollsBackOnError(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	mustExec(t, s.DB(), `INSERT INTO files(id, path) VALUES(1, 'original.go')`)

	wantErr := errFake
	err := s.Reindex(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO files(id, path) VALUES(2, 'partial.go')`); err != nil {
			return err
		}
		return wantErr
	})
	if err != wantErr {
		t.Fatalf("Reindex error = %v, want %v", err, wantErr)
	}

	if got := count(t, s.DB(), "files"); got != 1 {
		t.Errorf("files rows = %d after failed re-index, want the original 1", got)
	}
	var path string
	s.DB().QueryRow(`SELECT path FROM files`).Scan(&path)
	if path != "original.go" {
		t.Errorf("files.path = %q, want original.go", path)
	}
}

func TestDefaultPath(t *testing.T) {
	if got, want := DefaultPath("/repo"), "/repo/.lectio/index.db"; got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}

type fakeErr struct{}

func (fakeErr) Error() string { return "boom" }

var errFake = fakeErr{}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
