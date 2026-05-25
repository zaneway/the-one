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
	wantVersion := 7
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
		"retrieval_trace",
		"memory_access_log",
		"code_ref",
		"memory_embedding",
		"doc_snapshot",
		"doc_section_snapshot",
		"mvp_acceptance_run",
		"mvp_acceptance_task",
		"mvp_metric_sample",
		"mvp_agent_capability",
	} {
		if !tableExists(t, store, table) {
			t.Fatalf("table %s does not exist after migration", table)
		}
	}
	for table, columns := range map[string][]string{
		"async_job":        {"job_type", "target_type", "dedup_key", "payload_json"},
		"memory_candidate": {"source_evidence_ids_json", "review_checkpoint_json", "admission_decision", "dedup_key"},
		"memory_relation":  {"source_id", "target_id", "relation_type", "weight"},
		"retrieval_trace":  {"retrieval_intent", "retrieval_mode", "used_fts", "used_vector", "used_relation", "used_code_index", "used_doc_index", "candidate_count", "injected_count", "latency_ms", "status"},
		"memory_access_log": {
			"memory_id", "retrieval_trace_id", "event_type", "event_weight",
			"score_breakdown_json", "inclusion_reason_json", "used_in_context",
		},
		"code_ref":             {"memory_id", "repo_id", "file_path", "symbol", "content_hash", "resolve_status"},
		"memory_embedding":     {"memory_id", "embedding_model", "embedding_dim", "embedding"},
		"doc_snapshot":         {"workspace_id", "project_id", "repo_id", "doc_path", "content_hash", "section_count"},
		"doc_section_snapshot": {"snapshot_id", "section_id", "heading_path_json", "content_hash", "summary"},
		"mvp_acceptance_run":   {"name", "mode", "workspace_id", "baseline_type", "candidate_type", "status", "summary_json", "report_path"},
		"mvp_acceptance_task": {
			"run_id", "scenario_id", "round", "agent_type", "baseline", "retrieval_trace_id",
			"task_success", "expected_json", "observed_json", "failure_reason",
		},
		"mvp_metric_sample": {
			"run_id", "scenario_id", "task_result_id", "agent_type", "metric_name",
			"metric_value", "numerator", "denominator", "threshold_operator", "passed",
		},
		"mvp_agent_capability": {
			"run_id", "agent_type", "capture_level", "conversation_capture", "tool_call_capture",
			"tool_output_capture", "file_edit_capture", "session_lifecycle", "memory_observe",
			"capability_coverage", "completeness", "degradation_reasons_json",
		},
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
