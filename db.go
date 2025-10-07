package dbx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uptrace/bun"
)

var ErrDBFileNotFound = errors.New("db file not found")

// DbFilePath converts a name into a full path to the db including the file extension
func DbFilePath(name, dbFolder string) (string, error) {
	name = filepath.Clean(name)
	if filepath.Ext(name) == "" {
		name += ".db"
	}

	dbf := filepath.Clean(dbFolder)
	if strings.HasPrefix(name, dbf) {
		name = strings.TrimPrefix(name, dbf)
	}

	dbFile := filepath.Join(dbf, name)
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		return dbFile, fmt.Errorf("%w: %s", ErrDBFileNotFound, dbFile)
	}

	return dbFile, nil
}

func createSQLiteDBFile(name, dbFolder string) (dbFile string, err error) {
	dbFile, err = DbFilePath(name, dbFolder)
	if err != nil && !errors.Is(err, ErrDBFileNotFound) {
		return "", err
	}
	if errors.Is(err, ErrDBFileNotFound) {
		var dbFh *os.File
		if dbFh, err = os.Create(dbFile); err != nil {
			return "", fmt.Errorf("failed to create db file(%s): %w", dbFile, err)
		}
		defer func() {
			if dbFh != nil {
				dbFh.Close()
			}
		}()
	}

	return dbFile, nil
}

// TableExists checks if a table exists in the database
func TableExists(ctx context.Context, db *bun.DB, tableName string) (bool, error) {
	// Normalize table name (strip quotes/backticks if any)
	tableName = strings.Trim(tableName, `"'`)

	// Get current dialect
	dialect := db.Dialect().Name()

	var query string
	switch DriverName(dialect) {
	case DriverSQLite:
		query = `SELECT name FROM sqlite_master WHERE type='table' AND name = ?`
	case DriverPostgres, DriverPgx:
		query = `SELECT to_regclass(?)`
	case DriverMySQL:
		query = `SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`
	default:
		return false, fmt.Errorf("unsupported dialect: %s", dialect)
	}

	var result string
	err := db.NewRaw(query, tableName).Scan(ctx, &result)
	if err != nil {
		// Bun returns sql.ErrNoRows if not found — treat as "does not exist"
		if err.Error() == "sql: no rows in result set" {
			return false, nil
		}
		return false, err
	}

	return result != "", nil
}
