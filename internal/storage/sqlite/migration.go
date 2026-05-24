package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"time"
)

type migration struct {
	version  int
	name     string
	fileName string
	sql      string
	checksum string
}

var migrationFileName = regexp.MustCompile(`^(\d+)_([a-zA-Z0-9_]+)\.sql$`)

// runMigrations 按版本号升序执行所有未应用的 migration。
// 处理流程：确保 schema_migration 表存在 -> 加载嵌入的 SQL 文件 -> 按版本号逐个执行。
// 安全机制：
//   - 幂等性：已应用的 migration 跳过，通过 checksum 校验防篡改
//   - 原子性：每个 migration 在独立事务中执行，失败时回滚并标记 Dirty
//   - FTS5 降级：init_fts migration 在 FTS5 不可用时自动跳过
func (s *Store) runMigrations(ctx context.Context) (MigrationStatus, error) {
	// 确保 schema_migration 表存在，用于记录已应用的 migration 版本
	if _, err := s.db.ExecContext(ctx, migrationTableDDL); err != nil {
		return MigrationStatus{Dirty: true}, fmt.Errorf("MIGRATION_FAILED: ensure schema_migration: %w", err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		return MigrationStatus{Dirty: true}, err
	}
	currentVersion := 0
	for _, item := range migrations {
		appliedChecksum, applied, err := s.appliedMigration(ctx, item.version)
		if err != nil {
			return MigrationStatus{CurrentVersion: currentVersion, Dirty: true}, err
		}
		if applied {
			// checksum 校验：已应用的 migration 文件被修改时报错，防止 schema 不一致
			if appliedChecksum != item.checksum {
				return MigrationStatus{CurrentVersion: currentVersion, Dirty: true},
					fmt.Errorf("MIGRATION_FAILED: checksum mismatch for version %d", item.version)
			}
			currentVersion = item.version
			continue
		}
		// FTS5 降级：init_fts migration 在 FTS5 不可用时自动跳过，不影响其他 migration
		if item.name == "init_fts" && !s.canCreateFTS5(ctx) {
			s.logger.Warn("skip optional fts migration because fts5 is unavailable", "version", item.version, "name", item.name)
			continue
		}
		startedAt := time.Now()
		if err := s.applyMigration(ctx, item); err != nil {
			return MigrationStatus{CurrentVersion: currentVersion, Dirty: true}, err
		}
		currentVersion = item.version
		s.logger.Info("migration applied",
			"version", item.version,
			"name", item.name,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	}
	return MigrationStatus{CurrentVersion: currentVersion, Dirty: false}, nil
}

// loadMigrations 从嵌入的 migrations/ 目录加载所有 SQL 文件。
// 文件命名规则：{version}_{name}.sql，例如 001_init.sql。
// 返回按版本号升序排列的 migration 列表，版本号重复时返回错误。
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("MIGRATION_FAILED: read migrations: %w", err)
	}
	items := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// 文件名格式校验：{version}_{name}.sql
		matches := migrationFileName.FindStringSubmatch(entry.Name())
		if len(matches) != 3 {
			return nil, fmt.Errorf("MIGRATION_FAILED: invalid migration file name %q", entry.Name())
		}
		version, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, fmt.Errorf("MIGRATION_FAILED: invalid migration version %q: %w", matches[1], err)
		}
		data, err := migrationFiles.ReadFile(path.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("MIGRATION_FAILED: read migration %q: %w", entry.Name(), err)
		}
		// 计算 SHA256 checksum，用于后续防篡改校验
		sum := sha256.Sum256(data)
		items = append(items, migration{
			version:  version,
			name:     matches[2],
			fileName: entry.Name(),
			sql:      string(data),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	// 按版本号升序排列
	sort.Slice(items, func(i, j int) bool {
		return items[i].version < items[j].version
	})
	// 版本号重复检测
	for i := 1; i < len(items); i++ {
		if items[i-1].version == items[i].version {
			return nil, fmt.Errorf("MIGRATION_FAILED: duplicate migration version %d", items[i].version)
		}
	}
	return items, nil
}

func (s *Store) appliedMigration(ctx context.Context, version int) (string, bool, error) {
	var checksum string
	err := s.db.QueryRowContext(ctx, "select checksum from schema_migration where version = ?", version).Scan(&checksum)
	if err == nil {
		return checksum, true, nil
	}
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return "", false, fmt.Errorf("MIGRATION_FAILED: query migration version %d: %w", version, err)
}

// applyMigration 在单个事务中执行 migration SQL 并记录到 schema_migration 表。
// 事务保证：migration SQL 和版本记录要么同时成功，要么同时回滚。
func (s *Store) applyMigration(ctx context.Context, item migration) error {
	// 事务保证：migration SQL 和版本记录要么同时成功，要么同时回滚
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("MIGRATION_FAILED: begin migration %s: %w", item.fileName, err)
	}
	// 执行 migration SQL（DDL + DML）
	if _, err := tx.ExecContext(ctx, item.sql); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("MIGRATION_FAILED: execute migration %s: %w", item.fileName, err)
	}
	// 记录到 schema_migration 表：版本号、名称、应用时间、checksum
	if _, err := tx.ExecContext(ctx,
		"insert into schema_migration(version, name, applied_at, checksum) values (?, ?, ?, ?)",
		item.version,
		item.name,
		time.Now().UTC().Format(time.RFC3339Nano),
		item.checksum,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("MIGRATION_FAILED: record migration %s: %w", item.fileName, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("MIGRATION_FAILED: commit migration %s: %w", item.fileName, err)
	}
	return nil
}

func (s *Store) canCreateFTS5(ctx context.Context) bool {
	tableName := fmt.Sprintf("temp.memoryd_migration_fts5_check_%d", time.Now().UnixNano())
	if _, err := s.db.ExecContext(ctx, "create virtual table "+tableName+" using fts5(content);"); err != nil {
		return false
	}
	_, _ = s.db.ExecContext(ctx, "drop table "+tableName+";")
	return true
}
