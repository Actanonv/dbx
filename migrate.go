package dbx

import (
	"database/sql"
	"fmt"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

type DriverName string

const (
	DriverSQLite   DriverName = "sqlite"
	DriverPostgres DriverName = "postgres"
	DriverPgx      DriverName = "pgx"
	DriverMySQL    DriverName = "mysql"
	DriverMSSQL    DriverName = "mssql"
)

// MigrateDB runs migrations on the db
func MigrateDB(dsn string, opts ...CreateOptFn) (err error) {
	option := CreateOptions{}
	setCreateOptions(&option, opts...)

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
	defer db.Close()

	if err := db.Ping(); err != nil {
		return err
	}

	if option.driverName == DriverSQLite {
		_, err = db.Exec("PRAGMA journal_mode=WAL;")
		if err != nil {
			return fmt.Errorf("failed to enable WAL mode: %w", err)
		}

		if _, err = db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
			return fmt.Errorf("failed to enable foreign keys mode: %w", err)
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
