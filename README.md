# dbx

[![Go Reference](https://pkg.go.dev/badge/github.com/actanonv/dbx.svg)](https://pkg.go.dev/github.com/actanonv/dbx)
[![Go Report Card](https://goreportcard.com/badge/github.com/actanonv/dbx)](https://goreportcard.com/report/github.com/actanonv/dbx)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/actanonv/dbx)](https://golang.org)

`dbx` is a Go package providing robust database management functionality, including connection pooling, multi-tenant caching, embedded schema migration lifecycle management, and nested transaction handling. It is built on top of the [Bun ORM](https://bun.uptrace.dev/) and provides high-level abstractions for common database workflows.

## Features

- **Optimized SQLite Support**: Automatic configuration with WAL mode, `synchronous=NORMAL`, busy timeouts, in-memory temp stores, and SQLite-tailored connection pooling.
- **Multi-Driver Dialect Support**: Built-in support for SQLite (both `mattn/go-sqlite3` and pure-Go `modernc.org/sqlite`), PostgreSQL (`postgres` / `pgx`), MySQL, and Microsoft SQL Server (`mssql`).
- **Connection Caching**: Thread-safe `Cache` with per-key double-checked locking (thundering-herd protection), background eviction of inactive connections, and manual single-tenant eviction (`Delete`).
- **Complete Migration Lifecycle**: Embedded migration management powered by [Goose](https://github.com/pressly/goose) with support for stepwise forward migration, rollbacks, targeted down migrations, resets, and version inspection.
- **Nested Transactions**: Stateful `Transact` manager supporting arbitrary levels of nested transactions using database savepoints with automatic panic recovery and rollback.
- **Bun ORM Integration**: Returns standard `*bun.DB` instances and `bun.IDB` executors for query execution.

## Installation

```bash
go get github.com/actanonv/dbx
```

## Quick Start

### Opening a Database Connection

`dbx.OpenDB` handles driver-specific configurations and sets up connection pooling.

```go
import "github.com/actanonv/dbx"

// Open a SQLite database (file will be located in ./data/myapp.db)
db, err := dbx.OpenDB("myapp", 
    dbx.WithDriverName(dbx.DriverSQLite),
    dbx.WithDbFolder("./data"),
)
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### Database Migrations Lifecycle

Manage embedded migrations from an `embed.FS` filesystem:

```go
//go:embed migrations/*.sql
var migrations embed.FS

// Run all pending up migrations
err := dbx.MigrateDB("myapp",
    dbx.WithSource(migrations),
    dbx.WithSrcFolder("migrations"),
    dbx.WithDbFolder("./data"),
)

// Check current migration version
ver, err := dbx.MigrationVersion("myapp",
    dbx.WithSource(migrations),
    dbx.WithSrcFolder("migrations"),
    dbx.WithDbFolder("./data"),
)

// Roll back 1 migration step
err = dbx.RollbackMigration("myapp",
    dbx.WithSource(migrations),
    dbx.WithSrcFolder("migrations"),
    dbx.WithDbFolder("./data"),
)

// Migrate forward up to a specific version
err = dbx.MigrateUpTo("myapp", 2,
    dbx.WithSource(migrations),
    dbx.WithSrcFolder("migrations"),
    dbx.WithDbFolder("./data"),
)
```

### Using the Connection Cache (Multi-Tenancy)

The `Cache` manages multiple database connections efficiently, which is ideal for multi-tenant applications:

```go
cache := dbx.NewCache(30 * time.Minute) // Inactive connections cleaned up after 30m
defer cache.Close()

// GetOrOpen acquires an existing connection or opens a new one
db, err := cache.GetOrOpen("tenant_1", 
    dbx.WithDbFolder("./tenants"),
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
- `WithMaxOpenConns(n)`: Set maximum open connections.
- `WithMaxIdleConns(n)`: Set maximum idle connections.
- `WithConnMaxIdleTime(d)`: Set maximum idle connection duration.
- `WithConnMaxLifetime(d)`: Set maximum connection lifetime.
- `WithLog(bool)`: Enable verbose SQL query logging.

### Create & Migration Options (`CreateOptFn`)
- `WithDriverName(name)` / `CreateWithDriverName(name)`: Specify the driver for migrations.
- `WithDbFolder(path)` / `CreateWithDbFolder(path)`: Folder for SQLite database files.
- `WithSource(fs)` / `CreateWithSource(fs)`: `embed.FS` containing migration SQL files.
- `WithSrcFolder(path)` / `CreateWithSrcFolder(path)`: Path within the `embed.FS` where migrations are located.

## License

[MIT](LICENSE)

