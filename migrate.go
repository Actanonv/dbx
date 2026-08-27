package dbx

import (
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

type DriverName string

var migrationMu sync.Mutex

const (
	DriverSQLiteMc DriverName = "sqlite"
	DriverSQLite   DriverName = "sqlite3"
	DriverPostgres DriverName = "postgres"
	DriverPgx      DriverName = "pgx"
	DriverMySQL    DriverName = "mysql"
	DriverMSSQL    DriverName = "mssql"
)

func IsSQLite(dn DriverName) bool {
	return dn == DriverSQLiteMc || dn == DriverSQLite
}

func gooseDialect(dn DriverName) string {
	switch {
	case IsSQLite(dn):
		return "sqlite3"
	case dn == DriverPostgres || dn == DriverPgx:
		return "postgres"
	case dn == DriverMySQL:
		return "mysql"
	case dn == DriverMSSQL:
		return "mssql"
	default:
		return string(dn)
	}
}

func openMigrationDB(dsn string, opts ...CreateOptFn) (*sql.DB, CreateOptions, error) {
	option := CreateOptions{}
	setCreateOptions(&option, opts...)

	if IsSQLite(option.driverName) {
		dbFile, err := createSQLiteDBFile(dsn, option.dbFolder)
		if err != nil {
			return nil, option, err
		}

		dsn = fmt.Sprintf("file:%s", dbFile)
	}

	db, err := sql.Open(string(option.driverName), dsn)
	if err != nil {
		return nil, option, err
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, option, err
	}

	if IsSQLite(option.driverName) {
		if _, err = db.Exec(`
			PRAGMA journal_mode = WAL;
			PRAGMA synchronous = NORMAL;
			PRAGMA busy_timeout = 5000;
			PRAGMA foreign_keys = ON;
			PRAGMA cache_size = -65536;
			PRAGMA temp_store = MEMORY;
		`); err != nil {
			_ = db.Close()
			return nil, option, fmt.Errorf("failed to configure sqlite: %w", err)
		}
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	goose.SetBaseFS(option.source)
	if err := goose.SetDialect(gooseDialect(option.driverName)); err != nil {
		_ = db.Close()
		return nil, option, fmt.Errorf("failed to set dialect: %w", err)
	}

	return db, option, nil
}

// MigrateDB runs all available up migrations on the db
func MigrateDB(dsn string, opts ...CreateOptFn) (err error) {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	db, option, err := openMigrationDB(dsn, opts...)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.Up(db, option.srcFolder); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// MigrateUpTo runs up migrations up to the specified version
func MigrateUpTo(dsn string, version int64, opts ...CreateOptFn) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	db, option, err := openMigrationDB(dsn, opts...)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.UpTo(db, option.srcFolder, version); err != nil {
		return fmt.Errorf("failed to run up-to migrations: %w", err)
	}

	return nil
}

// RollbackMigration rolls back the most recently applied migration
func RollbackMigration(dsn string, opts ...CreateOptFn) error {
	return MigrateDown(dsn, opts...)
}

// MigrateDown rolls back the most recently applied migration
func MigrateDown(dsn string, opts ...CreateOptFn) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	db, option, err := openMigrationDB(dsn, opts...)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.Down(db, option.srcFolder); err != nil {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	return nil
}

// MigrateDownTo rolls back migrations down to the specified version
func MigrateDownTo(dsn string, version int64, opts ...CreateOptFn) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	db, option, err := openMigrationDB(dsn, opts...)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.DownTo(db, option.srcFolder, version); err != nil {
		return fmt.Errorf("failed to rollback down-to migration: %w", err)
	}

	return nil
}

// ResetMigrations rolls back all applied migrations
func ResetMigrations(dsn string, opts ...CreateOptFn) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	db, option, err := openMigrationDB(dsn, opts...)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.Reset(db, option.srcFolder); err != nil {
		return fmt.Errorf("failed to reset migrations: %w", err)
	}

	return nil
}

// MigrationVersion returns the current migration version of the database
func MigrationVersion(dsn string, opts ...CreateOptFn) (int64, error) {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	db, _, err := openMigrationDB(dsn, opts...)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	version, err := goose.GetDBVersion(db)
	if err != nil {
		return 0, fmt.Errorf("failed to get migration version: %w", err)
	}

	return version, nil
}

// MigrationStatus prints the status of migrations
func MigrationStatus(dsn string, opts ...CreateOptFn) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	db, option, err := openMigrationDB(dsn, opts...)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.Status(db, option.srcFolder); err != nil {
		return fmt.Errorf("failed to get migration status: %w", err)
	}

	return nil
}
