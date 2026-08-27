package dbx

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

//go:embed testfixtures/broken_migrations/*.sql
var brokenMigrations embed.FS

//go:embed testfixtures/multitenant_migrations/*.sql
var multitenantMigrations embed.FS

func TestMigrate_AtomicRollbackOnBrokenSQL(t *testing.T) {
	tmp := t.TempDir()
	name := "broken_migration_test"

	opts := []CreateOptFn{
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(brokenMigrations),
		CreateWithSrcFolder("testfixtures/broken_migrations"),
	}

	// MigrateDB should fail on migration 00002
	err := MigrateDB(name, opts...)
	if err == nil {
		t.Fatal("expected MigrateDB to fail on broken migration, but it succeeded")
	}

	// Verify that version is 1 (00001 succeeded, 00002 rolled back atomically)
	ver, err := MigrationVersion(name, opts...)
	if err != nil {
		t.Fatalf("MigrationVersion failed: %v", err)
	}
	if ver != 1 {
		t.Fatalf("expected version to remain 1 after failed migration, got %d", ver)
	}

	db, err := OpenDB(filepath.Join(tmp, name), WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Table 'users' from 00001 should exist
	usersExist, err := TableExists(ctx, db, "users")
	if err != nil || !usersExist {
		t.Fatalf("expected users table to exist: %v", err)
	}

	// Table 'profiles' from broken 00002 should NOT exist
	profilesExist, err := TableExists(ctx, db, "profiles")
	if err != nil || profilesExist {
		t.Fatalf("expected profiles table NOT to exist due to atomic rollback: %v", err)
	}
}

func TestMigrate_ConcurrentMigrationLockContention(t *testing.T) {
	tmp := t.TempDir()
	name := "concurrent_migration_test"

	concurrency := 20
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := MigrateDB(name,
				CreateWithDriverName(DriverSQLite),
				CreateWithDbFolder(tmp),
				CreateWithSource(multiStepMigrations),
				CreateWithSrcFolder("testmigrations"),
			)
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// In SQLite WAL mode with busy_timeout, concurrent migrations should cleanly succeed
	for err := range errCh {
		t.Errorf("concurrent migration goroutine returned error: %v", err)
	}

	// Final verification
	ver, err := MigrationVersion(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil {
		t.Fatalf("MigrationVersion failed after concurrent runs: %v", err)
	}
	if ver != 3 {
		t.Fatalf("expected final version 3, got %d", ver)
	}
}

func TestMigrate_NonexistentDirectoryAutoCreation(t *testing.T) {
	tmp := t.TempDir()
	nestedFolder := filepath.Join(tmp, "deep", "nested", "storage", "db")
	name := "nested_db"

	opts := []CreateOptFn{
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(nestedFolder),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	}

	if err := MigrateDB(name, opts...); err != nil {
		t.Fatalf("MigrateDB failed with nested folder: %v", err)
	}

	// Verify DB file exists in nested directory
	expectedFile := filepath.Join(nestedFolder, name+".db")
	if _, err := os.Stat(expectedFile); err != nil {
		t.Fatalf("expected DB file at %s, got err: %v", expectedFile, err)
	}

	ver, err := MigrationVersion(name, opts...)
	if err != nil || ver != 3 {
		t.Fatalf("expected version 3 in nested DB, got %d (err: %v)", ver, err)
	}
}

func TestMigrate_MultiTenantMigrationConcurrency(t *testing.T) {
	tmp := t.TempDir()
	tenantCount := 10

	var wg sync.WaitGroup
	errCh := make(chan error, tenantCount)

	for i := 0; i < tenantCount; i++ {
		wg.Add(1)
		tenantName := filepath.Join(tmp, "tenants", filepath.Clean("tenant_"+string(rune('0'+i))))
		go func(tName string) {
			defer wg.Done()
			err := MigrateDB(tName,
				CreateWithDriverName(DriverSQLite),
				CreateWithDbFolder(filepath.Dir(tName)),
				CreateWithSource(multitenantMigrations),
				CreateWithSrcFolder("testfixtures/multitenant_migrations"),
			)
			if err != nil {
				errCh <- err
			}
		}(tenantName)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("multi-tenant migration error: %v", err)
	}
}
