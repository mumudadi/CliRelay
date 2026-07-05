package sqliteinventory

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	postgresstore "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/postgres"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	_ "modernc.org/sqlite"
)

func TestImportSQLiteDryRunAndApply(t *testing.T) {
	dsn := os.Getenv("CLIRELAY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("CLIRELAY_POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	pgDB, err := postgresstore.OpenRuntimeDB(ctx, config.PostgresConfig{DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 1})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer pgDB.Close()
	if _, err := pgDB.Exec(`
		TRUNCATE
			request_log_content,
			request_logs,
			api_keys,
			api_key_permission_profiles
		RESTART IDENTITY CASCADE
	`); err != nil {
		t.Fatalf("truncate postgres: %v", err)
	}

	sqlitePath := filepath.Join(t.TempDir(), "usage.db")
	sqliteDB, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := sqliteDB.Exec(`
		CREATE TABLE api_key_permission_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			allowed_models TEXT NOT NULL DEFAULT '[]'
		);
		CREATE TABLE api_keys (
			key TEXT PRIMARY KEY,
			id TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			allowed_models TEXT NOT NULL DEFAULT '[]'
		);
		CREATE TABLE request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL,
			api_key TEXT NOT NULL,
			api_key_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			failed INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE request_log_content (
			log_id INTEGER PRIMARY KEY,
			timestamp DATETIME NOT NULL,
			compression TEXT NOT NULL DEFAULT 'zstd',
			input_content BLOB NOT NULL DEFAULT X'',
			output_content BLOB NOT NULL DEFAULT X'',
			detail_content BLOB NOT NULL DEFAULT X''
		);
		INSERT INTO api_key_permission_profiles (id, name, allowed_models)
		VALUES ('profile-fixture', 'Fixture', '["gpt-test"]');
		INSERT INTO api_keys (key, id, name, allowed_models)
		VALUES ('fixture-key-a', 'key-a', 'Key A', '["gpt-test"]');
		INSERT INTO request_logs (id, timestamp, api_key, api_key_id, model, failed, total_tokens)
		VALUES (7, '2026-07-05T01:00:00Z', 'fixture-key-a', 'key-a', 'gpt-test', 0, 11);
		INSERT INTO request_log_content (log_id, timestamp, input_content, output_content, detail_content)
		VALUES (7, '2026-07-05T01:00:00Z', X'7B7D', X'7B226F6B223A747275657D', X'7B2264657461696C223A747275657D');
	`); err != nil {
		t.Fatalf("seed sqlite: %v", err)
	}
	if err := sqliteDB.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	opts := ImportOptions{
		SQLitePath:  sqlitePath,
		PostgresDSN: dsn,
		DryRun:      true,
		Now:         time.Date(2026, 7, 5, 4, 0, 0, 0, time.UTC),
	}
	dryRun, err := Import(ctx, opts)
	if err != nil {
		t.Fatalf("dry-run import: %v", err)
	}
	if !dryRun.DryRun || findImportTable(dryRun.Tables, "request_logs") == nil {
		t.Fatalf("dry-run report = %#v", dryRun)
	}
	if got := findImportTable(dryRun.Tables, "request_logs"); got.SourceRows != 1 || got.TargetRows != 0 || got.SourceChecksum == "" {
		t.Fatalf("request_logs dry-run = %#v", got)
	}

	opts.DryRun = false
	applied, err := Import(ctx, opts)
	if err != nil {
		t.Fatalf("apply import: %v", err)
	}
	if got := findImportTable(applied.Tables, "request_logs"); got == nil || got.InsertedRows != 1 || !got.SequenceReset {
		t.Fatalf("request_logs applied = %#v", got)
	}
	var count int
	if err := pgDB.QueryRow("SELECT COUNT(*) FROM request_logs WHERE id = 7 AND api_key = 'fixture-key-a'").Scan(&count); err != nil {
		t.Fatalf("count imported request log: %v", err)
	}
	if count != 1 {
		t.Fatalf("imported request log count = %d", count)
	}
}

func findImportTable(rows []ImportTableReport, name string) *ImportTableReport {
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	return nil
}
