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

// DriverName represents supported database driver identifiers.
// Note: Per Go standard library conventions, database drivers must be registered
// by the calling application via blank import (e.g. `_ "github.com/mattn/go-sqlite3"`
// or `_ "modernc.org/sqlite"`).
type DriverName string

const (
	// DriverSQLiteMc is the pure-Go SQLite driver identifier ("sqlite", modernc.org/sqlite).
	DriverSQLiteMc DriverName = "sqlite"
	// DriverSQLite is the cgo SQLite driver identifier ("sqlite3", github.com/mattn/go-sqlite3).
	DriverSQLite DriverName = "sqlite3"
	// DriverPostgres is the PostgreSQL standard driver identifier (postgres).
	DriverPostgres DriverName = "postgres"
	// DriverPgx is the pgx PostgreSQL driver identifier (pgx).
	DriverPgx DriverName = "pgx"
	// DriverMySQL is the MySQL driver identifier (mysql).
	DriverMySQL DriverName = "mysql"
	// DriverMSSQL is the Microsoft SQL Server driver identifier (mssql).
	DriverMSSQL DriverName = "mssql"
)

// IsSQLite returns true if the driver is a SQLite variant.
func IsSQLite(dn DriverName) bool {
	return dn == DriverSQLiteMc || dn == DriverSQLite
}

// Options holds configuration settings for opening database connections.
type Options struct {
	driverName      string
	dbFolder        string
	maxOpenConns    int
	maxIdleConns    int
	connMaxIdleTime time.Duration
	connMaxLifetime time.Duration
	logQueries      bool
	onInit          func(*bun.DB) error
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

// WithOnInit registers an initialization hook executed immediately after the database connection
// is opened, pinged, and configured with pragmas, but before it is returned or cached.
// If fn returns an error, the database connection is closed and OpenDB returns the error.
func WithOnInit(fn func(db *bun.DB) error) OpenOptFn {
	return func(opt *Options) {
		opt.onInit = fn
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

// BuildDSN constructs the full connection string / DSN based on provided options.
// For SQLite, it resolves the file path, ensures parent directories exist, and appends recommended WAL pragmas.
// For PostgreSQL, MySQL, and MSSQL, it returns the provided connection string.
func BuildDSN(name string, opts ...OpenOptFn) (string, error) {
	var opt Options
	setOptions(&opt, opts...)
	driver := DriverName(opt.driverName)
	if IsSQLite(driver) {
		dbFile, err := DbFilePath(name, opt.dbFolder)
		if err != nil {
			return "", err
		}

		if driver == DriverSQLite {
			return "file:" + dbFile +
				"?_journal_mode=WAL" +
				"&_synchronous=NORMAL" +
				"&_busy_timeout=5000" +
				"&_foreign_keys=on" +
				"&_cache_size=-4096" +
				"&cache=private", nil
		}
		return "file:" + dbFile +
			"?_pragma=journal_mode(WAL)" +
			"&_pragma=synchronous(NORMAL)" +
			"&_pragma=busy_timeout(5000)" +
			"&_pragma=foreign_keys(ON)" +
			"&_pragma=cache_size(-4096)" +
			"&_pragma=temp_store(MEMORY)", nil
	}
	return name, nil
}

// OpenDB opens a database connection and wraps it in a *bun.DB instance.
//
// Note: The calling application must register the required database driver via a blank import
// (e.g. `_ "github.com/mattn/go-sqlite3"` or `_ "modernc.org/sqlite"` for SQLite).
//
// For SQLite, dsn should be the database file name (e.g. "myapp" or "myapp.db").
// OpenDB automatically creates parent directories and the database file if needed,
// applies WAL mode, foreign keys, synchronous=NORMAL, busy timeouts, and SQLite-tailored pool limits.
//
// For PostgreSQL, MySQL, or MSSQL, dsn is the connection string/URI.
//
// If WithOnInit is provided, the callback executes after the connection is verified.
// If the callback returns an error, the connection is closed and the error is returned.
//
// Example:
//
//	import _ "github.com/mattn/go-sqlite3" // register driver in calling app
//
//	db, err := dbx.OpenDB("myapp",
//	    dbx.WithDriverName(dbx.DriverSQLite),
//	    dbx.WithDbFolder("./data"),
//	)
func OpenDB(dsn string, opts ...OpenOptFn) (*bun.DB, error) {
	var opt Options
	setOptions(&opt, opts...)
	driver := DriverName(opt.driverName)

	fullDSN, err := BuildDSN(dsn, opts...)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(opt.driverName, fullDSN)
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
			return nil, fmt.Errorf("failed to configure SQLite temp_store: %w", err)
		}
	}

	bunDB := bun.NewDB(db, bunDialect(driver), bun.WithDiscardUnknownColumns())
	if opt.logQueries {
		bunDB.AddQueryHook(bundebug.NewQueryHook(
			bundebug.WithVerbose(true),
		))
	}

	if opt.onInit != nil {
		if err := opt.onInit(bunDB); err != nil {
			_ = bunDB.Close()
			return nil, fmt.Errorf("failed to initialize database: %w", err)
		}
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
