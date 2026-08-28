// Package dbx provides high-level database connection management, connection pooling,
// multi-tenant caching, SQLite WAL optimization, and nested transaction management
// built on top of the Bun ORM (https://bun.uptrace.dev/).
//
// # Core Features
//
//   - Optimized SQLite: Automatic configuration with WAL mode, synchronous=NORMAL,
//     busy timeouts, in-memory temp stores, auto-creation of directories/files,
//     and connection pooling tailored for SQLite.
//   - Multi-Driver Support: Seamless support for SQLite (both cgo mattn/go-sqlite3 and
//     pure-Go modernc.org/sqlite), PostgreSQL, MySQL, and Microsoft SQL Server.
//   - Connection Caching: Built-in thread-safe Cache for managing multiple database
//     connections with per-key thundering-herd protection, automatic background eviction
//     of inactive connections, and single-tenant eviction (Delete).
//   - Lifecycle Initialization: WithOnInit hook for executing caller-driven schema bootstrap
//     (e.g., schemagen or custom DDL) safely inside the thundering-herd lock before connections
//     are returned or cached.
//   - Robust Transactions: Stateful Transact manager supporting arbitrary levels of nested
//     transactions via database savepoints with automatic panic recovery.
//   - DSN & Path Utilities: Helper functions including BuildDSN, DbFilePath, and TableExists.
//
// # Quick Start
//
// To open a database connection:
//
//	db, err := dbx.OpenDB("myapp",
//	    dbx.WithDriverName(dbx.DriverSQLite),
//	    dbx.WithDbFolder("./data"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer db.Close()
//
// # Schema Initialization with WithOnInit
//
// You can supply an initialization callback to run schema setup (e.g., via schemagen)
// immediately upon opening:
//
//	db, err := dbx.OpenDB("myapp",
//	    dbx.WithDriverName(dbx.DriverSQLite),
//	    dbx.WithOnInit(func(d *bun.DB) error {
//	        _, err := d.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS users (id INT PRIMARY KEY)")
//	        return err
//	    }),
//	)
//
// # Multi-Tenant Database Caching
//
// In multi-tenant systems, the Cache manages multiple tenant database connections efficiently:
//
//	cache := dbx.NewCache(30 * time.Minute)
//	defer cache.Close()
//
//	// Get existing connection or open a new one with thundering-herd protection
//	tenantDB, err := cache.GetOrOpen("tenant_42",
//	    dbx.WithDbFolder("./tenants"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Close and evict a specific tenant when unneeded
//	_ = cache.Delete("tenant_42")
//
// # Nested Transactions
//
// The Transact helper simplifies transaction handling and transparently uses savepoints
// when transactions are nested:
//
//	tx, err := dbx.NewTransact(ctx, db)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	err = tx.Transaction(nil, func(ctx context.Context) error {
//	    // Outer transaction operations
//	    _, err := tx.Db().ExecContext(ctx, "INSERT INTO items(name) VALUES (?)", "Item 1")
//	    if err != nil {
//	        return err
//	    }
//
//	    // Nested transaction (backed by a savepoint)
//	    return tx.Transaction(nil, func(ctx context.Context) error {
//	        _, err := tx.Db().ExecContext(ctx, "INSERT INTO items(name) VALUES (?)", "Item 2")
//	        return err
//	    })
//	})
package dbx
