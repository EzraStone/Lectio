package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/EzraStone/Lectio/internal/core"
)

// IndexWriter accumulates one generation of the index inside a single
// transaction. Callers get one from WriteIndex and must not retain it.
//
// Everything is prepared once and executed many times: a mid-size Go service
// produces on the order of 10^5 call edges, and the difference between
// prepared and re-parsed statements there is the difference between a few
// seconds and a coffee break.
type IndexWriter struct {
	ctx     context.Context
	tx      *sql.Tx
	fileIDs map[string]int64

	insFile      *sql.Stmt
	insSymbol    *sql.Stmt
	insCall      *sql.Stmt
	insImport    *sql.Stmt
	insCoverage  *sql.Stmt
	insCommit    *sql.Stmt
	insCommitFil *sql.Stmt
	insAuthor    *sql.Stmt
}

// WriteIndex clears the previous index generation and hands fn a writer.
// User state is untouched; a returned error rolls the whole thing back.
func (s *Store) WriteIndex(ctx context.Context, fn func(*IndexWriter) error) error {
	return s.Reindex(ctx, func(tx *sql.Tx) error {
		w, err := newIndexWriter(ctx, tx)
		if err != nil {
			return err
		}
		defer w.close()
		return fn(w)
	})
}

func newIndexWriter(ctx context.Context, tx *sql.Tx) (*IndexWriter, error) {
	w := &IndexWriter{ctx: ctx, tx: tx, fileIDs: make(map[string]int64, 1024)}

	stmts := []struct {
		dst **sql.Stmt
		q   string
	}{
		{&w.insFile, `INSERT INTO files(path, loc, is_test) VALUES(?,?,?)
                      ON CONFLICT(path) DO UPDATE SET loc=excluded.loc, is_test=excluded.is_test`},
		{&w.insSymbol, `INSERT INTO symbols(id,name,kind,package,file_id,start_line,end_line,exported,doc)
                        VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`},
		{&w.insCall, `INSERT INTO call_edges(caller,callee,kind,file,line) VALUES(?,?,?,?,?)
                      ON CONFLICT DO NOTHING`},
		{&w.insImport, `INSERT INTO import_edges(from_pkg,to_pkg) VALUES(?,?) ON CONFLICT DO NOTHING`},
		{&w.insCoverage, `INSERT INTO coverage(test,symbol,fraction) VALUES(?,?,?)
                          ON CONFLICT(test,symbol) DO UPDATE SET fraction=max(fraction,excluded.fraction)`},
		{&w.insCommit, `INSERT INTO commits(hash,author_name,author_email,ts,subject,is_fix,is_revert,ai_assisted)
                        VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(hash) DO NOTHING`},
		{&w.insCommitFil, `INSERT INTO commit_files(commit_hash,path,added,deleted,renamed)
                           VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`},
		{&w.insAuthor, `INSERT INTO authorship(path,author,lines,last_active) VALUES(?,?,?,?)
                        ON CONFLICT(path,author) DO UPDATE SET
                          lines = lines + excluded.lines,
                          last_active = max(last_active, excluded.last_active)`},
	}
	for _, s := range stmts {
		stmt, err := tx.PrepareContext(ctx, s.q)
		if err != nil {
			w.close()
			return nil, fmt.Errorf("prepare %q: %w", s.q, err)
		}
		*s.dst = stmt
	}
	return w, nil
}

func (w *IndexWriter) close() {
	for _, s := range []*sql.Stmt{
		w.insFile, w.insSymbol, w.insCall, w.insImport,
		w.insCoverage, w.insCommit, w.insCommitFil, w.insAuthor,
	} {
		if s != nil {
			s.Close()
		}
	}
}

// File registers a source file and returns its id, memoized per writer.
func (w *IndexWriter) File(path string, loc int) (int64, error) {
	if id, ok := w.fileIDs[path]; ok {
		return id, nil
	}
	res, err := w.insFile.ExecContext(w.ctx, path, loc, boolInt(core.IsTestFile(path)))
	if err != nil {
		return 0, fmt.Errorf("insert file %s: %w", path, err)
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		// An upsert that took the DO UPDATE branch reports no insert id.
		if err := w.tx.QueryRowContext(w.ctx, `SELECT id FROM files WHERE path = ?`, path).Scan(&id); err != nil {
			return 0, fmt.Errorf("resolve file id for %s: %w", path, err)
		}
	}
	w.fileIDs[path] = id
	return id, nil
}

// Symbol writes one declaration, registering its file as a side effect.
func (w *IndexWriter) Symbol(s core.Symbol) error {
	fileID, err := w.File(s.File, 0)
	if err != nil {
		return err
	}
	_, err = w.insSymbol.ExecContext(w.ctx,
		string(s.ID), s.Name, string(s.Kind), s.Package, fileID,
		s.StartLine, s.EndLine, boolInt(s.Exported), s.Doc)
	if err != nil {
		return fmt.Errorf("insert symbol %s: %w", s.ID, err)
	}
	return nil
}

// CallEdge writes one dependency edge.
func (w *IndexWriter) CallEdge(e core.CallEdge) error {
	_, err := w.insCall.ExecContext(w.ctx, string(e.Caller), string(e.Callee), string(e.Kind), e.File, e.Line)
	if err != nil {
		return fmt.Errorf("insert call edge %s -> %s: %w", e.Caller, e.Callee, err)
	}
	return nil
}

// ImportEdge writes one package-level dependency.
func (w *IndexWriter) ImportEdge(e core.ImportEdge) error {
	_, err := w.insImport.ExecContext(w.ctx, e.From, e.To)
	return err
}

// Coverage writes one test-to-symbol link.
func (w *IndexWriter) Coverage(c core.CoverageEdge) error {
	_, err := w.insCoverage.ExecContext(w.ctx, string(c.Test), string(c.Symbol), c.Fraction)
	return err
}

// Commit writes a revision and its file changes.
func (w *IndexWriter) Commit(c core.Commit) error {
	_, err := w.insCommit.ExecContext(w.ctx,
		c.Hash, c.AuthorName, c.AuthorEmail, c.When.Unix(), c.Subject,
		boolInt(c.IsFix()), boolInt(c.IsRevert()), boolInt(c.AIAssisted))
	if err != nil {
		return fmt.Errorf("insert commit %s: %w", c.Hash, err)
	}
	for _, f := range c.Files {
		if _, err := w.insCommitFil.ExecContext(w.ctx, c.Hash, f.Path, f.Added, f.Deleted, f.Renamed); err != nil {
			return fmt.Errorf("insert commit file %s@%s: %w", f.Path, c.Hash, err)
		}
	}
	return nil
}

// Authorship writes a blame-derived ownership record.
func (w *IndexWriter) Authorship(a core.Authorship) error {
	_, err := w.insAuthor.ExecContext(w.ctx, a.Path, a.Author, a.Lines, a.LastActive.Unix())
	return err
}

// ---------------------------------------------------------------- readers --

// Symbols returns every indexed declaration.
func (s *Store) Symbols(ctx context.Context) ([]core.Symbol, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT s.id, s.name, s.kind, s.package, f.path, s.start_line, s.end_line, s.exported, s.doc
        FROM symbols s JOIN files f ON f.id = s.file_id
        ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.Symbol
	for rows.Next() {
		var s core.Symbol
		var exported int
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.Package, &s.File,
			&s.StartLine, &s.EndLine, &exported, &s.Doc); err != nil {
			return nil, err
		}
		s.Exported = exported != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

// SymbolByID looks up one declaration. A missing symbol is not an error;
// callers get the zero value and false.
func (s *Store) SymbolByID(ctx context.Context, id core.SymbolID) (core.Symbol, bool, error) {
	var sym core.Symbol
	var exported int
	err := s.db.QueryRowContext(ctx, `
        SELECT s.id, s.name, s.kind, s.package, f.path, s.start_line, s.end_line, s.exported, s.doc
        FROM symbols s JOIN files f ON f.id = s.file_id WHERE s.id = ?`, string(id)).
		Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.Package, &sym.File,
			&sym.StartLine, &sym.EndLine, &exported, &sym.Doc)
	if err == sql.ErrNoRows {
		return core.Symbol{}, false, nil
	}
	if err != nil {
		return core.Symbol{}, false, err
	}
	sym.Exported = exported != 0
	return sym, true, nil
}

// CallEdges returns the dependency graph. When staticOnly is set, edges that
// CHA over-approximated are excluded — which is what grading a probe requires.
func (s *Store) CallEdges(ctx context.Context, staticOnly bool) ([]core.CallEdge, error) {
	q := `SELECT caller, callee, kind, file, line FROM call_edges`
	if staticOnly {
		q += ` WHERE kind = 'static'`
	}
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.CallEdge
	for rows.Next() {
		var e core.CallEdge
		if err := rows.Scan(&e.Caller, &e.Callee, &e.Kind, &e.File, &e.Line); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ImportEdges returns package-level dependencies.
func (s *Store) ImportEdges(ctx context.Context) ([]core.ImportEdge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT from_pkg, to_pkg FROM import_edges`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.ImportEdge
	for rows.Next() {
		var e core.ImportEdge
		if err := rows.Scan(&e.From, &e.To); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Coverage returns test-to-symbol links.
func (s *Store) Coverage(ctx context.Context) ([]core.CoverageEdge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT test, symbol, fraction FROM coverage`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.CoverageEdge
	for rows.Next() {
		var c core.CoverageEdge
		if err := rows.Scan(&c.Test, &c.Symbol, &c.Fraction); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Commits returns revisions newer than since, oldest first. A zero time means
// all of them.
func (s *Store) Commits(ctx context.Context, since time.Time) ([]core.Commit, error) {
	var cutoff int64
	if !since.IsZero() {
		cutoff = since.Unix()
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT hash, author_name, author_email, ts, subject, ai_assisted
        FROM commits WHERE ts >= ? ORDER BY ts`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byHash := make(map[string]int)
	var out []core.Commit
	for rows.Next() {
		var c core.Commit
		var ts int64
		var ai int
		if err := rows.Scan(&c.Hash, &c.AuthorName, &c.AuthorEmail, &ts, &c.Subject, &ai); err != nil {
			return nil, err
		}
		c.When = time.Unix(ts, 0).UTC()
		c.AIAssisted = ai != 0
		byHash[c.Hash] = len(out)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// File changes in one sweep rather than a query per commit: history for a
	// year of a busy repo is thousands of commits, and the round trips dominate.
	frows, err := s.db.QueryContext(ctx, `
        SELECT cf.commit_hash, cf.path, cf.added, cf.deleted, cf.renamed
        FROM commit_files cf JOIN commits c ON c.hash = cf.commit_hash
        WHERE c.ts >= ?`, cutoff)
	if err != nil {
		return nil, err
	}
	defer frows.Close()

	for frows.Next() {
		var hash string
		var fc core.FileChange
		if err := frows.Scan(&hash, &fc.Path, &fc.Added, &fc.Deleted, &fc.Renamed); err != nil {
			return nil, err
		}
		if i, ok := byHash[hash]; ok {
			out[i].Files = append(out[i].Files, fc)
		}
	}
	return out, frows.Err()
}

// Authorship returns blame-derived ownership records.
func (s *Store) Authorship(ctx context.Context) ([]core.Authorship, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path, author, lines, last_active FROM authorship`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.Authorship
	for rows.Next() {
		var a core.Authorship
		var ts int64
		if err := rows.Scan(&a.Path, &a.Author, &a.Lines, &ts); err != nil {
			return nil, err
		}
		a.LastActive = time.Unix(ts, 0).UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}

// Stats summarizes an index for the CLI to report after a run.
type Stats struct {
	Files, Symbols, CallEdges, ImportEdges, Coverage, Commits int
}

// Stats counts what the current index holds.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	targets := []struct {
		dst   *int
		table string
	}{
		{&st.Files, "files"}, {&st.Symbols, "symbols"}, {&st.CallEdges, "call_edges"},
		{&st.ImportEdges, "import_edges"}, {&st.Coverage, "coverage"}, {&st.Commits, "commits"},
	}
	for _, t := range targets {
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+t.table).Scan(t.dst); err != nil {
			return st, err
		}
	}
	return st, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
