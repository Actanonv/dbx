package dbx

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
	noLog           bool
}
type OpenOptFn func(options *Options)

func WithDriverName(dn DriverName) OpenOptFn {
	return func(opt *Options) {
		opt.driverName = string(dn)
	}
}

func WithLog(log bool) OpenOptFn {
	return func(opt *Options) {
		opt.noLog = log
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
	if IsSQLite(driver) {
		dbFile, err := DbFilePath(dsn, opt.dbFolder)
		if err != nil {
			return nil, err
		}

		if driver == DriverSQLite {
			dsn = "file:" + dbFile +
				"?_journal_mode=WAL" +
				"&_synchronous=NORMAL" +
				"&_busy_timeout=5000" +
				"&_foreign_keys=on" +
				"&_cache_size=-65536" +
				"&cache=private"
		} else {
			dsn = "file:" + dbFile +
				"?_pragma=journal_mode(WAL)" +
				"&_pragma=synchronous(NORMAL)" +
				"&_pragma=busy_timeout(5000)" +
				"&_pragma=foreign_keys(ON)" +
				"&_pragma=cache_size(-65536)" +
				"&_pragma=temp_store(MEMORY)"
		}
	}

	db, err := sql.Open(opt.driverName, dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(opt.maxOpenConns)
	db.SetMaxIdleConns(opt.maxIdleConns)
	db.SetConnMaxLifetime(opt.connMaxLifetime)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	if IsSQLite(driver) && driver == DriverSQLite {
		if _, err = db.Exec(`PRAGMA temp_store = MEMORY;`); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}
	}

	bunDB := bun.NewDB(db, sqlitedialect.New(), bun.WithDiscardUnknownColumns())
	if !opt.noLog {
		bunDB.AddQueryHook(bundebug.NewQueryHook(
			bundebug.WithVerbose(true),
			// bundebug.FromEnv("BUN_DEBUG")
		))
	}

	return bunDB, nil
}

func setOptions(opt *Options, opts ...OpenOptFn) {

	// Apply all options
	for _, optFn := range opts {
		optFn(opt)
	}

	if opt.driverName == "" {
		WithDriverName(DriverSQLite)(opt)
	}

	if opt.maxIdleConns == 0 {
		if IsSQLite(DriverName(opt.driverName)) {
			WithMaxIdleConns(1)(opt)
		} else {
			WithMaxIdleConns(2)(opt)
		}
	}
	if opt.maxOpenConns == 0 {
		if IsSQLite(DriverName(opt.driverName)) {
			WithMaxOpenConns(1)(opt)
		} else {
			WithMaxOpenConns(10)(opt)
		}
	}

	if opt.dbFolder == "" && IsSQLite(DriverName(opt.driverName)) {
		WithDbFolder("./data")(opt)
	}
}
