package dbx

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mssqldialect"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/extra/bundebug"
	"github.com/uptrace/bun/schema"
)

// Options holds configuration settings for opening database connections.
type Options struct {
	driverName      string
	dbFolder        string
	maxOpenConns    int
	maxIdleConns    int
	connMaxIdleTime time.Duration
	connMaxLifetime time.Duration
	logQueries      bool
}

// OpenOptFn is a functional option for configuring OpenDB.
type OpenOptFn func(options *Options)

// WithDriverName specifies the database driver to use (default: DriverSQLite).
func WithDriverName(dn DriverName) OpenOptFn {
	return func(opt *Options) {
		opt.driverName = string(dn)
	}
}

// WithLog enables or disables verbose SQL query logging via bundebug.
func WithLog(log bool) OpenOptFn {
	return func(opt *Options) {
		opt.logQueries = log
	}
}

// WithDbFolder specifies the directory where SQLite database files are stored (default: "./data").
func WithDbFolder(nme string) OpenOptFn {
	return func(opt *Options) {
		opt.dbFolder = filepath.Clean(nme)
	}
}

// WithMaxOpenConns sets the maximum number of open connections in the pool.
func WithMaxOpenConns(n int) OpenOptFn {
	return func(opt *Options) {
		opt.maxOpenConns = n
	}
}

// WithMaxIdleConns sets the maximum number of idle connections in the pool.
func WithMaxIdleConns(n int) OpenOptFn {
	return func(opt *Options) {
		opt.maxIdleConns = n
	}
}

// WithConnMaxIdleTime sets the maximum amount of time a connection may be idle before being closed.
func WithConnMaxIdleTime(n time.Duration) OpenOptFn {
	return func(opt *Options) {
		opt.connMaxIdleTime = n
	}
}

// WithConnMaxLifetime sets the maximum amount of time a connection may be reused.
func WithConnMaxLifetime(d time.Duration) OpenOptFn {
	return func(opt *Options) {
		opt.connMaxLifetime = d
	}
}

func bunDialect(driver DriverName) schema.Dialect {
	switch {
	case IsSQLite(driver):
		return sqlitedialect.New()
	case driver == DriverPostgres || driver == DriverPgx:
		return pgdialect.New()
	case driver == DriverMySQL:
		return mysqldialect.New()
	case driver == DriverMSSQL:
		return mssqldialect.New()
	default:
		return sqlitedialect.New()
	}
}

// OpenDB opens a new database connection and wraps it in a *bun.DB instance.
//
// For SQLite, dsn should be the database file name (e.g. "myapp" or "myapp.db").
// OpenDB automatically applies WAL mode, foreign keys, synchronous=NORMAL,
// busy timeouts, and SQLite-tailored connection pool settings.
//
// For PostgreSQL, MySQL, or MSSQL, dsn is the connection string/URI.
//
// Example:
//
//	db, err := dbx.OpenDB("myapp",
//	    dbx.WithDriverName(dbx.DriverSQLite),
//	    dbx.WithDbFolder("./data"),
//	)
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
				"&_cache_size=-4096" +
				"&cache=private"
		} else {
			dsn = "file:" + dbFile +
				"?_pragma=journal_mode(WAL)" +
				"&_pragma=synchronous(NORMAL)" +
				"&_pragma=busy_timeout(5000)" +
				"&_pragma=foreign_keys(ON)" +
				"&_pragma=cache_size(-4096)" +
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
	db.SetConnMaxIdleTime(opt.connMaxIdleTime)

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

	bunDB := bun.NewDB(db, bunDialect(driver), bun.WithDiscardUnknownColumns())
	if opt.logQueries {
		bunDB.AddQueryHook(bundebug.NewQueryHook(
			bundebug.WithVerbose(true),
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

	if opt.maxOpenConns == 0 {
		if IsSQLite(DriverName(opt.driverName)) {
			WithMaxOpenConns(1)(opt)
		} else {
			WithMaxOpenConns(10)(opt)
		}
	}

	if opt.maxIdleConns == 0 {
		if IsSQLite(DriverName(opt.driverName)) {
			WithMaxIdleConns(1)(opt)
		} else {
			WithMaxIdleConns(2)(opt)
		}
	}

	if opt.connMaxIdleTime == 0 {
		if IsSQLite(DriverName(opt.driverName)) {
			WithConnMaxIdleTime(15 * time.Minute)(opt)
		}
	}

	if opt.connMaxLifetime == 0 {
		if IsSQLite(DriverName(opt.driverName)) {
			WithConnMaxLifetime(0)(opt)
		}
	}

	if opt.dbFolder == "" && IsSQLite(DriverName(opt.driverName)) {
		WithDbFolder("./data")(opt)
	}
}
