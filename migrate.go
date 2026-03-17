package dbx

import (
	"database/sql"
	"fmt"
	"github.com/pressly/goose/v3"
)

type DriverName string

const (
	DriverSQLite      DriverName = "sqlite"
	DriverSQLiteMattn DriverName = "sqlite3"
	DriverPostgres    DriverName = "postgres"
	DriverPgx         DriverName = "pgx"
	DriverMySQL       DriverName = "mysql"
	DriverMSSQL       DriverName = "mssql"
)

func IsSQLite(dn DriverName) bool {
	return dn == DriverSQLite || dn == DriverSQLiteMattn
}

// MigrateDB runs migrations on the db
func MigrateDB(dsn string, opts ...CreateOptFn) (err error) {
	option := CreateOptions{}
	setCreateOptions(&option, opts...)

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
		if _, err = db.Exec(`
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

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	goose.SetBaseFS(option.source)
	if err := goose.SetDialect(string(option.driverName)); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}
	if err := goose.Up(db, option.srcFolder); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
