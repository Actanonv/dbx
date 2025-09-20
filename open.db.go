package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/extra/bundebug"
)

type Options struct {
	driverName      string
	dbFolder        string
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
}
type OpenOptFn func(options *Options)

func WithDriverName(dn DriverName) OpenOptFn {
	return func(opt *Options) {
		opt.driverName = string(dn)
	}
}

func WithDbFolder(nme string) OpenOptFn {
	return func(opt *Options) {
		opt.dbFolder = filepath.Clean(nme)
	}
}

func WithMaxOpenConns(n int) OpenOptFn {
	return func(opt *Options) {
		opt.maxOpenConns = n
	}
}

func WithMaxIdleConns(n int) OpenOptFn {
	return func(opt *Options) {
		opt.maxIdleConns = n
	}
}

func WithConnMaxLifetime(d time.Duration) OpenOptFn {
	return func(opt *Options) {
		opt.connMaxLifetime = d
	}
}

// OpenDB opens a new database connection.
// for sqlite, dsn should be a file name (without extension)
func OpenDB(dsn string, opts ...OpenOptFn) (*bun.DB, error) {
	var opt Options
	setOptions(&opt, opts...)
	driver := DriverName(opt.driverName)

	if driver == DriverSQLite {
		dbFile, err := DbFilePath(dsn, opt.dbFolder)
		if err != nil {
			return nil, err
		}

		dsn = fmt.Sprintf("file:%s?_journal=WAL&mode=rwc&busy=2000&_foreign_keys=1", dbFile)
	}

	db, err := sql.Open(opt.driverName, dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	if driver == DriverSQLite {
		_, err = db.Exec("PRAGMA journal_mode=WAL;")
		if err != nil {
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}

		if _, err = db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
			return nil, fmt.Errorf("failed to enable foreign keys mode: %w", err)
		}
	}

	db.SetMaxOpenConns(opt.maxOpenConns)
	db.SetMaxIdleConns(opt.maxIdleConns)
	db.SetConnMaxLifetime(opt.connMaxLifetime)

	bunDB := bun.NewDB(db, sqlitedialect.New(), bun.WithDiscardUnknownColumns())
	bunDB.AddQueryHook(bundebug.NewQueryHook(
		bundebug.WithVerbose(true),
		//bundebug.FromEnv("BUN_DEBUG")
	))

	return bunDB, nil
}

func setOptions(opt *Options, opts ...OpenOptFn) {
	if len(opts) == 0 {
		opts = []OpenOptFn{
			WithDriverName(DriverSQLite),
			WithMaxOpenConns(1),
			WithMaxIdleConns(1),
			WithConnMaxLifetime(0),
		}
	}
	// Apply all options
	for _, optFn := range opts {
		optFn(opt)
	}

	if opt.dbFolder == "" && opt.driverName == string(DriverSQLite) {
		opt.dbFolder = "./data"
	}
}
