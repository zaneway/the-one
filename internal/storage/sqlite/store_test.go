package sqlite

import (
	"context"
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
	wantVersion := 4
	if status.Migrations.CurrentVersion != wantVersion {
		t.Fatalf("current version = %d, want %d", status.Migrations.CurrentVersion, wantVersion)
	}
	if status.Migrations.Dirty {
		t.Fatal("migration status dirty, want false")
	}
	if !status.Capabilities.SQLite {
		t.Fatal("sqlite capability false, want true")
	}
	for _, table := range []string{"agent_session", "agent_task", "raw_event"} {
		if !tableExists(t, store, table) {
			t.Fatalf("table %s does not exist after migration", table)
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
