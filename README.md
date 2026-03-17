# dbx

`dbx` is a Go package that provides robust database management functionality, including connection pooling, caching, migration support, and optimized SQLite configuration. It is built on top of the [Bun ORM](https://bun.uptrace.dev/) and provides high-level abstractions for common database operations.

## Features

- **Optimized SQLite Support**: Automatic configuration with WAL mode, synchronous=NORMAL, and connection pooling settings tailored for SQLite.
- **Connection Caching**: Built-in cache for database connections with automatic cleanup of inactive connections.
- **Migration Support**: Seamless integration with [goose](https://github.com/pressly/goose) for running migrations from embedded filesystems.
- **Robust Transactions**: Simple API for managing transactions, including support for **nested transactions** via savepoints.
- **Bun ORM Integration**: Returns `*bun.DB` instances, allowing you to use all the power of the Bun ORM.
- **Multi-Driver Support**: Compatible with SQLite (modern `sqlite` and `mattn/go-sqlite3`), PostgreSQL, MySQL, and MSSQL.

## Installation

```bash
go get github.com/akinmayowa/dbx
```

## Quick Start

### Opening a Database Connection

`dbx.OpenDB` handles driver-specific configurations and sets up connection pooling.

```go
import "github.com/akinmayowa/dbx"

// Open a SQLite database
db, err := dbx.OpenDB("myapp", 
    dbx.WithDriverName(dbx.DriverSQLite),
    dbx.WithDbFolder("./data"),
)
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### Database Migrations

You can easily run migrations using an embedded filesystem.

```go
//go:embed migrations/*.sql
var migrations embed.FS

err := dbx.MigrateDB("myapp",
    dbx.CreateWithSource(migrations),
    dbx.CreateWithSrcFolder("migrations"),
)
```

### Using the Connection Cache

The `Cache` allows you to manage multiple database connections efficiently, which is useful in multi-tenant applications.

```go
cache := dbx.NewCache(30 * time.Minute) // Cleanup connections inactive for 30m
defer cache.Close()

// GetOrOpen will return an existing connection or open a new one
db, err := cache.GetOrOpen("tenant_1", 
    dbx.WithDbFolder("./tenants"),
)
```

### Transaction Management

The `Transact` helper simplifies transaction handling and supports nesting.

```go
t, err := dbx.NewTransact(db)
if err != nil {
    log.Fatal(err)
}

err = t.Transaction(ctx, nil, func(ctx context.Context) error {
    // Perform operations using t.Db()
    _, err := t.Db().NewInsert().Model(&item).Exec(ctx)
    if err != nil {
        return err // Will trigger rollback
    }

    // Nested transaction (uses savepoints)
    return t.Transaction(ctx, nil, func(ctx context.Context) error {
        // ... nested operations
        return nil
    })
})
```

## Configuration Options

### Open Options (`OpenOptFn`)
- `WithDriverName(name)`: Specify the database driver (default: `DriverSQLite`).
- `WithDbFolder(path)`: Folder for SQLite database files (default: `./data`).
- `WithMaxOpenConns(n)`: Set maximum open connections.
- `WithMaxIdleConns(n)`: Set maximum idle connections.
- `WithConnMaxLifetime(d)`: Set maximum connection lifetime.

### Create Options (`CreateOptFn`)
- `CreateWithDriverName(name)`: Specify the driver for migrations.
- `CreateWithDbFolder(path)`: Folder for SQLite database files.
- `CreateWithSource(fs)`: `embed.FS` containing migration files.
- `CreateWithSrcFolder(path)`: Path within the `embed.FS` where migrations are located.

## License

[MIT](LICENSE)
