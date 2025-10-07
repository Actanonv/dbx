package dbx

import (
	"context"
	"database/sql"
	"embed"
	"os"
	"path/filepath"
	"testing"
)

//go:embed testmigrations/*.sql
var testMigrations embed.FS

func TestDbFilePath(t *testing.T) {
	type args struct {
		name     string
		dbFolder string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "with db folder",
			args: args{
				name:     "test",
				dbFolder: "db",
			},
			want: "db/test.db",
		},
		{
			name: "without db folder",
			args: args{
				name:     "test",
				dbFolder: "",
			},
			want: "test.db",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := DbFilePath(tt.args.name, tt.args.dbFolder)
			if got != tt.want {
				t.Errorf("DbFilePath() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenDB_SQLitePragmas(t *testing.T) {
	tmp := t.TempDir()

	// ensure sqlite file exists via helper
	dsn := filepath.Join(tmp, "opendbtest")
	if _, err := createSQLiteDBFile(dsn, tmp); err != nil {
		t.Fatalf("createSQLiteDBFile failed: %v", err)
	}

	db, err := OpenDB(dsn, WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	// Verify WAL mode
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&mode); err != nil {
		t.Fatalf("query PRAGMA journal_mode failed: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", mode)
	}

	// Verify foreign_keys is ON (1)
	var fk int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&fk); err != nil {
		t.Fatalf("query PRAGMA foreign_keys failed: %v", err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}
}

func TestMigrateDB_RunsMigrations(t *testing.T) {
	tmp := t.TempDir()
	name := "migratedbtest"

	// Run migrations using embedded SQL files
	if err := MigrateDB(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(testMigrations),
		CreateWithSrcFolder("testmigrations"),
	); err != nil {
		t.Fatalf("MigrateDB failed: %v", err)
	}

	// Open the DB and verify the table exists and is usable
	db, err := OpenDB(filepath.Join(tmp, name), WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("OpenDB after migration failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	// Verify table exists via sqlite_master
	var tbl string
	q := "SELECT name FROM sqlite_master WHERE type='table' AND name='items'"
	if err := db.QueryRowContext(ctx, q).Scan(&tbl); err != nil {
		if err == sql.ErrNoRows {
			t.Fatalf("items table not found after migration")
		}
		t.Fatalf("query sqlite_master failed: %v", err)
	}
	if tbl != "items" {
		t.Fatalf("expected table name 'items', got %q", tbl)
	}

	// Insert a row to ensure table is functional
	if _, err := db.ExecContext(ctx, "INSERT INTO items(name) VALUES (?)", "foo"); err != nil {
		t.Fatalf("insert after migration failed: %v", err)
	}
}

func TestCreateDB_CreatesFileAndRunsMigrations(t *testing.T) {
	tmp := t.TempDir()
	name := "createdbtest"

	// Create DB which should also run migrations
	if err := CreateDB(name,
		CreateWithDriverName(DriverSQLite),
		CreateWithDbFolder(tmp),
		CreateWithSource(testMigrations),
		CreateWithSrcFolder("testmigrations"),
	); err != nil {
		t.Fatalf("CreateDB failed: %v", err)
	}

	// DB file should exist
	dbFile := filepath.Join(tmp, name+".db")
	if _, err := os.Stat(dbFile); err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("expected db file to be created: %s", dbFile)
		}
		t.Fatalf("stat db file failed: %v", err)
	}

	// Open and verify migration effects
	db, err := OpenDB(filepath.Join(tmp, name), WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("OpenDB after CreateDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	var tbl string
	q := "SELECT name FROM sqlite_master WHERE type='table' AND name='items'"
	if err := db.QueryRowContext(ctx, q).Scan(&tbl); err != nil {
		if err == sql.ErrNoRows {
			t.Fatalf("items table not found after CreateDB")
		}
		t.Fatalf("query sqlite_master failed: %v", err)
	}
	if tbl != "items" {
		t.Fatalf("expected table name 'items', got %q", tbl)
	}

	if _, err := db.ExecContext(ctx, "INSERT INTO items(name) VALUES (?)", "bar"); err != nil {
		t.Fatalf("insert after CreateDB failed: %v", err)
	}
}
func TestTableExists(t *testing.T) {
	tmp := t.TempDir()
	name := "tableexiststest"

	// Create DB
	if err := CreateDB(name, CreateWithDriverName(DriverSQLite), CreateWithDbFolder(tmp)); err != nil {
		t.Fatalf("CreateDB failed: %v", err)
	}

	// Open the DB
	db, err := OpenDB(filepath.Join(tmp, name), WithDbFolder(tmp), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	// Create a test table
	_, err = db.ExecContext(ctx, "CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	tests := []struct {
		name      string
		tableName string
		want      bool
		wantErr   bool
	}{
		{
			name:      "existing table",
			tableName: "test_table",
			want:      true,
			wantErr:   false,
		},
		{
			name:      "nonexistent table",
			tableName: "nonexistent_table",
			want:      false,
			wantErr:   false,
		},
		{
			name:      "table with quotes",
			tableName: "\"test_table\"",
			want:      true,
			wantErr:   false,
		},
		{
			name:      "table with single quotes",
			tableName: "'test_table'",
			want:      true,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TableExists(ctx, db, tt.tableName)
			if (err != nil) != tt.wantErr {
				t.Errorf("TableExists() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("TableExists() got = %v, want %v", got, tt.want)
			}
		})
	}
}
