// Package dbx provides high-level database management, connection pooling,
// multi-tenant caching, embedded schema migrations, and nested transaction management
// built on top of the Bun ORM (https://bun.uptrace.dev/).
//
// # Core Features
//
//   - Optimized SQLite: Automatic configuration with WAL mode, synchronous=NORMAL,
//     busy timeouts, in-memory temp stores, and connection pooling tailored for SQLite.
//   - Multi-Driver Support: Seamless support for SQLite (both cgo mattn/go-sqlite3 and
//     pure-Go modernc.org/sqlite), PostgreSQL, MySQL, and Microsoft SQL Server.
//   - Connection Caching: Built-in thread-safe Cache for managing multiple database
//     connections with automatic background eviction of inactive connections and
//     single-tenant eviction (Delete).
//   - Migration Lifecycle: Embedded migration execution and complete lifecycle management
//     (MigrateDB, MigrateUpTo, MigrateDown, RollbackMigration, ResetMigrations, MigrationVersion)
//     powered by Goose.
//   - Robust Transactions: Transaction management supporting arbitrary levels of nested
//     transactions via database savepoints with automatic panic recovery.
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
// # Multi-Tenant Database Caching
//
// In multi-tenant systems, the Cache manages multiple tenant database files efficiently:
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
// # Embedded Migrations
//
// Schema migrations can be executed directly from embedded SQL files:
//
//	//go:embed migrations/*.sql
//	var migrations embed.FS
//
//	err := dbx.MigrateDB("myapp",
//	    dbx.WithSource(migrations),
//	    dbx.WithSrcFolder("migrations"),
//	    dbx.WithDbFolder("./data"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
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
