package db

import (
	"database/sql"
	"embed"
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
func MigrateDB(dsn string, driverName DriverName, srcFolder string, source embed.FS) (err error) {
	if driverName == DriverSQLite {
		dbFile, err := createSQLiteDBFile(dsn)
		if err != nil {
			return err
		}

		dsn = fmt.Sprintf("file:%s", dbFile)
	}

	db, err := sql.Open(string(driverName), dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return err
	}

	if driverName == DriverSQLite {
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

	goose.SetBaseFS(source)
	if err := goose.SetDialect(string(driverName)); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}
	if err := goose.Up(db, srcFolder); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
