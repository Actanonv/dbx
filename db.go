package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"embed"
	"strings"
)

var (
	dbFolder string
)

func CreateDB(dsn string, driverName DriverName, srcFolder string, source embed.FS) (err error) {
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
	if db != nil {
		db.Close()
	}

	if err = MigrateDB(dsn, driverName, srcFolder, source); err != nil {
		return err
	}

	return nil
}

var ErrDBFileNotFound = errors.New("db file not found")

// dbFilePath converts a name into a full path to the db including the file extension
func dbFilePath(name string) (string, error) {
	name = filepath.Clean(name + "db")
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

func createSQLiteDBFile(name string) (dbFile string, err error) {
	dbFile, err = dbFilePath(name)
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
