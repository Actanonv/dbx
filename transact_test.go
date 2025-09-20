package dbx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/uptrace/bun"
)

var (
	dbFolder string
)

// Test setup utilities
func setupTestDB(t *testing.T) *bun.DB {
	t.Helper()

	// Isolate DB files under a temp dir and configure package-level dbFolder
	tmp := t.TempDir()
	dbFolder = tmp

	// Use a unique filename per test
	dsn := filepath.Join(tmp, "testdb.sqlite")

	// Ensure the file exists because OpenDB expects an existing SQLite file path
	if _, err := createSQLiteDBFile(dsn, dbFolder); err != nil {
		t.Fatalf("createSQLiteDBFile failed: %v", err)
	}

	db, err := OpenDB(dsn, WithDbFolder(dbFolder), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}

	// basic schema for testing
	_, err = db.ExecContext(context.Background(), `
        CREATE TABLE IF NOT EXISTS items (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL
        );
    `)
	if err != nil {
		t.Fatalf("failed creating schema: %v", err)
	}

	return db
}

func mustNewTx(t *testing.T, db *bun.DB) *Transact {
	t.Helper()
	tx := NewTransact(db)
	return tx
}

func insertItem(t *testing.T, db bun.IDB, name string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.ExecContext(ctx, "INSERT INTO items(name) VALUES (?)", name)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}
}

func countItems(t *testing.T, db bun.IDB) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM items").Scan(&n); err != nil {
		t.Fatalf("count failed: %v", err)
	}
	return n
}

func TestDbMethodSwitchesBetweenDBAndTx(t *testing.T) {
	db := setupTestDB(t)
	tx := mustNewTx(t, db)

	// Initially, Db() should be *bun.DB
	switch tx.Db().(type) {
	case *bun.DB:
		// ok
	default:
		t.Fatalf("expected Db() to be *bun.DB before Start, got %T", tx.Db())
	}

	// Start transaction -> Db() should be *bun.Tx
	if err := tx.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	switch tx.Db().(type) {
	case *bun.Tx:
		// ok
	default:
		t.Fatalf("expected Db() to be *bun.Tx after Start, got %T", tx.Db())
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}

	// After rollback, it should be *bun.DB again
	switch tx.Db().(type) {
	case *bun.DB:
		// ok
	default:
		t.Fatalf("expected Db() to be *bun.DB after Rollback, got %T", tx.Db())
	}
}

func TestOuterCommitAndRollback(t *testing.T) {
	db := setupTestDB(t)
	tx := mustNewTx(t, db)

	// Start outer tx, insert 1 row, commit
	if err := tx.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	insertItem(t, tx.Db(), "a")
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit error: %v", err)
	}
	if got := countItems(t, db); got != 1 {
		t.Fatalf("want 1 after commit, got %d", got)
	}

	// Start outer tx, insert 1 row, rollback
	if err := tx.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	insertItem(t, tx.Db(), "b")
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}
	if got := countItems(t, db); got != 1 {
		t.Fatalf("want 1 after rollback, got %d", got)
	}
}

func TestNestedTransactionsCommitInnerThenOuter(t *testing.T) {
	db := setupTestDB(t)
	tx := mustNewTx(t, db)

	// Outer
	if err := tx.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start outer error: %v", err)
	}
	insertItem(t, tx.Db(), "outer-a")

	// Inner (savepoint)
	if err := tx.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start inner error: %v", err)
	}
	insertItem(t, tx.Db(), "inner-b")

	// Commit inner
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit inner error: %v", err)
	}

	// Now commit outer
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit outer error: %v", err)
	}

	if got := countItems(t, db); got != 2 {
		t.Fatalf("want 2 after nested commit, got %d", got)
	}
}

func TestNestedTransactionsRollbackInnerCommitOuter(t *testing.T) {
	db := setupTestDB(t)
	tx := mustNewTx(t, db)

	// Outer
	if err := tx.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start outer error: %v", err)
	}
	insertItem(t, tx.Db(), "outer-a")

	// Inner
	if err := tx.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start inner error: %v", err)
	}
	insertItem(t, tx.Db(), "inner-b")

	// Rollback inner
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback inner error: %v", err)
	}

	// Commit outer
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit outer error: %v", err)
	}

	if got := countItems(t, db); got != 1 {
		t.Fatalf("want 1 after rollback inner + commit outer, got %d", got)
	}
}

func TestTransactionHelperSuccessAndError(t *testing.T) {
	db := setupTestDB(t)
	tx := mustNewTx(t, db)

	// success path
	if err := tx.Transaction(context.Background(), nil, func(ctx context.Context) error {
		insertItem(t, tx.Db(), "ok")
		return nil
	}); err != nil {
		t.Fatalf("Transaction success returned error: %v", err)
	}
	if got := countItems(t, db); got != 1 {
		t.Fatalf("want 1 after successful Transaction, got %d", got)
	}

	// error path
	wantErr := errors.New("boom")
	if err := tx.Transaction(context.Background(), nil, func(ctx context.Context) error {
		insertItem(t, tx.Db(), "should-rollback")
		return wantErr
	}); err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("Transaction should return function error; got %v", err)
	}
	if got := countItems(t, db); got != 1 {
		t.Fatalf("want 1 after error rollback, got %d", got)
	}
}

func TestTransactionHelperPanicRollsBackAndRepanics(t *testing.T) {
	db := setupTestDB(t)
	tx := mustNewTx(t, db)

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic to be rethrown")
		}
		// ensure rollback happened
		if got := countItems(t, db); got != 0 {
			t.Fatalf("want 0 after panic rollback, got %d", got)
		}
	}()

	_ = tx.Transaction(context.Background(), nil, func(ctx context.Context) error {
		insertItem(t, tx.Db(), "x")
		panic("kaboom")
	})
}

// Ensure no tx active produces expected errors on Commit/Rollback
func TestCommitRollbackWithoutActiveTx(t *testing.T) {
	db := setupTestDB(t)
	tx := mustNewTx(t, db)

	if err := tx.Commit(); err == nil {
		t.Fatalf("expected error committing without active tx")
	}
	if err := tx.Rollback(); err == nil {
		t.Fatalf("expected error rolling back without active tx")
	}
}

// Silence staticcheck warning about unused import in tests when running in certain modes
var _ = fmt.Sprintf
var _ = os.Stat
