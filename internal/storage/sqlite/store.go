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
	"github.com/zaneway/theone/internal/config"
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

// Store 封装 SQLite 连接、能力探测和 migration 状态。
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
// 初始化顺序：创建目录 -> 打开连接 -> 设置连接池参数 -> 应用 PRAGMA -> 运行 migration -> 探测能力。
// 任何步骤失败都会关闭连接并返回错误，避免半初始化状态。
func Open(ctx context.Context, cfg config.StorageConfig, logger *slog.Logger) (*Store, error) {
	// 确保数据库文件所在目录存在
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
		return nil, fmt.Errorf("STORAGE_OPEN_FAILED: create db dir: %w", err)
	}
	db, err := sql.Open("sqlite3", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("STORAGE_OPEN_FAILED: open sqlite: %w", err)
	}
	store := &Store{db: db, path: cfg.Path, logger: logger}
	// SQLite 单文件数据库，连接池设为 4（WAL 模式下支持 1 写 + 多读并发）
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0) // SQLite 连接不设过期时间

	// 应用 PRAGMA（WAL + synchronous=NORMAL + foreign_keys + busy_timeout）
	if err := store.applyPragmas(ctx, cfg.BusyTimeoutMS); err != nil {
		db.Close()
		return nil, err
	}
	// 运行 schema migration（幂等 + checksum 校验）
	status, err := store.runMigrations(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}
	store.migrations = status
	// 探测 FTS5/sqlite-vec 能力，决定检索降级路径
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

// Status 返回诊断状态。该方法不暴露敏感配置。
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

// applyPragmas 应用 SQLite 运行时 PRAGMA 设置。
// WAL 模式：支持并发读写，写入不阻塞读取。
// synchronous=NORMAL：WAL 模式下推荐设置，兼顾性能和数据安全。
// foreign_keys=ON：启用外键约束，保证数据引用完整性。
// busy_timeout：写锁等待上限，避免请求无限阻塞。
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

// detectCapabilities 探测 SQLite 后端可用能力。
// FTS5 是检索的必要能力，不可用时降级为 metadata + LIKE 查询。
// sqlite-vec 是可选的向量索引增强，当前默认不启用。
// FallbackRetrieval 列表决定了检索降级路径。
func (s *Store) detectCapabilities(ctx context.Context) Capabilities {
	capabilities := Capabilities{
		SQLite:            true,
		FTS5:              s.detectFTS5(ctx),
		SQLiteVec:         false,                // sqlite-vec 当前默认不启用
		FallbackRetrieval: []string{"metadata"}, // FTS5 不可用时的降级路径
	}
	// FTS5 可用时，检索降级路径为 fts -> metadata（先 FTS5 全文检索，无结果再 LIKE）
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

// detectFTS5 通过创建临时虚表检测 FTS5 模块是否可用。
// 使用时间戳作为表名避免并发检测冲突。
func (s *Store) detectFTS5(ctx context.Context) bool {
	tableName := fmt.Sprintf("temp.the_one_fts5_check_%d", time.Now().UnixNano())
	if _, err := s.db.ExecContext(ctx, "create virtual table "+tableName+" using fts5(content);"); err != nil {
		s.logger.Warn("fts5 unavailable", "error", err)
		return false
	}
	_, _ = s.db.ExecContext(ctx, "drop table "+tableName+";")
	return true
}
