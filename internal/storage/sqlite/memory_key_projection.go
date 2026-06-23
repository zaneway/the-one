package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"
	"unicode"

	"github.com/zaneway/theone/internal/memory"
)

type memoryKeyProjection struct {
	KeyType string
	KeyText string
	Weight  float64
}

func upsertMemoryKeys(ctx context.Context, tx *sql.Tx, item memory.MemoryItem) error {
	if err := deleteMemoryKeys(ctx, tx, item.ID); err != nil {
		return err
	}
	if !shouldIndex(item.State) {
		return nil
	}
	keys := buildMemoryKeyProjections(item)
	if len(keys) == 0 {
		return nil
	}
	now := time.Now().Format(time.RFC3339Nano)
	for _, key := range keys {
		keyID := item.ID + ":" + key.KeyType + ":" + shortHash(key.KeyText)
		_, err := tx.ExecContext(ctx, `insert into memory_key(
			key_id, memory_id, key_type, key_text, key_hash, weight,
			scope, memory_type, state, tier, created_at, updated_at
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			keyID, item.ID, key.KeyType, key.KeyText, hashText(key.KeyText), key.Weight,
			item.Scope, item.MemoryType, item.State, item.Tier, now, now,
		)
		if err != nil {
			return storageErr(err)
		}
	}
	return nil
}

func deleteMemoryKeys(ctx context.Context, tx *sql.Tx, memoryID string) error {
	_, err := tx.ExecContext(ctx, "delete from memory_key where memory_id = ?", memoryID)
	return storageErr(err)
}

func (s *Store) backfillMemoryKeys(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, baseMemorySelect()+`
		where state in (?, ?, ?)
		  and id not in (select memory_id from memory_key)`,
		memory.StateStable, memory.StatePendingReview, memory.StateProvisional,
	)
	if err != nil {
		return storageErr(err)
	}
	defer rows.Close()
	items, err := scanMemoryRows(rows)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storageErr(err)
	}
	for _, item := range items {
		if err := upsertMemoryKeys(ctx, tx, item); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return storageErr(tx.Commit())
}

func buildMemoryKeyProjections(item memory.MemoryItem) []memoryKeyProjection {
	values := []struct {
		keyType string
		text    string
		weight  float64
	}{
		{keyType: "search_text", text: item.SearchText, weight: 1.0},
		{keyType: "title", text: item.Title, weight: 0.85},
		{keyType: "content", text: item.Content, weight: 0.75},
	}
	out := make([]memoryKeyProjection, 0, len(values)*2)
	seen := map[string]bool{}
	add := func(keyType, text string, weight float64) {
		text = normalizeKeyText(text)
		if text == "" {
			return
		}
		key := keyType + "\x00" + text
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, memoryKeyProjection{KeyType: keyType, KeyText: text, Weight: weight})
		compact := compactKeyText(text)
		if compact == "" || compact == text {
			return
		}
		compactKey := keyType + "_compact\x00" + compact
		if seen[compactKey] {
			return
		}
		seen[compactKey] = true
		out = append(out, memoryKeyProjection{KeyType: keyType + "_compact", KeyText: compact, Weight: weight * 0.9})
	}
	for _, value := range values {
		add(value.keyType, value.text, value.weight)
	}
	return out
}

func normalizeKeyText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func compactKeyText(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r > unicode.MaxASCII {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func shortHash(value string) string {
	hash := hashText(value)
	if len(hash) <= 16 {
		return hash
	}
	return hash[:16]
}
