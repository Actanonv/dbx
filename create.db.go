package dbx

import (
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
)

// CreateOptions configures database creation and migration behavior.
type CreateOptions struct {
	driverName DriverName
	dbFolder   string
	source     *embed.FS
	srcFolder  string
}

// CreateOptFn is a functional option for configuring CreateDB and MigrateDB.
type CreateOptFn func(options *CreateOptions)

// CreateDB creates a new database specified by the dsn and optionally runs migrations.
//
// For SQLite, if the database file does not exist, it will be created in the specified
// dbFolder (defaulting to "./data") with optimized WAL mode and pragma configurations.
// If a migration source is provided (via WithSource or CreateWithSource), all migrations
// in the specified folder will be executed.
//
// Example:
//
//	err := dbx.CreateDB("myapp",
//	    dbx.WithDriverName(dbx.DriverSQLite),
//	    dbx.WithDbFolder("./data"),
//	    dbx.WithSource(migrationsFS),
//	    dbx.WithSrcFolder("migrations"),
//	)
func CreateDB(dsn string, opts ...CreateOptFn) error {
	option := CreateOptions{}
	setCreateOptions(&option, opts...)

	// If no source is provided, we just want to ensure the database can be opened (and file created for SQLite)
	if option.source == nil {
		if IsSQLite(option.driverName) {
			dbFile, err := createSQLiteDBFile(dsn, option.dbFolder)
			if err != nil {
				return err
			}
			dsn = fmt.Sprintf("file:%s", dbFile)
		}

		db, err := sql.Open(string(option.driverName), dsn)
		if err != nil {
			return err
		}
		defer db.Close()

		if err := db.Ping(); err != nil {
			return err
		}

		if IsSQLite(option.driverName) {
			if _, err := db.Exec(`
				PRAGMA journal_mode = WAL;
				PRAGMA synchronous = NORMAL;
				PRAGMA busy_timeout = 5000;
				PRAGMA foreign_keys = ON;
				PRAGMA cache_size = -65536;
				PRAGMA temp_store = MEMORY;
			`); err != nil {
				return fmt.Errorf("failed to configure sqlite: %w", err)
			}
		}

		return nil
	}

	// Run migrations (that also includes opening/pinging the DB)
	return MigrateDB(dsn, opts...)
}

// CreateWithDriverName specifies the database driver to use (default: DriverSQLite).
func CreateWithDriverName(dn DriverName) CreateOptFn {
	return func(opt *CreateOptions) {
		opt.driverName = dn
	}
}

// CreateWithDbFolder specifies the directory containing SQLite database files (default: "./data").
func CreateWithDbFolder(nme string) CreateOptFn {
	return func(opt *CreateOptions) {
		opt.dbFolder = filepath.Clean(nme)
	}
}

// CreateWithSource specifies the embedded filesystem containing migration SQL files.
func CreateWithSource(fs embed.FS) CreateOptFn {
	return func(opt *CreateOptions) {
		opt.source = &fs
	}
}

// CreateWithSrcFolder specifies the subdirectory within the embed.FS containing migration SQL files.
func CreateWithSrcFolder(n string) CreateOptFn {
	return func(opt *CreateOptions) {
		opt.srcFolder = n
	}
}

// WithSource is an alias for CreateWithSource to provide consistent option naming across dbx functions.
func WithSource(fs embed.FS) CreateOptFn {
	return CreateWithSource(fs)
}

// WithSrcFolder is an alias for CreateWithSrcFolder to provide consistent option naming across dbx functions.
func WithSrcFolder(n string) CreateOptFn {
	return CreateWithSrcFolder(n)
}

func setCreateOptions(opt *CreateOptions, opts ...CreateOptFn) {
	// Apply all options
	for _, optFn := range opts {
		optFn(opt)
	}

	if opt.driverName == "" {
		CreateWithDriverName(DriverSQLite)(opt)
	}
	if opt.dbFolder == "" && IsSQLite(opt.driverName) {
		CreateWithDbFolder("./data")(opt)
	}
}
