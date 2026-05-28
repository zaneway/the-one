package sqlite

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// UpsertMemoryEmbedding 写入或替换单条 memory embedding。
// 设计约束：embedding 是可替换派生数据，按 memory_id + model 幂等覆盖，模型升级可并存多版本。
func (s *Store) UpsertMemoryEmbedding(ctx context.Context, memoryID, model string, vector []float32) error {
	if memoryID == "" || model == "" || len(vector) == 0 {
		return fmt.Errorf("VALIDATION_FAILED: memory_id, embedding model and vector are required")
	}
	blob := float32VectorBlob(vector)
	now := time.Now().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `insert into memory_embedding(
		memory_id, embedding_model, embedding_dim, embedding, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?)
	on conflict(memory_id, embedding_model) do update set
		embedding_dim = excluded.embedding_dim,
		embedding = excluded.embedding,
		updated_at = excluded.updated_at`,
		memoryID, model, len(vector), blob, now, now,
	)
	return storageErr(err)
}

// DeleteMemoryEmbedding 删除单条或某 memory 的 embedding。
// model 为空时删除该 memory 的全部 embedding 版本。
func (s *Store) DeleteMemoryEmbedding(ctx context.Context, memoryID, model string) error {
	if memoryID == "" {
		return fmt.Errorf("VALIDATION_FAILED: memory_id is required")
	}
	var err error
	if model == "" {
		_, err = s.db.ExecContext(ctx, "delete from memory_embedding where memory_id = ?", memoryID)
	} else {
		_, err = s.db.ExecContext(ctx, "delete from memory_embedding where memory_id = ? and embedding_model = ?", memoryID, model)
	}
	return storageErr(err)
}

func float32VectorBlob(vector []float32) []byte {
	blob := make([]byte, 4*len(vector))
	for i, value := range vector {
		binary.LittleEndian.PutUint32(blob[i*4:(i+1)*4], math.Float32bits(value))
	}
	return blob
}
