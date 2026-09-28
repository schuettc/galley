package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/schuettc/tools-common/sqlitedb"
)

// The index opens through tools-common/sqlitedb. These pin what that must not
// change for a user's existing ~/.galley/ledger.db, and what it adds.

func TestSchemaVersionIsTheMigrationCount(t *testing.T) {
	if schemaVersion != len(migrations) {
		t.Fatalf("schemaVersion %d, but %d migrations", schemaVersion, len(migrations))
	}
}

// TestOpenExistingV3IndexKeepsRows: a v3 index the way galley 0.10.1 left it
// (built here with a raw handle and the old DSN, each step in its own
// transaction) opens with every row intact and stays at v3.
func TestOpenExistingV3IndexKeepsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, step := range migrations {
		tx, err := raw.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := step(ctx, tx); err != nil {
			t.Fatalf("step %d: %v", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 3; i++ {
		if _, err := raw.Exec(`INSERT INTO decisions
			(source, line, v, at, at_unix, doc, review, kind, author, old, "new", reason, quote, context, raw, digest, round, text)
			VALUES ('/log.jsonl',?,1,'x',0,'doc.md','r','approved','agent','','','','q','','{}',?,1,'t')`, i, fmt.Sprintf("d%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	x, err := OpenIndexAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = x.Close() }()
	if v, err := x.SchemaVersion(); err != nil || v != 3 {
		t.Fatalf("schema v%d (%v), want v3", v, err)
	}
	if n, err := x.Count(); err != nil || n != 3 {
		t.Fatalf("%d rows (%v), want the 3 written by the old build", n, err)
	}
}

func TestIndexFilesArePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	x, err := OpenIndexAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = x.Close() }()
	if _, err := x.db.Exec(`INSERT INTO sources (path) VALUES ('/log.jsonl')`); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode %v, want 0600", filepath.Base(f), got)
		}
	}
}

// TestNewerIndexNamesRebuild: the refusal is sqlitedb's typed error, and it
// still tells the user the two ways out.
func TestNewerIndexNamesRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	x, err := OpenIndexAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion+7)); err != nil {
		t.Fatal(err)
	}
	_ = x.Close()
	_, err = OpenIndexAt(path)
	var newer *sqlitedb.NewerError
	if !errors.As(err, &newer) {
		t.Fatalf("error %v, want a *sqlitedb.NewerError", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "upgrade galley") || !strings.Contains(msg, "rebuild") {
		t.Fatalf("message %q must name upgrading galley and rebuilding", msg)
	}
}

// TestConcurrentFirstOpenIndex: two galley processes syncing at once may both
// be the first to open a new index.
func TestConcurrentFirstOpenIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x, err := OpenIndexAt(path)
			if err != nil {
				errs <- err
				return
			}
			errs <- x.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent first open: %v", err)
		}
	}
}
