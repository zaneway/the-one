package sqlite

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/zaneway/the-one/internal/config"
)

func TestOpenRunsMigrationAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	cfg := config.StorageConfig{
		Backend:          "sqlite",
		Path:             dbPath,
		SQLiteVecEnabled: "auto",
		BusyTimeoutMS:    1000,
	}
	logger := slog.New(slog.DiscardHandler)

	store, err := Open(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	status := store.Status()
	wantVersion := 5
	if status.Migrations.CurrentVersion != wantVersion {
		t.Fatalf("current version = %d, want %d", status.Migrations.CurrentVersion, wantVersion)
	}
	if status.Migrations.Dirty {
		t.Fatal("migration status dirty, want false")
	}
	if !status.Capabilities.SQLite {
		t.Fatal("sqlite capability false, want true")
	}
	for _, table := range []string{
		"agent_session",
		"agent_task",
		"raw_event",
		"async_job",
		"memory_candidate",
		"memory_relation",
	} {
		if !tableExists(t, store, table) {
			t.Fatalf("table %s does not exist after migration", table)
		}
	}
	for table, columns := range map[string][]string{
		"async_job":        {"job_type", "target_type", "dedup_key", "payload_json"},
		"memory_candidate": {"source_evidence_ids_json", "review_checkpoint_json", "admission_decision", "dedup_key"},
		"memory_relation":  {"source_id", "target_id", "relation_type", "weight"},
	} {
		for _, column := range columns {
			if !columnExists(t, store, table, column) {
				t.Fatalf("column %s.%s does not exist after migration", table, column)
			}
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("Open() second time error = %v", err)
	}
	defer reopened.Close()
	if reopened.Status().Migrations.CurrentVersion != wantVersion {
		t.Fatalf("current version after reopen = %d, want %d", reopened.Status().Migrations.CurrentVersion, wantVersion)
	}
}

func tableExists(t *testing.T, store *Store, table string) bool {
	t.Helper()
	var count int
	err := store.db.QueryRow("select count(*) from sqlite_master where type = 'table' and name = ?", table).Scan(&count)
	if err != nil {
		t.Fatalf("query sqlite_master for %s: %v", table, err)
	}
	return count == 1
}

func columnExists(t *testing.T, store *Store, table, column string) bool {
	t.Helper()
	rows, err := store.db.Query("pragma table_info(" + table + ")")
	if err != nil {
		t.Fatalf("query table_info for %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("scan table_info for %s: %v", table, err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info for %s: %v", table, err)
	}
	return false
}
