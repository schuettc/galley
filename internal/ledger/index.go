package ledger

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/schuettc/galley/internal/ondisk"
	"github.com/schuettc/tools-common/sqlitedb"
)

// THE INDEX IS DERIVED, AND `galley ledger rebuild` IS THE PROOF.
//
// Everything below can be thrown away and reconstructed from the logs. That is
// not a nice property, it is the ONE that makes "the ledger is memory, never
// truth" checkable rather than aspirational — and it is also the migration
// path, so a schema change never needs a data migration that could lose a
// decision the log still holds.
//
// modernc.org/sqlite and not mattn/go-sqlite3: it is pure Go, so
// `CGO_ENABLED=0 GOOS=… GOARCH=…` still builds all four release targets. muster
// ships the same driver across the same matrix; `just cross` is what says so
// here. The dependency's weight is real (it is most of the binary-size delta
// this branch carries) and was accepted up front rather than discovered at
// release.

// schemaVersion is the index's schema generation, stored in SQLite's own
// `PRAGMA user_version`. Bumping it and adding a migration step is the whole
// upgrade procedure; `rebuild` is the escape hatch when a step would be more
// trouble than re-reading the logs.
const schemaVersion = 3

// migrations[i] takes the schema from version i to version i+1. Append only —
// an existing entry has already run on somebody's machine.
//
// The muster pattern (a list of guarded `ALTER TABLE`s replayed on every open,
// swallowing "duplicate column name") was read and deliberately not copied. It
// works, but it makes "what version is this database" unanswerable, so a
// migration that is NOT an additive column has nowhere to go. user_version is
// a counter SQLite already maintains for exactly this, and it costs one pragma.
var migrations = []sqlitedb.Step{
	// 0 -> 1: the initial schema.
	func(_ context.Context, tx *sql.Tx) error {
		_, err := tx.Exec(`
CREATE TABLE decisions (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	source   TEXT    NOT NULL,
	line     INTEGER NOT NULL,
	v        INTEGER NOT NULL,
	at       TEXT    NOT NULL,
	at_unix  INTEGER NOT NULL,
	doc      TEXT    NOT NULL,
	review   TEXT    NOT NULL,
	kind     TEXT    NOT NULL,
	author   TEXT    NOT NULL,
	old      TEXT    NOT NULL,
	"new"    TEXT    NOT NULL,
	reason   TEXT    NOT NULL,
	quote    TEXT    NOT NULL,
	context  TEXT    NOT NULL,
	raw      TEXT    NOT NULL,
	UNIQUE (source, line)
);
CREATE INDEX decisions_kind ON decisions (kind);
CREATE INDEX decisions_at   ON decisions (at_unix);
CREATE INDEX decisions_doc  ON decisions (doc);

CREATE TABLE sources (
	path           TEXT    PRIMARY KEY,
	ingested_bytes INTEGER NOT NULL DEFAULT 0,
	ingested_lines INTEGER NOT NULL DEFAULT 0,
	last_sync      TEXT    NOT NULL DEFAULT ''
);`)
		return err
	},

	// 1 -> 2: dedupe on CONTENT as well as position. See the comment above
	// Sync, and Record.Digest for what is hashed.
	//
	// This runs in the migration machinery rather than being folded into the
	// step above even though v1 never left this branch, because a schema
	// change that has been committed once is a schema somebody could be
	// holding — and because a migration path nobody has ever run is a
	// migration path nobody knows works. TestMigrateV1AddsTheDigestAndDrops
	// Duplicates builds a v1 database WITH a duplicate in it by hand and takes
	// it through this step.
	func(_ context.Context, tx *sql.Tx) error {
		if _, err := tx.Exec(`ALTER TABLE decisions ADD COLUMN digest TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
		// SQLite has no SHA-256, so the backfill is a read-compute-write in
		// Go, over the raw line each row already carries. A row whose raw
		// will not parse keeps its empty digest and is excluded from the
		// unique index below by the partial-index predicate — an unparseable
		// row is one we could not have deduped on ingest either.
		rows, err := tx.Query(`SELECT id, raw FROM decisions`)
		if err != nil {
			return err
		}
		type patch struct {
			id     int64
			digest string
		}
		var patches []patch
		for rows.Next() {
			var id int64
			var raw string
			if err := rows.Scan(&id, &raw); err != nil {
				_ = rows.Close()
				return err
			}
			var rec Record
			if err := json.Unmarshal([]byte(raw), &rec); err != nil {
				continue
			}
			patches = append(patches, patch{id, rec.Digest()})
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, p := range patches {
			if _, err := tx.Exec(`UPDATE decisions SET digest = ? WHERE id = ?`, p.digest, p.id); err != nil {
				return err
			}
		}
		// Any duplicate already indexed under v1 has to go before the unique
		// index can exist. Lowest id wins — the first time the decision was
		// seen is the one to keep.
		if _, err := tx.Exec(`DELETE FROM decisions WHERE digest <> '' AND id NOT IN
			(SELECT MIN(id) FROM decisions WHERE digest <> '' GROUP BY source, digest)`); err != nil {
			return err
		}
		_, err = tx.Exec(`CREATE UNIQUE INDEX decisions_digest ON decisions (source, digest) WHERE digest <> ''`)
		return err
	},

	// 2 -> 3: instructions are ledger rows in the rounds model. Their text and
	// the round they belong to are first-class columns so the derived index can
	// answer history queries without decoding every raw line.
	func(_ context.Context, tx *sql.Tx) error {
		if _, err := tx.Exec(`ALTER TABLE decisions ADD COLUMN round INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		_, err := tx.Exec(`ALTER TABLE decisions ADD COLUMN text TEXT NOT NULL DEFAULT ''`)
		return err
	},
}

// Index is the per-user SQLite store.
type Index struct {
	db   *sqlitedb.DB
	path string
}

// IndexDir is where the index lives. GALLEY_LEDGER_DIR overrides it — tests
// point it at a temp dir, and a user who keeps their home directory on a
// network mount can move it.
//
// NOT license.Dir(). That one is os.UserConfigDir()/galley, which on macOS is
// `~/Library/Application Support/galley` — right for a token nobody should be
// editing, wrong for a store whose whole pitch is "you can open it with the
// sqlite3 binary and ask it questions". The spec asks for a per-user store on
// the machine; `~/.galley` is where a person will look for it.
func IndexDir() (string, error) {
	if dir := os.Getenv("GALLEY_LEDGER_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".galley"), nil
}

// IndexPath is the database file.
func IndexPath() (string, error) {
	dir, err := IndexDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ledger.db"), nil
}

// OpenIndex opens (creating if needed) the index at the default location.
func OpenIndex() (*Index, error) {
	path, err := IndexPath()
	if err != nil {
		return nil, err
	}
	return OpenIndexAt(path)
}

// OpenIndexAt opens the index at an explicit path.
//
// Fails soft like everything else here: a home directory that cannot be
// written returns an error, and the caller's decision still reached the log.
//
// tools-common/sqlitedb owns the conventions: WAL so a reader (`stats`) is
// not blocked by a writer (`sync`), a busy timeout because two galley
// processes may sync at once, one connection (the index is tiny and the work
// bursty; a pool buys nothing and costs SQLITE_BUSY), files 0600, and the
// migrations below as user_version steps, one transaction each.
func OpenIndexAt(path string) (*Index, error) {
	db, err := sqlitedb.Open(context.Background(), path, sqlitedb.Options{Migrations: migrations})
	if err != nil {
		var newer *sqlitedb.NewerError
		if errors.As(err, &newer) {
			return nil, fmt.Errorf("%w — upgrade galley, or delete %s and rebuild", err, path)
		}
		return nil, fmt.Errorf("open index %s: %w", path, err)
	}
	return &Index{db: db, path: path}, nil
}

// Path is where this index lives on disk.
func (x *Index) Path() string { return x.path }

// Close releases the database.
func (x *Index) Close() error { return x.db.Close() }

// SchemaVersion reports the version the open database is at.
func (x *Index) SchemaVersion() (int, error) {
	return x.db.Version(context.Background())
}

// Source is one known log and how far into it the index has read.
type Source struct {
	Path     string `json:"path"`
	Bytes    int64  `json:"ingestedBytes"`
	Lines    int64  `json:"ingestedLines"`
	LastSync string `json:"lastSync"`
}

// Sources lists every log this index has ever been pointed at, in path order.
func (x *Index) Sources() ([]Source, error) {
	rows, err := x.db.Query(`SELECT path, ingested_bytes, ingested_lines, last_sync FROM sources ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Source{}
	for rows.Next() {
		var s Source
		if err := rows.Scan(&s.Path, &s.Bytes, &s.Lines, &s.LastSync); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SyncResult is what one Sync did.
type SyncResult struct {
	Path string `json:"path"`
	// Added counts records inserted on this pass.
	Added int `json:"added"`
	// Skipped counts COMPLETE lines that would not parse. They are stepped
	// over and their bytes consumed — a line that is whole and malformed will
	// never become valid, and stalling the offset on it would freeze the index
	// forever behind one bad byte.
	Skipped int `json:"skipped"`
	// Future counts INGESTED rows whose `v` is newer than this build's
	// ledger.Version. They are kept, indexed and counted — never skipped —
	// which is the policy Version states: the log is authoritative, lines are
	// never rewritten, and a reader ignores the fields it cannot interpret.
	//
	// IT EXISTS BECAUSE THE FIELD WAS OTHERWISE DECORATIVE. `v` was written on
	// every line, hashed into Digest and stored in this column, and nothing
	// ever compared it to anything — so a log carrying rows from a galley whose
	// meanings had moved read exactly like one that did not, and every count in
	// the rollups was quietly over a mixed population. Counting is the whole of
	// what the stated policy allows: not skipping the row, not refusing the
	// sync, just being able to say the population is mixed.
	Future int `json:"future"`
	// Torn is set when the file ended mid-line. Those bytes are NOT consumed;
	// the next Sync reads the line once the appender finishes it.
	Torn bool `json:"torn"`
	// Missing is set when there is no such log yet. Not an error: a repo where
	// nothing has been decided has no decisions file.
	Missing bool `json:"missing"`
	// Reset is set when the log got SHORTER than what had been ingested, so
	// this source's rows were dropped and it was re-read from zero.
	Reset bool  `json:"reset"`
	Bytes int64 `json:"ingestedBytes"`
}

// Sync ingests logPath from the stored offset to the last COMPLETE line.
//
// Incremental and idempotent, and those are separate mechanisms on purpose.
// The offset in `sources` is what makes a re-sync CHEAP; the two UNIQUE
// constraints on `decisions`, with `INSERT OR IGNORE`, are what make a re-sync
// CORRECT. The offset alone would be a bug waiting for either of them to fail.
//
// THE TWO CONSTRAINTS ANSWER TWO DIFFERENT QUESTIONS, and BOTH are kept:
//
//   - `UNIQUE (source, line)` — POSITION. A wrong offset cannot duplicate a
//     row. This holds by construction, independent of any hash function.
//   - `UNIQUE (source, digest) WHERE digest <> ”` — CONTENT. The same
//     decision recorded at two positions cannot duplicate a row. This is the
//     one `merge=union` makes necessary: the union merge keeps BOTH sides'
//     lines — right for concurrent appends, and it means a cherry-pick, a
//     rebase replay, or a merge of two branches that each recorded the same
//     decision leaves that record in the log twice at different line numbers.
//     Position uniqueness cannot see that, and every count `stats` reports
//     would inflate: precisely the numbers this whole store exists to make
//     trustworthy.
//
// The content constraint does subsume the position one for the case they
// overlap — re-reading the same bytes yields the same digest — and the
// position one is kept anyway BECAUSE THEY DO NOT REST ON THE SAME
// ASSUMPTION. A digest is a claim about a hash; a line number is a claim about
// arithmetic this file does itself. Dropping the cheaper claim to lean the
// whole property on SHA-256 buys one index and gives up an independent check.
//
// ONE LOSS, ACCEPTED KNOWINGLY: two genuinely distinct decisions that hash
// alike collapse to one row. Every field is in the digest including `at` at
// nanosecond precision, so that needs two identical decisions about identical
// text in the same nanosecond. An undercount of one there is strictly better
// than double-counting every merge.
//
// A TORN FINAL LINE IS EXPECTED, NOT EXCEPTIONAL. The log is appended to by
// live galley processes; a sync that runs while one of them is mid-write, or
// after a machine died mid-write, sees a line with no newline on it. Ingest
// stops at the last '\n' and leaves the offset there, so the partial line is
// read whole on the next pass (or, if the writer really did die, sits there
// costing one unparsed line and nothing else).
func (x *Index) Sync(logPath string) (SyncResult, error) {
	abs, err := filepath.Abs(logPath)
	if err != nil {
		return SyncResult{Path: logPath}, err
	}
	res := SyncResult{Path: abs}

	if err := x.remember(abs); err != nil {
		return res, err
	}

	var offset, lineno int64
	if err := x.db.QueryRow(`SELECT ingested_bytes, ingested_lines FROM sources WHERE path = ?`, abs).
		Scan(&offset, &lineno); err != nil {
		return res, err
	}

	st, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		res.Missing = true
		res.Bytes = offset
		return res, nil
	}
	if err != nil {
		return res, err
	}

	// The log got shorter than what we had read. A committed append-only file
	// does not normally shrink, so this is a branch switch, a revert, or a
	// hand-edit — and in every one of those cases the rows we hold are about
	// bytes that no longer exist. Drop them and re-read from zero, which is
	// `rebuild` applied to one source. (A log REPLACED with different content
	// of the same length is not detectable from a length alone, and is not
	// guessed at: `galley ledger rebuild` is the answer to any doubt.)
	if st.Size() < offset {
		if err := x.forget(abs); err != nil {
			return res, err
		}
		offset, lineno = 0, 0
		res.Reset = true
	}
	if st.Size() == offset {
		res.Bytes = offset
		return res, nil
	}

	f, err := os.Open(abs) //nolint:gosec // the path is the caller's own log
	if err != nil {
		return res, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(offset, 0); err != nil {
		return res, err
	}
	// The file may have grown since the Stat — a live galley appending while
	// this runs is the ordinary case. Reading exactly the bytes Stat reported
	// is what makes the offset arithmetic below true: anything appended after
	// the measurement belongs to the next Sync.
	buf := make([]byte, st.Size()-offset)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return res, err
	}
	buf = buf[:n]

	// Everything after the last newline is an unfinished line. Its bytes stay
	// unconsumed.
	end := bytes.LastIndexByte(buf, '\n')
	if end < 0 {
		res.Torn = len(buf) > 0
		res.Bytes = offset
		return res, nil
	}
	res.Torn = end+1 < len(buf)
	complete := buf[:end+1]

	tx, err := x.db.Begin()
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	ins, err := tx.Prepare(`INSERT OR IGNORE INTO decisions
		(source, line, digest, v, at, at_unix, doc, review, kind, author, old, "new", reason, quote, context, round, text, raw)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return res, err
	}
	defer func() { _ = ins.Close() }()

	for _, raw := range splitLines(complete) {
		lineno++
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(trimmed), &rec); err != nil {
			res.Skipped++
			continue
		}
		if ondisk.Future(rec.V, Version) {
			// KEPT AND COUNTED — see SyncResult.Future.
			res.Future++
		}
		out, err := ins.Exec(abs, lineno, rec.Digest(), rec.V, rec.At.UTC().Format(time.RFC3339Nano),
			rec.At.UnixNano(), rec.Doc, rec.Review, string(rec.Kind), rec.Author,
			rec.Old, rec.New, rec.Reason, rec.Quote, rec.Context, rec.Round, rec.Text, trimmed)
		if err != nil {
			return res, err
		}
		if affected, aerr := out.RowsAffected(); aerr == nil && affected > 0 {
			res.Added++
		}
	}

	consumed := offset + int64(end+1)
	if _, err := tx.Exec(`UPDATE sources SET ingested_bytes = ?, ingested_lines = ?, last_sync = ? WHERE path = ?`,
		consumed, lineno, time.Now().UTC().Format(time.RFC3339), abs); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	res.Bytes = consumed
	return res, nil
}

// SyncAll syncs every known source plus any extra paths, registering the
// extras as it goes. Results come back in the order they were synced.
func (x *Index) SyncAll(extra ...string) ([]SyncResult, error) {
	seen := map[string]bool{}
	var paths []string
	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if !seen[abs] {
			seen[abs] = true
			paths = append(paths, abs)
		}
	}
	for _, p := range extra {
		add(p)
	}
	known, err := x.Sources()
	if err != nil {
		return nil, err
	}
	for _, s := range known {
		add(s.Path)
	}

	out := make([]SyncResult, 0, len(paths))
	for _, p := range paths {
		res, err := x.Sync(p)
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}

// Rebuild wipes every derived row and re-ingests every known source from byte
// zero.
//
// THIS IS THE COMMAND THAT MAKES THE INVARIANT CHECKABLE. If rebuild does not
// reproduce the index, something in here is holding state the logs do not — and
// that state is truth living in the wrong store. TestRebuildReproducesTheIndex
// asserts row-for-row equality across it.
//
// The `sources` rows are kept (minus their offsets): the set of logs this
// machine knows about is not derivable from any of them, and losing it would
// make rebuild a command that quietly empties the index on a laptop whose repos
// are not all checked out today.
func (x *Index) Rebuild() ([]SyncResult, error) {
	known, err := x.Sources()
	if err != nil {
		return nil, err
	}
	if _, err := x.db.Exec(`DELETE FROM decisions`); err != nil {
		return nil, err
	}
	if _, err := x.db.Exec(`UPDATE sources SET ingested_bytes = 0, ingested_lines = 0, last_sync = ''`); err != nil {
		return nil, err
	}
	out := make([]SyncResult, 0, len(known))
	for _, s := range known {
		res, err := x.Sync(s.Path)
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}

// remember registers a log without reading it.
func (x *Index) remember(abs string) error {
	_, err := x.db.Exec(`INSERT OR IGNORE INTO sources (path) VALUES (?)`, abs)
	return err
}

// forget drops one source's derived rows and rewinds it to zero.
func (x *Index) forget(abs string) error {
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM decisions WHERE source = ?`, abs); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE sources SET ingested_bytes = 0, ingested_lines = 0 WHERE path = ?`, abs); err != nil {
		return err
	}
	return tx.Commit()
}

// STORAGE IS PER SOURCE, COUNTING IS PER DECISION.
//
// A decision recorded once and cloned into five worktrees is ONE decision, and
// the ledger must never report five. This is not a corner case: Court works in
// git worktrees constantly — this repository had eight live while the ledger
// was being built — and each checkout carries its own copy of the same
// committed `.galley/decisions.jsonl` at its own path. Add an ordinary second
// clone of a repo and the same thing happens. It lands on day one.
//
// The two halves are both right, and they are answering different questions:
//
//   - ROWS stay per source, because that is the honest record: THIS file, at
//     THIS path, contained THIS line. `UNIQUE (source, digest)` keeps one
//     source from holding a line twice (the merge=union case) and deliberately
//     says nothing across sources — `forget` and `Rebuild` both work per
//     source, and a global unique index would make "delete this source's rows"
//     leave rows behind and let ingest ORDER decide which checkout owns a
//     decision. The reasoning for keeping two independent constraints applies
//     here too: do not collapse a storage question into a counting one.
//   - EVERY AGGREGATE counts DISTINCT digest, because every question the
//     ledger answers is about DECISIONS, not about lines in files. Counts by
//     kind, top decline reasons, the accept-versus-rewrite ratio: all of them
//     go through `canonical`.
//
// Anything added here that aggregates, ranks or lists decisions joins
// `canonical`. A new query that reads `decisions` directly is a new instance of
// this bug.
const canonical = `WITH canonical AS (
	SELECT MIN(id) AS id FROM decisions WHERE digest <> '' GROUP BY digest
	UNION ALL
	SELECT id FROM decisions WHERE digest = ''
)`

// The representative is MIN(id) — an arbitrary choice that is safe because it
// is not arbitrary in any way that shows: every field of the record is in the
// digest, so two rows sharing one differ ONLY in `source`, `line` and possibly
// `raw`'s whitespace, and no aggregate reads any of those.
//
// A row with an EMPTY digest is one whose stored `raw` would not parse (only
// reachable in an index migrated up from v1 — ingest steps over such lines).
// Those are counted INDIVIDUALLY rather than collapsed together: a row with no
// digest has no identity to collapse ON, and folding them into one would be
// the dedupe inventing an equality it cannot support.

// decidedOnly is the WHERE clause that turns "every row" into "every decision".
// It is a NOT IN over NonDecisionKinds rather than a hand-written `kind <>
// 'reopened'` so a second non-decision kind cannot be added to the vocabulary
// and leave the counters silently including it.
func decidedOnly() (string, []any) {
	return `d.kind NOT IN (` + placeholders(len(NonDecisionKinds)) + `)`, kindArgs(NonDecisionKinds)
}

// Count is the number of indexed ROWS — the storage-level number, per source.
// It is what the sync report shows beside each log, and what the tests assert
// the per-source record with. For the number of DECISIONS, which is what a
// person means, use CountDecisions or Stats.
func (x *Index) Count() (int, error) {
	var n int
	err := x.db.QueryRow(`SELECT COUNT(*) FROM decisions`).Scan(&n)
	return n, err
}

// CountDecisions is the number of distinct DECISIONS: the same decision
// recorded in several checkouts of one repository collapses to one, and a row
// that decides nothing (see NonDecisionKinds) is not counted at all. The
// function is named for what it answers, and it has to answer that.
func (x *Index) CountDecisions() (int, error) {
	where, args := decidedOnly()
	var n int
	err := x.db.QueryRow(canonical+`
		SELECT COUNT(*) FROM decisions d JOIN canonical c ON c.id = d.id
		WHERE `+where, args...).Scan(&n)
	return n, err
}

// Decisions returns indexed records in decision order, newest last, capped by
// limit (0 means all). Mostly for tests and for anyone reading the index by
// hand — the honest answer to "what is in there" without a sqlite3 binary.
//
// A listing of DECISIONS, so it goes through canonical like every aggregate: a
// decision cloned into five worktrees appears once.
func (x *Index) Decisions(limit int) ([]Record, error) {
	if limit <= 0 {
		limit = -1 // SQLite's "no limit"
	}
	rows, err := x.db.Query(canonical+`
		SELECT d.raw FROM decisions d JOIN canonical c ON c.id = d.id
		ORDER BY d.at_unix, d.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Record{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var rec Record
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Instructions returns the reviewer asks attached to one committed document
// round. They are ordered as written and remain on that round permanently: a
// later revision discharges an instruction rather than resolving, moving, or
// rewriting its row.
func (x *Index) Instructions(doc string, round int) ([]Record, error) {
	rows, err := x.db.Query(canonical+`
		SELECT d.raw FROM decisions d JOIN canonical c ON c.id = d.id
		WHERE d.kind = ? AND d.doc = ? AND d.round = ?
		ORDER BY d.at_unix, d.id`, string(KindInstruction), filepath.ToSlash(doc), round)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Record{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var rec Record
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// KindCount is one row of the by-kind tally.
type KindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// ReasonCount is one row of the decline-reason tally.
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// AgentFate is what became of the agent's proposals, and it is the question
// the whole idea was justified by: which of my proposals get accepted versus
// quietly rewritten.
//
// The buckets are not the kinds. `approved` and `entrusted` both mean the text
// landed as proposed, so they are one bucket. `hand` is the interesting one and
// has no other home: the reviewer neither accepted nor declined, they REWROTE
// it — which every accept/reject counter in galley reads as silence. `rejected`
// is its own bucket rather than part of `declined`, because the whole
// difference between the two verbs is whether a reason was given, and folding
// them would make "top decline reasons" look like it covered a population it
// has nothing to say about.
//
// THE POPULATION IS ProposalKinds, NOT "every row the agent authored". The
// wiring also records resolves, deletes, verdicts and reopens, all of which
// carry an author; counting those in Total would give an accept rate that falls
// when somebody settles a conversation. The four buckets therefore sum to
// Total, and TestAgentRollupCountsProposalsOnly asserts that they do.
type AgentFate struct {
	Accepted   int     `json:"accepted"`
	Declined   int     `json:"declined"`
	Rejected   int     `json:"rejected"`
	Rewritten  int     `json:"rewritten"`
	Total      int     `json:"total"`
	AcceptRate float64 `json:"acceptRate"`
}

// Stats is the small, honest rollup `galley ledger stats` prints.
//
// TOTAL IS DECISIONS, AND A REOPEN IS NOT ONE. NonDecisionKinds names the
// exception and NotDecisions counts what was left out, so the two numbers
// together still account for every row `ByKind` lists — a total that quietly
// dropped rows nobody could see would be worse than the overcount it replaced.
type Stats struct {
	Total int `json:"total"`
	// NotDecisions is how many recorded rows are not decisions (today: every
	// `reopened`). Reported rather than hidden: the by-kind tally below counts
	// every row, so without this the columns do not add up to the headline and
	// a reader has no way to find out why.
	NotDecisions   int           `json:"notDecisions"`
	Sources        int           `json:"sources"`
	Docs           int           `json:"docs"`
	ByKind         []KindCount   `json:"byKind"`
	DeclineReasons []ReasonCount `json:"declineReasons"`
	Agent          AgentFate     `json:"agent"`
	First          string        `json:"first"`
	Last           string        `json:"last"`
}

// Stats computes the rollup. GROUP BY questions the log cannot answer and was
// never meant to — which is the entire reason the index exists.
func (x *Index) Stats(topReasons int) (Stats, error) {
	var s Stats
	if topReasons <= 0 {
		topReasons = 5
	}

	// Every aggregate below joins canonical — see the rule above it. Sources
	// is the one number that is legitimately per source, because it IS the
	// question "how many logs does this machine know".
	//
	// TOTAL COUNTS DECISIONS AND DOCS COUNTS DOCUMENTS, which is why only one
	// of them wears the filter: a reopen is not a decision, but the document it
	// happened in is a document this ledger knows about.
	where, args := decidedOnly()
	if err := x.db.QueryRow(canonical+`
		SELECT
			SUM(CASE WHEN `+where+` THEN 1 ELSE 0 END),
			SUM(CASE WHEN `+where+` THEN 0 ELSE 1 END),
			COUNT(DISTINCT d.doc)
		FROM decisions d JOIN canonical c ON c.id = d.id`, append(args, args...)...).
		Scan(&nullInt{&s.Total}, &nullInt{&s.NotDecisions}, &s.Docs); err != nil {
		return s, err
	}
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM sources`).Scan(&s.Sources); err != nil {
		return s, err
	}

	s.ByKind = []KindCount{}
	rows, err := x.db.Query(canonical + `
		SELECT d.kind, COUNT(*) n FROM decisions d JOIN canonical c ON c.id = d.id
		GROUP BY d.kind ORDER BY n DESC, d.kind`)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var kc KindCount
		if err := rows.Scan(&kc.Kind, &kc.Count); err != nil {
			_ = rows.Close()
			return s, err
		}
		s.ByKind = append(s.ByKind, kc)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return s, err
	}

	// Declines with no reason are excluded rather than bucketed as "".
	// `decline` takes an optional --note, so a blank one is "the reviewer did
	// not say", and printing that at the top of a list headed "top decline
	// reasons" would be the list lying about its own subject.
	s.DeclineReasons = []ReasonCount{}
	rrows, err := x.db.Query(canonical+`
		SELECT d.reason, COUNT(*) n FROM decisions d JOIN canonical c ON c.id = d.id
		WHERE d.kind = ? AND TRIM(d.reason) <> ''
		GROUP BY d.reason ORDER BY n DESC, d.reason LIMIT ?`,
		string(KindDeclined), topReasons)
	if err != nil {
		return s, err
	}
	for rrows.Next() {
		var rc ReasonCount
		if err := rrows.Scan(&rc.Reason, &rc.Count); err != nil {
			_ = rrows.Close()
			return s, err
		}
		s.DeclineReasons = append(s.DeclineReasons, rc)
	}
	_ = rrows.Close()
	if err := rrows.Err(); err != nil {
		return s, err
	}

	// Joins canonical like every other aggregate here, and restricts the
	// population to ProposalKinds — see AgentFate for why the denominator has
	// to. The IN list is built rather than written out so adding a proposal
	// kind cannot leave the total counting a fate no bucket holds.
	if err := x.db.QueryRow(canonical+`
		SELECT
			SUM(CASE WHEN d.kind IN (?,?) THEN 1 ELSE 0 END),
			SUM(CASE WHEN d.kind = ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN d.kind = ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN d.kind = ? THEN 1 ELSE 0 END),
			COUNT(*)
		FROM decisions d JOIN canonical c ON c.id = d.id
		WHERE d.author = ? AND d.kind IN (`+placeholders(len(ProposalKinds))+`)`,
		append([]any{
			string(KindApproved), string(KindEntrusted), string(KindDeclined),
			string(KindRejected), string(KindHand), AuthorAgent,
		}, kindArgs(ProposalKinds)...)...).
		Scan(&nullInt{&s.Agent.Accepted}, &nullInt{&s.Agent.Declined}, &nullInt{&s.Agent.Rejected},
			&nullInt{&s.Agent.Rewritten}, &s.Agent.Total); err != nil {
		return s, err
	}
	if s.Agent.Total > 0 {
		s.Agent.AcceptRate = float64(s.Agent.Accepted) / float64(s.Agent.Total)
	}

	// THE RANGE IS THE RANGE OF THE DECISIONS THE HEADLINE COUNTS. It wears the
	// same filter as Total, and it did not: `MIN/MAX(d.at)` over every row let a
	// `reopened` — the one kind Total excludes — set either end, so the dates
	// printed under "N decisions" could name a day on which, by that headline's
	// own reckoning, nothing was decided. Docs is the deliberate exemption here
	// and says so above; this one was silent, which is the difference.
	var first, last sql.NullString
	if err := x.db.QueryRow(canonical+`
		SELECT MIN(d.at), MAX(d.at) FROM decisions d JOIN canonical c ON c.id = d.id
		WHERE `+where, args...).
		Scan(&first, &last); err != nil {
		return s, err
	}
	s.First, s.Last = first.String, last.String
	return s, nil
}

// placeholders is "?,?,?" for an IN list of n values.
func placeholders(n int) string {
	if n == 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// kindArgs is a kind list as query arguments.
func kindArgs(kinds []Kind) []any {
	out := make([]any, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, string(k))
	}
	return out
}

// nullInt scans a SQL NULL (which SUM over zero rows returns) as 0.
type nullInt struct{ dst *int }

func (n *nullInt) Scan(v any) error {
	if v == nil {
		*n.dst = 0
		return nil
	}
	switch t := v.(type) {
	case int64:
		*n.dst = int(t)
	case int:
		*n.dst = t
	default:
		return fmt.Errorf("ledger: cannot scan %T as int", v)
	}
	return nil
}

// splitLines splits a buffer that ENDS in '\n' into its lines, without the
// terminators and without the trailing empty element bytes.Split would leave.
func splitLines(b []byte) [][]byte {
	lines := bytes.Split(b, []byte{'\n'})
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}
