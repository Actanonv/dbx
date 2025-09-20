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

type CreateOptions struct {
	DSN        string
	DriverName DriverName
	DbFolder   string
	Source     embed.FS
	SrcFolder  string
}

func CreateDB(opts CreateOptions) (err error) {
	dsn := opts.DSN
	if opts.DriverName == DriverSQLite {
		dbFile, err := createSQLiteDBFile(opts.DSN, "./data")
		if err != nil {
			return err
		}

		dsn = fmt.Sprintf("file:%s", dbFile)
	}

	db, err := sql.Open(string(opts.DriverName), dsn)
	if err != nil {
		return err
	}
	if db != nil {
		db.Close()
	}

	if err = MigrateDB(opts); err != nil {
		return err
	}

	return nil
}

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
