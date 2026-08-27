package dbx

import (
	"context"
	"embed"
	"path/filepath"
	"testing"
)

//go:embed testmigrations/*.sql
var multiStepMigrations embed.FS

func TestMigrate_FullLifecycle(t *testing.T) {
	tmp := t.TempDir()
	name := "lifecycle_test"

	ctx := context.Background()

	// 1. MigrateUpTo version 1
	err := MigrateUpTo(name, 1,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil {
		t.Fatalf("MigrateUpTo(1) failed: %v", err)
	}

	ver, err := MigrationVersion(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil {
		t.Fatalf("MigrationVersion failed: %v", err)
	}
	if ver != 1 {
		t.Fatalf("expected version 1, got %d", ver)
	}

	db, err := OpenDB(filepath.Join(tmp, name), WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	itemsExist, err := TableExists(ctx, db, "items")
	if err != nil || !itemsExist {
		t.Fatalf("expected items table to exist at v1: %v", err)
	}
	ordersExist, err := TableExists(ctx, db, "orders")
	if err != nil || ordersExist {
		t.Fatalf("expected orders table NOT to exist at v1: %v", err)
	}

	// 2. MigrateUpTo version 2
	err = MigrateUpTo(name, 2,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil {
		t.Fatalf("MigrateUpTo(2) failed: %v", err)
	}

	ver, err = MigrationVersion(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil || ver != 2 {
		t.Fatalf("expected version 2, got %d (err: %v)", ver, err)
	}

	ordersExist, err = TableExists(ctx, db, "orders")
	if err != nil || !ordersExist {
		t.Fatalf("expected orders table to exist at v2: %v", err)
	}

	// 3. MigrateDB to latest (version 3)
	err = MigrateDB(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil {
		t.Fatalf("MigrateDB to latest failed: %v", err)
	}

	ver, err = MigrationVersion(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil || ver != 3 {
		t.Fatalf("expected version 3, got %d (err: %v)", ver, err)
	}

	// Insert item and order with price column to verify v3 schema
	_, err = db.ExecContext(ctx, "INSERT INTO items(name, price) VALUES (?, ?)", "Widget", 100)
	if err != nil {
		t.Fatalf("insert into items at v3 failed: %v", err)
	}
	_, err = db.ExecContext(ctx, "INSERT INTO orders(item_id, quantity) VALUES (1, 5)")
	if err != nil {
		t.Fatalf("insert into orders at v3 failed: %v", err)
	}

	// 4. RollbackMigration (down to version 2)
	err = RollbackMigration(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil {
		t.Fatalf("RollbackMigration failed: %v", err)
	}

	ver, err = MigrationVersion(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil || ver != 2 {
		t.Fatalf("expected version 2 after rollback, got %d (err: %v)", ver, err)
	}

	// 5. MigrateDownTo version 1
	err = MigrateDownTo(name, 1,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil {
		t.Fatalf("MigrateDownTo(1) failed: %v", err)
	}

	ver, err = MigrationVersion(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil || ver != 1 {
		t.Fatalf("expected version 1 after MigrateDownTo, got %d (err: %v)", ver, err)
	}

	ordersExist, err = TableExists(ctx, db, "orders")
	if err != nil || ordersExist {
		t.Fatalf("expected orders table to be dropped at v1: %v", err)
	}

	// 6. ResetMigrations (down to 0)
	err = ResetMigrations(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil {
		t.Fatalf("ResetMigrations failed: %v", err)
	}

	ver, err = MigrationVersion(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil || ver != 0 {
		t.Fatalf("expected version 0 after ResetMigrations, got %d (err: %v)", ver, err)
	}

	itemsExist, err = TableExists(ctx, db, "items")
	if err != nil || itemsExist {
		t.Fatalf("expected items table to be dropped after reset: %v", err)
	}

	// 7. Re-apply all migrations from 0 to 3
	err = MigrateDB(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil {
		t.Fatalf("re-applying MigrateDB failed: %v", err)
	}

	ver, err = MigrationVersion(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	)
	if err != nil || ver != 3 {
		t.Fatalf("expected version 3 after full re-migration, got %d", ver)
	}
}

func TestMigrate_Idempotency(t *testing.T) {
	tmp := t.TempDir()
	name := "idempotency_test"

	opts := []CreateOptFn{
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	}

	// Run migration 3 times
	for i := 0; i < 3; i++ {
		if err := MigrateDB(name, opts...); err != nil {
			t.Fatalf("MigrateDB run %d failed: %v", i+1, err)
		}
	}

	ver, err := MigrationVersion(name, opts...)
	if err != nil {
		t.Fatalf("MigrationVersion failed: %v", err)
	}
	if ver != 3 {
		t.Fatalf("expected version 3, got %d", ver)
	}
}

func TestMigrate_Status(t *testing.T) {
	tmp := t.TempDir()
	name := "status_test"

	opts := []CreateOptFn{
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(multiStepMigrations),
		CreateWithSrcFolder("testmigrations"),
	}

	if err := MigrateDB(name, opts...); err != nil {
		t.Fatalf("MigrateDB failed: %v", err)
	}

	if err := MigrationStatus(name, opts...); err != nil {
		t.Fatalf("MigrationStatus failed: %v", err)
	}
}

func TestMigrate_DriverVariants(t *testing.T) {
	drivers := []DriverName{
		DriverSQLite,   // mattn/go-sqlite3 ("sqlite3")
		DriverSQLiteMc, // modernc.org/sqlite ("sqlite")
	}

	for _, drv := range drivers {
		t.Run(string(drv), func(t *testing.T) {
			tmp := t.TempDir()
			name := "driver_test_" + string(drv)

			opts := []CreateOptFn{
				CreateWithDriverName(drv),
				CreateWithDbFolder(tmp),
				CreateWithSource(multiStepMigrations),
				CreateWithSrcFolder("testmigrations"),
			}

			if err := MigrateDB(name, opts...); err != nil {
				t.Fatalf("MigrateDB with %s failed: %v", drv, err)
			}

			ver, err := MigrationVersion(name, opts...)
			if err != nil {
				t.Fatalf("MigrationVersion with %s failed: %v", drv, err)
			}
			if ver != 3 {
				t.Fatalf("expected version 3 with %s, got %d", drv, ver)
			}
		})
	}
}

func TestGooseDialect_Mapping(t *testing.T) {
	tests := []struct {
		driver DriverName
		want   string
	}{
		{DriverSQLite, "sqlite3"},
		{DriverSQLiteMc, "sqlite3"},
		{DriverPostgres, "postgres"},
		{DriverPgx, "postgres"},
		{DriverMySQL, "mysql"},
		{DriverMSSQL, "mssql"},
		{DriverName("custom"), "custom"},
	}

	for _, tt := range tests {
		got := gooseDialect(tt.driver)
		if got != tt.want {
			t.Errorf("gooseDialect(%s) = %s; want %s", tt.driver, got, tt.want)
		}
	}
}

