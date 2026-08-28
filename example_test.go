package dbx_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/actanonv/dbx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/uptrace/bun"
)

func ExampleOpenDB() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_open")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Open the database (automatically creates the SQLite file and applies WAL pragmas)
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

func ExampleWithOnInit() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_oninit")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Open the database and execute schema initialization safely before the DB is returned
	db, err := dbx.OpenDB("store",
		dbx.WithDriverName(dbx.DriverSQLite),
		dbx.WithDbFolder(tmpDir),
		dbx.WithOnInit(func(d *bun.DB) error {
			_, err := d.ExecContext(ctx, "CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT)")
			return err
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	exists, err := dbx.TableExists(ctx, db, "products")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Products table created: %t\n", exists)
	// Output:
	// Products table created: true
}

func ExampleBuildDSN() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_dsn")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dsn, err := dbx.BuildDSN("myapp",
		dbx.WithDriverName(dbx.DriverSQLite),
		dbx.WithDbFolder(tmpDir),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("DSN prefix: %s\n", dsn[:5])
	// Output:
	// DSN prefix: file:
}

func ExampleCache() {
	tmpDir, err := os.MkdirTemp("", "dbx_example_cache")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cache := dbx.NewCache(10 * time.Minute)
	defer cache.Close()

	// GetOrOpen acquires an existing connection or opens and creates a new one
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

	ctx := context.Background()

	db, err := dbx.OpenDB("txdb",
		dbx.WithDbFolder(tmpDir),
		dbx.WithOnInit(func(d *bun.DB) error {
			_, err := d.ExecContext(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)")
			return err
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

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

	ctx := context.Background()

	db, err := dbx.OpenDB(filepath.Join(tmpDir, "tbldb"),
		dbx.WithDbFolder(tmpDir),
		dbx.WithOnInit(func(d *bun.DB) error {
			_, err := d.ExecContext(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)")
			return err
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

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
