package dbx

import (
	"context"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

func TestCache_Cleanup(t *testing.T) {
	tmp := t.TempDir()
	dbName := "cleanup_test"

	inactive := 300 * time.Millisecond
	c := NewCache(inactive)
	defer c.Close()

	db, err := c.GetOrOpen(dbName, WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("GetOrOpen failed: %v", err)
	}

	if c.Has(dbName) == nil {
		t.Fatal("DB should be in cache")
	}

	// Wait for cleanup to happen
	// Cleanup runs every inactive/10, but at least 1s.
	// Since we set inactive to 300ms, the ticker is 1s.
	time.Sleep(1500 * time.Millisecond)

	if c.Has(dbName) != nil {
		t.Fatal("DB should have been cleaned up")
	}

	// Check if DB is closed
	err = db.Ping()
	if err == nil {
		t.Fatal("DB should be closed after cleanup")
	}
}

func TestCache_CloseClosesDBs(t *testing.T) {
	tmp := t.TempDir()
	dbName := "close_test"

	c := NewCache(30 * time.Minute)
	db, err := c.GetOrOpen(dbName, WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("GetOrOpen failed: %v", err)
	}

	err = c.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After cache is closed, the DB should also be closed.
	err = db.Ping()
	if err == nil {
		t.Fatal("DB should be closed after cache Close")
	}
}

func TestCache_GetUpdatesLastAccessed(t *testing.T) {
	tmp := t.TempDir()
	dbName := "access_test"

	inactive := 1500 * time.Millisecond // 1s ticker
	c := NewCache(inactive)
	defer c.Close()

	_, err := c.GetOrOpen(dbName, WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("GetOrOpen failed: %v", err)
	}

	// 0s: Set (lastAccess=0s)
	// 1s: Ticker (since=1s < 1.5s, NO cleanup)
	time.Sleep(1200 * time.Millisecond)

	// 1.2s: Access it
	_, _ = c.Get(dbName)

	// 2s: Ticker (since=0.8s < 1.5s, NO cleanup)
	time.Sleep(1200 * time.Millisecond)

	// 2.4s: Should still be there
	if c.Has(dbName) == nil {
		t.Fatal("DB should still be in cache because of Get access")
	}

	// 3s: Ticker (since=1.8s > 1.5s, CLEANUP)
	time.Sleep(1200 * time.Millisecond)
	if c.Has(dbName) != nil {
		t.Fatal("DB should have been cleaned up after inactivity")
	}
}

func TestCache_Delete(t *testing.T) {
	tmp := t.TempDir()
	dbName := "delete_test"

	c := NewCache(10 * time.Minute)
	defer c.Close()

	db, err := c.GetOrOpen(dbName, WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("GetOrOpen failed: %v", err)
	}

	if c.Has(dbName) == nil {
		t.Fatal("expected DB in cache")
	}

	// Delete existing DB
	if err := c.Delete(dbName); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify evicted from cache
	if c.Has(dbName) != nil {
		t.Fatal("expected DB to be removed from cache after Delete")
	}

	// Verify DB is closed
	if err := db.Ping(); err == nil {
		t.Fatal("expected DB connection to be closed after Delete")
	}

	// Idempotent Delete on non-existing key
	if err := c.Delete("non_existing_key"); err != nil {
		t.Fatalf("expected nil error deleting non-existent key, got: %v", err)
	}

	// Delete on closed cache
	_ = c.Close()
	if err := c.Delete(dbName); err != ErrCacheClosed {
		t.Fatalf("expected ErrCacheClosed on closed cache Delete, got: %v", err)
	}
}

func TestCache_WithOnInit(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(10 * time.Minute)
	defer c.Close()

	ctx := context.Background()
	dbName := "tenant_with_schema"

	db, err := c.GetOrOpen(dbName,
		WithDbFolder(tmp),
		WithDriverName(DriverSQLite),
		WithOnInit(func(d *bun.DB) error {
			_, err := d.ExecContext(ctx, "CREATE TABLE tenant_configs (key TEXT PRIMARY KEY, val TEXT)")
			return err
		}),
	)
	if err != nil {
		t.Fatalf("GetOrOpen with OnInit failed: %v", err)
	}

	exists, err := TableExists(ctx, db, "tenant_configs")
	if err != nil || !exists {
		t.Fatalf("expected tenant_configs table to exist: %v", err)
	}

	// Subsequent GetOrOpen gets cached connection
	cachedDB, err := c.GetOrOpen(dbName, WithDbFolder(tmp))
	if err != nil {
		t.Fatalf("subsequent GetOrOpen failed: %v", err)
	}
	if cachedDB != db {
		t.Fatalf("expected cached DB instance to match original")
	}
}

