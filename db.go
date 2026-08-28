package dbx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uptrace/bun"
)

// DbFilePath converts a database name into a full absolute path to the SQLite database file.
// It ensures that the parent directory exists on disk.
func DbFilePath(name, dbFolder string) (string, error) {
	name = filepath.Clean(name)
	if filepath.Ext(name) == "" {
		name += ".db"
	}

	dbf := filepath.Clean(dbFolder)
	if after, ok := strings.CutPrefix(name, dbf); ok {
		name = strings.TrimPrefix(after, string(filepath.Separator))
	}

	dbFile := filepath.Join(dbf, name)
	dir := filepath.Dir(dbFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create db directory (%s): %w", dir, err)
	}

	return filepath.Abs(dbFile)
}

// TableExists checks if a table exists in the database.
// Supports SQLite, PostgreSQL, and MySQL.
func TableExists(ctx context.Context, db *bun.DB, tableName string) (bool, error) {
	// Normalize table name (strip quotes/backticks if any)
	tableName = strings.Trim(tableName, `"'`)

	// Get current dialect name
	dName := db.Dialect().Name()

	var query string
	switch dName := DriverName(dName.String()); {
	case IsSQLite(dName):
		query = `SELECT name FROM sqlite_master WHERE type='table' AND name = ?`
	case dName == DriverPostgres || dName == DriverPgx:
		query = `SELECT to_regclass(?)`
	case dName == DriverMySQL:
		query = `SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`
	default:
		return false, fmt.Errorf("unsupported dialect: %s", dName)
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
