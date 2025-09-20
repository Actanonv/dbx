package db

import (
	"database/sql"
	"embed"
	"fmt"
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
		dbFile, err := createSQLiteDBFile(opts.DSN, opts.DbFolder)
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
