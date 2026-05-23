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

func (s *Store) runMigrations(ctx context.Context) (MigrationStatus, error) {
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
			if appliedChecksum != item.checksum {
				return MigrationStatus{CurrentVersion: currentVersion, Dirty: true},
					fmt.Errorf("MIGRATION_FAILED: checksum mismatch for version %d", item.version)
			}
			currentVersion = item.version
			continue
		}
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
		sum := sha256.Sum256(data)
		items = append(items, migration{
			version:  version,
			name:     matches[2],
			fileName: entry.Name(),
			sql:      string(data),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].version < items[j].version
	})
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

func (s *Store) applyMigration(ctx context.Context, item migration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("MIGRATION_FAILED: begin migration %s: %w", item.fileName, err)
	}
	if _, err := tx.ExecContext(ctx, item.sql); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("MIGRATION_FAILED: execute migration %s: %w", item.fileName, err)
	}
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
