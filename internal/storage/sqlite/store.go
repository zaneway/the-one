package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/zaneway/the-one/internal/config"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const migrationTableDDL = `
create table if not exists schema_migration (
  version integer primary key,
  name text not null,
  applied_at datetime not null,
  checksum text
);`

// Store 封装 P0 SQLite 连接、能力探测和 migration 状态。
type Store struct {
	db           *sql.DB
	path         string
	capabilities Capabilities
	migrations   MigrationStatus
	logger       *slog.Logger
}

// Capabilities 表示当前 SQLite 后端可用能力，用于 status 和后续检索降级判断。
type Capabilities struct {
	SQLite            bool     `json:"sqlite"`
	FTS5              bool     `json:"fts5"`
	SQLiteVec         bool     `json:"sqlite_vec"`
	FallbackRetrieval []string `json:"fallback_retrieval"`
}

// MigrationStatus 表示本地 schema migration 状态。Dirty 为 true 时不应继续启动业务能力。
type MigrationStatus struct {
	CurrentVersion int  `json:"current_version"`
	Dirty          bool `json:"dirty"`
}

// Status 是 SQLite 存储层对 diagnostics 暴露的非敏感状态快照。
type Status struct {
	Backend      string          `json:"backend"`
	DBPath       string          `json:"db_path"`
	Capabilities Capabilities    `json:"capabilities"`
	Migrations   MigrationStatus `json:"migrations"`
}

// Open 打开 SQLite、执行 PRAGMA、运行 migration 并探测 FTS5/sqlite-vec 能力。
func Open(ctx context.Context, cfg config.StorageConfig, logger *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
		return nil, fmt.Errorf("STORAGE_OPEN_FAILED: create db dir: %w", err)
	}
	db, err := sql.Open("sqlite3", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("STORAGE_OPEN_FAILED: open sqlite: %w", err)
	}
	store := &Store{db: db, path: cfg.Path, logger: logger}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	if err := store.applyPragmas(ctx, cfg.BusyTimeoutMS); err != nil {
		db.Close()
		return nil, err
	}
	status, err := store.runMigrations(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}
	store.migrations = status
	store.capabilities = store.detectCapabilities(ctx)
	return store, nil
}

// Ping 验证 SQLite 连接是否仍可用。
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("STORAGE_UNAVAILABLE: sqlite ping failed: %w", err)
	}
	return nil
}

// Status 返回 P0 诊断状态。该方法不暴露敏感配置。
func (s *Store) Status() Status {
	return Status{
		Backend:      "sqlite",
		DBPath:       s.path,
		Capabilities: s.capabilities,
		Migrations:   s.migrations,
	}
}

// Close 关闭 SQLite 连接池。
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) applyPragmas(ctx context.Context, busyTimeoutMS int) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
		fmt.Sprintf("PRAGMA busy_timeout = %d;", busyTimeoutMS),
	}
	for _, pragma := range pragmas {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("STORAGE_OPEN_FAILED: apply pragma: %w", err)
		}
	}
	return nil
}

func (s *Store) detectCapabilities(ctx context.Context) Capabilities {
	capabilities := Capabilities{
		SQLite:            true,
		FTS5:              s.detectFTS5(ctx),
		SQLiteVec:         false,
		FallbackRetrieval: []string{"metadata"},
	}
	if capabilities.FTS5 {
		capabilities.FallbackRetrieval = []string{"fts", "metadata"}
	}
	s.logger.Info("sqlite capability detected",
		"sqlite", capabilities.SQLite,
		"fts5", capabilities.FTS5,
		"sqlite_vec", capabilities.SQLiteVec,
	)
	return capabilities
}

func (s *Store) detectFTS5(ctx context.Context) bool {
	tableName := fmt.Sprintf("temp.memoryd_fts5_check_%d", time.Now().UnixNano())
	if _, err := s.db.ExecContext(ctx, "create virtual table "+tableName+" using fts5(content);"); err != nil {
		s.logger.Warn("fts5 unavailable", "error", err)
		return false
	}
	_, _ = s.db.ExecContext(ctx, "drop table "+tableName+";")
	return true
}
