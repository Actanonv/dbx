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
func CreateDB(dsn string, opts ...CreateOptFn) (err error) {
	option := CreateOptions{}
	setCreateOptions(&option, opts...)

	// keep original dsn for migration step
	origDSN := dsn

	if option.driverName == DriverSQLite {
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
	if db != nil {
		db.Close()
	}

	if option.source != nil {
		// Run migrations using the original DSN (not the file: DSN)
		if err = MigrateDB(origDSN, opts...); err != nil {
			return err
		}
	}

	return nil
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
	if opt.dbFolder == "" && opt.driverName == DriverSQLite {
		CreateWithDbFolder("./data")(opt)
	}
}
