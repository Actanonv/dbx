package dbx_test

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/actanonv/dbx"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed testmigrations/*.sql
var exampleMigrations embed.FS

func ExampleOpenDB() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_open")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create an empty database first
	if err := dbx.CreateDB("example_app", dbx.CreateWithDbFolder(tmpDir)); err != nil {
		log.Fatal(err)
	}

	// Open the database with options
	db, err := dbx.OpenDB("example_app",
		dbx.WithDriverName(dbx.DriverSQLite),
		dbx.WithDbFolder(tmpDir),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Database connection established successfully")
	// Output:
	// Database connection established successfully
}

func ExampleCreateDB() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_create")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a SQLite database and automatically apply migrations from embed.FS
	err = dbx.CreateDB("store",
		dbx.CreateWithDriverName(dbx.DriverSQLite),
		dbx.CreateWithDbFolder(tmpDir),
		dbx.WithSource(exampleMigrations),
		dbx.WithSrcFolder("testmigrations"),
	)
	if err != nil {
		log.Fatal(err)
	}

	ver, err := dbx.MigrationVersion("store",
		dbx.CreateWithDbFolder(tmpDir),
		dbx.WithSource(exampleMigrations),
		dbx.WithSrcFolder("testmigrations"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Database created at migration version: %d\n", ver)
	// Output:
	// Database created at migration version: 3
}

func ExampleMigrateDB() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_migrate")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Run all available migrations
	err = dbx.MigrateDB("appdb",
		dbx.CreateWithDriverName(dbx.DriverSQLite),
		dbx.CreateWithDbFolder(tmpDir),
		dbx.WithSource(exampleMigrations),
		dbx.WithSrcFolder("testmigrations"),
	)
	if err != nil {
		log.Fatal(err)
	}

	ver, err := dbx.MigrationVersion("appdb",
		dbx.CreateWithDbFolder(tmpDir),
		dbx.WithSource(exampleMigrations),
		dbx.WithSrcFolder("testmigrations"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Migrated database to version: %d\n", ver)
	// Output:
	// Migrated database to version: 3
}

func ExampleMigrateUpTo() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_upto")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Migrate forward to version 1 only
	err = dbx.MigrateUpTo("appdb", 1,
		dbx.CreateWithDriverName(dbx.DriverSQLite),
		dbx.CreateWithDbFolder(tmpDir),
		dbx.WithSource(exampleMigrations),
		dbx.WithSrcFolder("testmigrations"),
	)
	if err != nil {
		log.Fatal(err)
	}

	ver, err := dbx.MigrationVersion("appdb",
		dbx.CreateWithDbFolder(tmpDir),
		dbx.WithSource(exampleMigrations),
		dbx.WithSrcFolder("testmigrations"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Targeted migration version: %d\n", ver)
	// Output:
	// Targeted migration version: 1
}

func ExampleRollbackMigration() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_rollback")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	opts := []dbx.CreateOptFn{
		dbx.CreateWithDriverName(dbx.DriverSQLite),
		dbx.CreateWithDbFolder(tmpDir),
		dbx.WithSource(exampleMigrations),
		dbx.WithSrcFolder("testmigrations"),
	}

	// Apply all migrations (version 3)
	_ = dbx.MigrateDB("appdb", opts...)

	// Roll back 1 migration step (version 2)
	err = dbx.RollbackMigration("appdb", opts...)
	if err != nil {
		log.Fatal(err)
	}

	ver, err := dbx.MigrationVersion("appdb", opts...)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Version after rollback: %d\n", ver)
	// Output:
	// Version after rollback: 2
}

func ExampleCache() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_cache")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Pre-create database for tenant_1
	_ = dbx.CreateDB("tenant_1", dbx.CreateWithDbFolder(tmpDir))

	cache := dbx.NewCache(10 * time.Minute)
	defer cache.Close()

	// GetOrOpen acquires an existing connection or opens a new one
	db, err := cache.GetOrOpen("tenant_1",
		dbx.WithDbFolder(tmpDir),
		dbx.WithDriverName(dbx.DriverSQLite),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Check if cache contains the tenant
	hasTenant := cache.Has("tenant_1") != nil

	fmt.Printf("Tenant connection active: %t (ping: %v)\n", hasTenant, db.Ping() == nil)
	// Output:
	// Tenant connection active: true (ping: true)
}

func ExampleCache_Delete() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_cache_del")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	_ = dbx.CreateDB("tenant_to_evict", dbx.CreateWithDbFolder(tmpDir))

	cache := dbx.NewCache(10 * time.Minute)
	defer cache.Close()

	_, _ = cache.GetOrOpen("tenant_to_evict", dbx.WithDbFolder(tmpDir))

	// Evict and close the tenant connection
	err = cache.Delete("tenant_to_evict")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Tenant in cache after delete: %t\n", cache.Has("tenant_to_evict") != nil)
	// Output:
	// Tenant in cache after delete: false
}

func ExampleTransact() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_tx")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	_ = dbx.CreateDB("txdb",
		dbx.CreateWithDbFolder(tmpDir),
		dbx.WithSource(exampleMigrations),
		dbx.WithSrcFolder("testmigrations"),
	)

	db, err := dbx.OpenDB("txdb", dbx.WithDbFolder(tmpDir))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	tx, err := dbx.NewTransact(ctx, db)
	if err != nil {
		log.Fatal(err)
	}

	// Execute transaction with nested savepoint
	err = tx.Transaction(nil, func(txCtx context.Context) error {
		// Outer insert
		_, err := tx.Db().ExecContext(txCtx, "INSERT INTO items(name) VALUES (?)", "Item 1")
		if err != nil {
			return err
		}

		// Nested transaction (savepoint)
		return tx.Transaction(nil, func(nestedCtx context.Context) error {
			_, err := tx.Db().ExecContext(nestedCtx, "INSERT INTO items(name) VALUES (?)", "Item 2")
			return err
		})
	})
	if err != nil {
		log.Fatal(err)
	}

	var count int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM items").Scan(&count)
	fmt.Printf("Committed items: %d\n", count)
	// Output:
	// Committed items: 2
}

func ExampleTableExists() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_tbl")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	_ = dbx.CreateDB("tbldb",
		dbx.CreateWithDbFolder(tmpDir),
		dbx.WithSource(exampleMigrations),
		dbx.WithSrcFolder("testmigrations"),
	)

	db, err := dbx.OpenDB(filepath.Join(tmpDir, "tbldb"), dbx.WithDbFolder(tmpDir))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	exists, err := dbx.TableExists(ctx, db, "items")
	if err != nil {
		log.Fatal(err)
	}

	notExists, err := dbx.TableExists(ctx, db, "nonexistent_table")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("items table exists: %t, nonexistent table exists: %t\n", exists, notExists)
	// Output:
	// items table exists: true, nonexistent table exists: false
}
