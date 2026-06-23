package sqlite

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
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
	wantVersion := 12
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
		"memory_key",
		"doc_snapshot",
		"doc_section_snapshot",
		"memory_provenance",
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
		"memory_candidate": {"source_evidence_ids_json", "review_checkpoint_json", "event_score", "admission_decision", "dedup_key"},
		"memory_relation":  {"source_id", "target_id", "relation_type", "weight"},
		"retrieval_trace":  {"retrieval_intent", "retrieval_mode", "used_fts", "used_vector", "used_relation", "used_code_index", "used_doc_index", "candidate_count", "injected_count", "latency_ms", "status"},
		"memory_access_log": {
			"memory_id", "retrieval_trace_id", "event_type", "event_weight",
			"score_breakdown_json", "inclusion_reason_json", "used_in_context",
		},
		"code_ref":             {"memory_id", "repo_id", "file_path", "symbol", "content_hash", "resolve_status"},
		"memory_embedding":     {"memory_id", "embedding_model", "embedding_dim", "embedding"},
		"memory_key":           {"memory_id", "key_type", "key_text", "key_hash", "weight", "scope", "memory_type", "state", "tier"},
		"doc_snapshot":         {"workspace_id", "project_id", "repo_id", "doc_path", "content_hash", "section_count"},
		"doc_section_snapshot": {"snapshot_id", "section_id", "heading_path_json", "content_hash", "summary"},
		"memory_provenance":    {"memory_id", "raw_event_id", "evidence_id", "candidate_id", "source_producer", "hook_phase", "provider", "derivation_stage", "admission_decision"},
		"raw_event":            {"raw_payload_json", "payload_schema", "raw_payload_hash", "redaction_state", "redaction_policy", "truncated", "original_size_bytes", "stored_size_bytes", "max_size_bytes", "truncation_reason"},
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

func TestOpenBackfillsMissingMemoryProvenance(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	logger := slog.New(slog.DiscardHandler)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	legacy := &Store{db: db, logger: logger}
	if _, err := db.ExecContext(ctx, migrationTableDDL); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	for _, item := range migrations {
		if item.version > 9 {
			continue
		}
		if item.name == "init_fts" && !legacy.canCreateFTS5(ctx) {
			continue
		}
		if err := legacy.applyMigration(ctx, item); err != nil {
			t.Fatalf("apply migration %d %s: %v", item.version, item.name, err)
		}
	}
	if _, err := db.ExecContext(ctx, `insert into raw_event(
		id, session_id, task_id, workspace_id, project_id, repo_id, agent_type, event_type, source_channel, occurred_at,
		actor, content_summary, source_refs_json, content_hash, created_at
	) values (
		'evt_backfill', 'sess_backfill', 'task_backfill', 'ws', 'project', 'repo', 'claude_code', 'agent.response.summary', 'agent_session', '2026-06-05T12:00:00Z',
		'agent', '【结论/决策】完成 provenance backfill', '[{"source_type":"agent_session","capture_method":"adapter_hook","protocol_version":"v1"}]', 'sha256:backfill', '2026-06-05T12:00:01Z'
	);
	insert into evidence(id, raw_event_id, source_type, interpreted_statement, source_ref_json, confidence, created_at)
	values ('ev_backfill', 'evt_backfill', 'agent_summary', '完成 provenance backfill', '{"raw_event_id":"evt_backfill","capture_method":"adapter_hook"}', 0.8, '2026-06-05T12:00:02Z');
	insert into memory_provenance(
		id, memory_id, raw_event_id, evidence_id, candidate_id, hook_phase, event_type, capture_method, provider, derivation_stage, admission_decision, admission_score, created_at
	) values (
		'prov_backfill', 'mem_backfill', 'evt_backfill', 'ev_backfill', 'cand_backfill', 'unknown', 'agent.response.summary', 'adapter_hook', 'rule_based', 'compute_admission', 'write_provisional', 0.7, '2026-06-05T12:00:03Z'
	);`); err != nil {
		t.Fatalf("insert legacy provenance fixtures: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	cfg := config.StorageConfig{
		Backend:          "sqlite",
		Path:             dbPath,
		SQLiteVecEnabled: "auto",
		BusyTimeoutMS:    1000,
	}
	store, err := Open(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	var producer, phase, agentType, sourceChannel string
	err = store.db.QueryRowContext(ctx, `select coalesce(source_producer, ''), hook_phase, agent_type, source_channel
		from memory_provenance where id = 'prov_backfill'`).Scan(&producer, &phase, &agentType, &sourceChannel)
	if err != nil {
		t.Fatalf("query backfilled provenance: %v", err)
	}
	if producer != "claude_code_hook:Stop" || phase != "turn_end" || agentType != "claude_code" || sourceChannel != "agent_session" {
		t.Fatalf("backfilled provenance = producer=%q phase=%q agent=%q channel=%q, want claude Stop turn_end", producer, phase, agentType, sourceChannel)
	}
}

func TestOpenBackfillsMemoryKeyProjectionForExistingMemories(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	logger := slog.New(slog.DiscardHandler)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	legacy := &Store{db: db, logger: logger}
	if _, err := db.ExecContext(ctx, migrationTableDDL); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	for _, item := range migrations {
		if item.version > 11 {
			continue
		}
		if item.name == "init_fts" && !legacy.canCreateFTS5(ctx) {
			continue
		}
		if err := legacy.applyMigration(ctx, item); err != nil {
			t.Fatalf("apply migration %d %s: %v", item.version, item.name, err)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `insert into memory_item(
		id, scope, workspace_id, project_id, memory_type, content, search_text,
		state, confidence, importance, decay_rate, tier, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mem_backfill_key", memory.ScopeProjectLocal, "ws", "project_backfill_key",
		memory.TypeDecision, "旧库记忆使用 QKV Retrieval Projection。", "retrieval: QKV Retrieval Projection",
		memory.StateStable, 0.8, 0.8, 0.8, memory.TierLongTerm, now, now,
	); err != nil {
		t.Fatalf("insert legacy memory_item: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	cfg := config.StorageConfig{
		Backend:          "sqlite",
		Path:             dbPath,
		SQLiteVecEnabled: "auto",
		BusyTimeoutMS:    1000,
	}
	store, err := Open(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("Open() migrated db error = %v", err)
	}
	defer store.Close()
	var keyCount int
	if err := store.db.QueryRowContext(ctx, "select count(*) from memory_key where memory_id = ?", "mem_backfill_key").Scan(&keyCount); err != nil {
		t.Fatalf("query memory_key count error = %v", err)
	}
	if keyCount == 0 {
		t.Fatal("memory_key count = 0, want backfilled key projection")
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
