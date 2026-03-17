package dbx

import (
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
)

type CreateOptions struct {
	driverName DriverName
	dbFolder   string
	source     *embed.FS
	srcFolder  string
}

type CreateOptFn func(options *CreateOptions)

// CreateDB creates a new database specified by the dsn and runs migrations.
// Provides the following options:
//
//   - CreateWithDriverName(driverName DriverName) - specify the database driver (default: DriverSQLite)
//   - CreateWithDbFolder(folder string) - specify the folder to create the SQLite database file in (default: "./data")
//   - CreateWithSource(fs embed.FS) - specify the embedded filesystem containing migration files
//   - CreateWithSrcFolder(folder string) - specify the folder within the embedded filesystem containing migration files
//
// For SQLite, if the database file already exists, it will not be overwritten.
// For other databases, ensure that the user has the necessary permissions to create a new database.
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

func CreateWithDriverName(dn DriverName) CreateOptFn {
	return func(opt *CreateOptions) {
		opt.driverName = dn
	}
}

func CreateWithDbFolder(nme string) CreateOptFn {
	return func(opt *CreateOptions) {
		opt.dbFolder = filepath.Clean(nme)
	}
}

func CreateWithSource(fs embed.FS) CreateOptFn {
	return func(opt *CreateOptions) {
		opt.source = &fs
	}
}

func CreateWithSrcFolder(n string) CreateOptFn {
	return func(opt *CreateOptions) {
		opt.srcFolder = n
	}
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
