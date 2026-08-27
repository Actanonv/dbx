package dbx

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

func TestCache_ThunderingHerdGetOrOpen(t *testing.T) {
	tmp := t.TempDir()
	dbName := "thundering_herd"

	// Pre-create DB file
	if _, err := createSQLiteDBFile(filepath.Join(tmp, dbName), tmp); err != nil {
		t.Fatalf("createSQLiteDBFile failed: %v", err)
	}

	c := NewCache(10 * time.Minute)
	defer c.Close()

	concurrency := 50
	var wg sync.WaitGroup
	results := make([]*bun.DB, concurrency)
	errors := make([]error, concurrency)

	// Launch 50 simultaneous GetOrOpen calls
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			db, err := c.GetOrOpen(dbName, WithDbFolder(tmp), WithDriverName(DriverSQLite))
			results[idx] = db
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// Verify all goroutines got no error and the EXACT same *bun.DB instance
	var firstDB *bun.DB
	for i := 0; i < concurrency; i++ {
		if errors[i] != nil {
			t.Fatalf("goroutine %d failed GetOrOpen: %v", i, errors[i])
		}
		if results[i] == nil {
			t.Fatalf("goroutine %d got nil DB", i)
		}
		if firstDB == nil {
			firstDB = results[i]
		} else if results[i] != firstDB {
			t.Fatalf("goroutine %d got different DB pointer instance %p, want %p", i, results[i], firstDB)
		}
	}
}

func TestCache_ConcurrentMultiTenantAccess(t *testing.T) {
	tmp := t.TempDir()
	tenantCount := 5
	workersPerTenant := 6

	for i := 0; i < tenantCount; i++ {
		tName := fmt.Sprintf("tenant_%d", i)
		if err := CreateDB(tName, CreateWithDriverName(DriverSQLite), CreateWithDbFolder(tmp)); err != nil {
			t.Fatalf("CreateDB for tenant %d failed: %v", i, err)
		}
	}

	c := NewCache(5 * time.Minute)
	defer c.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, tenantCount*workersPerTenant)

	for tIdx := 0; tIdx < tenantCount; tIdx++ {
		for wIdx := 0; wIdx < workersPerTenant; wIdx++ {
			wg.Add(1)
			go func(tenantID, workerID int) {
				defer wg.Done()
				tName := fmt.Sprintf("tenant_%d", tenantID)

				db, err := c.GetOrOpen(tName, WithDbFolder(tmp), WithDriverName(DriverSQLite))
				if err != nil {
					errCh <- fmt.Errorf("tenant %d worker %d GetOrOpen failed: %w", tenantID, workerID, err)
					return
				}

				if err := db.Ping(); err != nil {
					errCh <- fmt.Errorf("tenant %d worker %d Ping failed: %w", tenantID, workerID, err)
					return
				}
			}(tIdx, wIdx)
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("multi-tenant cache worker error: %v", err)
	}
}

func TestCache_SetMethod(t *testing.T) {
	tmp := t.TempDir()
	dbName := "set_test"

	if err := CreateDB(dbName, CreateWithDriverName(DriverSQLite), CreateWithDbFolder(tmp)); err != nil {
		t.Fatalf("CreateDB failed: %v", err)
	}

	db, err := OpenDB(dbName, WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	c := NewCache(5 * time.Minute)
	defer c.Close()

	// Initial Set should return true
	if ok := c.Set(dbName, db); !ok {
		t.Fatal("expected Set to return true for new entry")
	}

	// Secondary Set for existing key should return false
	if ok := c.Set(dbName, db); ok {
		t.Fatal("expected Set to return false for duplicate key")
	}

	// Verify Has returns db
	if got := c.Has(dbName); got != db {
		t.Fatalf("expected Has to return %p, got %p", db, got)
	}
}

func TestCache_ClosedStateOperations(t *testing.T) {
	tmp := t.TempDir()
	dbName := "closed_cache_test"
	if err := CreateDB(dbName, CreateWithDriverName(DriverSQLite), CreateWithDbFolder(tmp)); err != nil {
		t.Fatalf("CreateDB failed: %v", err)
	}

	c := NewCache(1 * time.Minute)
	_ = c.Close()

	// Has on closed cache
	if got := c.Has(dbName); got != nil {
		t.Fatalf("expected Has on closed cache to return nil, got %v", got)
	}

	// Get on closed cache
	if _, err := c.Get(dbName); err != ErrCacheClosed {
		t.Fatalf("expected ErrCacheClosed on Get, got %v", err)
	}

	// GetOrOpen on closed cache
	if _, err := c.GetOrOpen(dbName, WithDbFolder(tmp)); err != ErrCacheClosed {
		t.Fatalf("expected ErrCacheClosed on GetOrOpen, got %v", err)
	}

	// Set on closed cache
	if ok := c.Set(dbName, nil); ok {
		t.Fatal("expected Set on closed cache to return false")
	}
}

func TestOpenDB_WithLog(t *testing.T) {
	tmp := t.TempDir()
	dbName := "log_test"

	if err := CreateDB(dbName, CreateWithDriverName(DriverSQLite), CreateWithDbFolder(tmp)); err != nil {
		t.Fatalf("CreateDB failed: %v", err)
	}

	// Open with WithLog(true)
	db, err := OpenDB(dbName, WithDbFolder(tmp), WithDriverName(DriverSQLite), WithLog(true))
	if err != nil {
		t.Fatalf("OpenDB with WithLog failed: %v", err)
	}
	defer db.Close()

	// Execute simple query to ensure query hook does not fail
	var count int
	if err := db.NewSelect().ColumnExpr("1").Scan(context.Background(), &count); err != nil {
		t.Fatalf("query failed with WithLog enabled: %v", err)
	}
}
