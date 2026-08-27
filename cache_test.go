package dbx

import (
	"os"
	"testing"
	"time"
)

func TestCache_Cleanup(t *testing.T) {
	_ = os.MkdirAll("./data", 0755)
	dbName := "cleanup_test"
	_ = CreateDB(dbName)
	defer os.Remove("./data/cleanup_test.db")

	inactive := 300 * time.Millisecond
	c := NewCache(inactive)
	defer c.Close()

	db, err := c.GetOrOpen(dbName)
	if err != nil {
		t.Fatalf("GetOrOpen failed: %v", err)
	}

	if c.Has(dbName) == nil {
		t.Fatal("DB should be in cache")
	}

	// Wait for cleanup to happen
	// Cleanup runs every inactive/10, but at least 1s.
	// Since we set inactive to 300ms, the ticker is 1s.
	// We need to wait more than 1s.
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
	_ = os.MkdirAll("./data", 0755)
	dbName := "close_test"
	_ = CreateDB(dbName)
	defer os.Remove("./data/close_test.db")

	c := NewCache(30 * time.Minute)
	db, err := c.GetOrOpen(dbName)
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
	_ = os.MkdirAll("./data", 0755)
	dbName := "access_test"
	_ = CreateDB(dbName)
	defer os.Remove("./data/access_test.db")

	inactive := 1500 * time.Millisecond // 1s ticker
	c := NewCache(inactive)
	defer c.Close()

	_, _ = c.GetOrOpen(dbName)

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
	if err := CreateDB(dbName, CreateWithDriverName(DriverSQLite), CreateWithDbFolder(tmp)); err != nil {
		t.Fatalf("CreateDB failed: %v", err)
	}

	c := NewCache(10 * time.Minute)
	defer c.Close()

	db, err := c.GetOrOpen(dbName, WithDbFolder(tmp))
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

