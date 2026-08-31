package dbx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/uptrace/bun"
	_ "modernc.org/sqlite"
)

func TestDbFilePath(t *testing.T) {
	tmp := t.TempDir()
	type args struct {
		name     string
		dbFolder string
	}
	tests := []struct {
		name     string
		args     args
		wantName string
	}{
		{
			name: "with db folder",
			args: args{
				name:     "test",
				dbFolder: tmp,
			},
			wantName: "test.db",
		},
		{
			name: "without db folder",
			args: args{
				name:     "test",
				dbFolder: "",
			},
			wantName: "test.db",
		},
		{
			name: "with explicit extension",
			args: args{
				name:     "custom.db",
				dbFolder: tmp,
			},
			wantName: "custom.db",
		},
		{
			name: "with subfolder path",
			args: args{
				name:     "nested/tenant",
				dbFolder: tmp,
			},
			wantName: "tenant.db",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DbFilePath(tt.args.name, tt.args.dbFolder)
			if err != nil {
				t.Fatalf("DbFilePath() unexpected error: %v", err)
			}
			if !strings.HasSuffix(got, tt.wantName) {
				t.Errorf("DbFilePath() got = %v, want suffix %v", got, tt.wantName)
			}
		})
	}
}

func TestBuildDSN(t *testing.T) {
	tmp := t.TempDir()

	t.Run("sqlite mattn", func(t *testing.T) {
		dsn, err := BuildDSN("app", WithDbFolder(tmp), WithDriverName(DriverSQLite))
		if err != nil {
			t.Fatalf("BuildDSN failed: %v", err)
		}
		if !strings.HasPrefix(dsn, "file:") || !strings.Contains(dsn, "_journal_mode=WAL") {
			t.Errorf("unexpected DSN for SQLite: %s", dsn)
		}
	})

	t.Run("sqlite modernc", func(t *testing.T) {
		dsn, err := BuildDSN("app", WithDbFolder(tmp), WithDriverName(DriverSQLiteMc))
		if err != nil {
			t.Fatalf("BuildDSN failed: %v", err)
		}
		if !strings.HasPrefix(dsn, "file:") || !strings.Contains(dsn, "_pragma=journal_mode(WAL)") {
			t.Errorf("unexpected DSN for modernc SQLite: %s", dsn)
		}
	})

	t.Run("postgres", func(t *testing.T) {
		orig := "postgres://user:pass@localhost:5432/mydb?sslmode=disable"
		dsn, err := BuildDSN(orig, WithDriverName(DriverPostgres))
		if err != nil {
			t.Fatalf("BuildDSN failed: %v", err)
		}
		if dsn != orig {
			t.Errorf("BuildDSN() = %s; want %s", dsn, orig)
		}
	})
}

func TestOpenDB_AutoCreateAndPragmas(t *testing.T) {
	tmp := t.TempDir()
	nestedFolder := filepath.Join(tmp, "nested", "data")
	name := "autocreatedb"

	// OpenDB on a non-existent database file and non-existent parent directory
	db, err := OpenDB(name, WithDbFolder(nestedFolder), WithDriverName(DriverSQLite))
	if err != nil {
		t.Fatalf("OpenDB auto-create failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Verify file was created
	dbFile := filepath.Join(nestedFolder, name+".db")
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("expected db file to exist at %s: %v", dbFile, err)
	}

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

func TestOpenDB_WithOnInit(t *testing.T) {
	tmp := t.TempDir()

	t.Run("success", func(t *testing.T) {
		ctx := context.Background()
		initExecuted := false

		db, err := OpenDB("init_success",
			WithDbFolder(tmp),
			WithDriverName(DriverSQLite),
			WithOnInit(func(db *bun.DB) error {
				initExecuted = true
				_, err := db.ExecContext(ctx, "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)")
				return err
			}),
		)
		if err != nil {
			t.Fatalf("OpenDB with OnInit failed: %v", err)
		}
		defer db.Close()

		if !initExecuted {
			t.Fatalf("expected onInit callback to execute")
		}

		exists, err := TableExists(ctx, db, "users")
		if err != nil {
			t.Fatalf("TableExists failed: %v", err)
		}
		if !exists {
			t.Fatalf("expected users table to exist after onInit")
		}
	})

	t.Run("error rolls back and closes db", func(t *testing.T) {
		expectedErr := errors.New("schema bootstrap failed")

		db, err := OpenDB("init_fail",
			WithDbFolder(tmp),
			WithDriverName(DriverSQLite),
			WithOnInit(func(db *bun.DB) error {
				return expectedErr
			}),
		)
		if err == nil {
			db.Close()
			t.Fatalf("expected error from failed onInit, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected error to wrap %v, got %v", expectedErr, err)
		}
	})
}

func TestOpenDB_ModernC(t *testing.T) {
	tmp := t.TempDir()
	name := "modernc_db"

	db, err := OpenDB(name, WithDbFolder(tmp), WithDriverName(DriverSQLiteMc))
	if err != nil {
		t.Fatalf("OpenDB with modernc failed: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestOpenDB_Errors(t *testing.T) {
	// Invalid driver should return error
	_, err := OpenDB("invalid_driver_db", WithDriverName("invalid_nonexistent_driver"))
	if err == nil {
		t.Fatal("expected error for invalid driver, got nil")
	}
}

func TestTableExists(t *testing.T) {
	tmp := t.TempDir()
	name := "tableexiststest"
	ctx := context.Background()

	db, err := OpenDB(name,
		WithDbFolder(tmp),
		WithDriverName(DriverSQLite),
		WithOnInit(func(d *bun.DB) error {
			_, err := d.ExecContext(ctx, "CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
			return err
		}),
	)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

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

func TestBunDialect_Mapping(t *testing.T) {
	tests := []struct {
		driver DriverName
		want   string
	}{
		{DriverSQLite, "sqlite"},
		{DriverSQLiteMc, "sqlite"},
		{DriverPostgres, "pg"},
		{DriverPgx, "pg"},
		{DriverMySQL, "mysql"},
		{DriverMSSQL, "mssql"},
		{DriverName("unknown"), "sqlite"},
	}

	for _, tt := range tests {
		d := bunDialect(tt.driver)
		if d == nil {
			t.Fatalf("expected non-nil dialect for %s", tt.driver)
		}
		if d.Name().String() != tt.want {
			t.Errorf("bunDialect(%s).Name() = %s; want %s", tt.driver, d.Name().String(), tt.want)
		}
	}
}


