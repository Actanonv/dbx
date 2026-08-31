# dbx

[![Go Reference](https://pkg.go.dev/badge/github.com/actanonv/dbx/v2.svg)](https://pkg.go.dev/github.com/actanonv/dbx/v2)
[![Go Report Card](https://goreportcard.com/badge/github.com/actanonv/dbx/v2)](https://goreportcard.com/report/github.com/actanonv/dbx/v2)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/actanonv/dbx)](https://golang.org)

`dbx` is a Go package providing robust database connection management, connection pooling, multi-tenant caching, SQLite WAL optimization, and nested transaction handling. It is built on top of the [Bun ORM](https://bun.uptrace.dev/) and provides high-level abstractions for common database workflows.

## Features

- **Optimized SQLite Support**: Automatic configuration with WAL mode, `synchronous=NORMAL`, busy timeouts, in-memory temp stores, auto-creation of directories and files, and SQLite-tailored connection pooling.
- **Multi-Driver Dialect Support**: Built-in support for SQLite (both `mattn/go-sqlite3` and pure-Go `modernc.org/sqlite`), PostgreSQL (`postgres` / `pgx`), MySQL, and Microsoft SQL Server (`mssql`).
- **Connection Caching**: Thread-safe `Cache` with per-key double-checked locking (thundering-herd protection), background eviction of inactive connections, and manual single-tenant eviction (`Delete`).
- **Lifecycle Initialization (`WithOnInit`)**: Safely execute caller-driven schema bootstrap (such as `schemagen` or custom DDL) inside the connection lock before queries or caching occur.
- **Nested Transactions**: Stateful `Transact` manager supporting arbitrary levels of nested transactions using database savepoints with automatic panic recovery and rollback.
- **Bun ORM Integration**: Returns standard `*bun.DB` instances and `bun.IDB` executors for query execution.

## Installation

```bash
go get github.com/actanonv/dbx/v2
```

## Breaking Changes in v2.0.0

`dbx` v2.0.0 streamlines the package scope by focusing strictly on database connection pooling, multi-tenant caching, SQLite WAL optimization, and nested transactions:

- **Decoupled Database Drivers**: SQLite drivers (`github.com/mattn/go-sqlite3` and `modernc.org/sqlite`) are no longer bundled as direct dependencies of `dbx`. Applications importing `dbx` should explicitly register their preferred database driver via a blank import (e.g. `_ "github.com/mattn/go-sqlite3"` or `_ "modernc.org/sqlite"`).
- **Migration Engine Decoupled**: All embedded Goose migration APIs (`MigrateDB`, `MigrateUpTo`, `MigrateDown`, `RollbackMigration`, `MigrateDownTo`, `ResetMigrations`, `MigrationVersion`, `MigrationStatus`) and the `goose` dependency have been removed. Schema management is delegated to external tools or Go libraries (e.g. `schemagen`).
- **`CreateDB` Removed**: `CreateDB`, `CreateOptions`, `CreateOptFn`, `WithSource`, and `WithSrcFolder` are removed. `OpenDB` now automatically creates parent directories and SQLite database files on demand.
- **`WithOnInit` Lifecycle Hook**: To initialize schemas or bootstrap tables dynamically (e.g., inside `Cache.GetOrOpen`), use the new `WithOnInit(func(db *bun.DB) error)` option.
- **Helper Utilities**: `BuildDSN(name, opts...)` and `DbFilePath(name, folder)` are now exported for DSN construction and path resolution.

## Quick Start

### Opening a Database Connection

`dbx.OpenDB` handles driver-specific configurations, auto-creates parent folders/files for SQLite, and sets up connection pooling. Make sure to register your driver of choice in your application (e.g., `_ "github.com/mattn/go-sqlite3"` or `_ "modernc.org/sqlite"`):

```go
import (
    "context"
    "log"

    "github.com/actanonv/dbx/v2"
    _ "github.com/mattn/go-sqlite3" // register driver in application
    "github.com/uptrace/bun"
)

// Open a SQLite database (creates ./data/myapp.db if it doesn't exist and enables WAL)
db, err := dbx.OpenDB("myapp", 
    dbx.WithDriverName(dbx.DriverSQLite),
    dbx.WithDbFolder("./data"),
)
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### Schema Initialization (`WithOnInit`)

Use `WithOnInit` to run schema generation or custom DDL safely when a database is opened:

```go
db, err := dbx.OpenDB("myapp",
    dbx.WithDriverName(dbx.DriverSQLite),
    dbx.WithDbFolder("./data"),
    dbx.WithOnInit(func(d *bun.DB) error {
        _, err := d.ExecContext(context.Background(), `
            CREATE TABLE IF NOT EXISTS users (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                email TEXT NOT NULL UNIQUE
            );
        `)
        return err
    }),
)
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### Using the Connection Cache (Multi-Tenancy)

The `Cache` manages multiple database connections efficiently, which is ideal for multi-tenant applications:

```go
cache := dbx.NewCache(30 * time.Minute) // Inactive connections cleaned up after 30m
defer cache.Close()

// GetOrOpen acquires an existing connection or opens/creates a new one with thundering-herd protection
db, err := cache.GetOrOpen("tenant_1", 
    dbx.WithDbFolder("./tenants"),
    dbx.WithOnInit(func(d *bun.DB) error {
        // Run schemagen / initial schema bootstrap inside the cache lock
        return bootstrapTenantSchema(d)
    }),
)
if err != nil {
    log.Fatal(err)
}

// Manually evict and close a tenant connection
err = cache.Delete("tenant_1")
```

### Nested Transaction Management

The `Transact` helper simplifies transaction management and supports nested savepoints:

```go
ctx := context.Background()
tx, err := dbx.NewTransact(ctx, db)
if err != nil {
    log.Fatal(err)
}

err = tx.Transaction(nil, func(txCtx context.Context) error {
    // Outer transaction operations using tx.Db()
    _, err := tx.Db().NewInsert().Model(&item).Exec(txCtx)
    if err != nil {
        return err // Will trigger rollback
    }

    // Nested transaction (transparently backed by a savepoint)
    return tx.Transaction(nil, func(nestedCtx context.Context) error {
        _, err := tx.Db().NewInsert().Model(&order).Exec(nestedCtx)
        return err
    })
})
```

## Configuration Options

### Open Options (`OpenOptFn`)
- `WithDriverName(name)`: Specify the database driver (default: `DriverSQLite`).
- `WithDbFolder(path)`: Directory for SQLite database files (default: `./data`).
- `WithOnInit(fn)`: Lifecycle hook `func(db *bun.DB) error` executed before returning/caching the connection.
- `WithMaxOpenConns(n)`: Set maximum open connections.
- `WithMaxIdleConns(n)`: Set maximum idle connections.
- `WithConnMaxIdleTime(d)`: Set maximum idle connection duration.
- `WithConnMaxLifetime(d)`: Set maximum connection lifetime.
- `WithLog(bool)`: Enable verbose SQL query logging via `bundebug`.

### Helper Utilities
- `BuildDSN(name, opts...)`: Construct fully resolved DSN string including recommended SQLite WAL pragmas.
- `DbFilePath(name, folder)`: Resolve absolute `.db` path and ensure parent directories exist.
- `TableExists(ctx, db, tableName)`: Check if a table exists across SQLite, PostgreSQL, or MySQL.

## License

[MIT](LICENSE)


