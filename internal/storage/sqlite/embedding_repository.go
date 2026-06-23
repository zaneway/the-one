package sqlite

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/zaneway/theone/internal/memory"
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

// SearchVector 使用已持久化的 memory embedding 做 cosine 召回。
// 设计约束：当前实现以 SQLite 持久化 + Go 内存排序完成工程闭环；后续可在相同接口下替换为 ANN 后端。
func (s *Store) SearchVector(ctx context.Context, req memory.SearchRequest, model string, queryVector []float32, limit int) ([]memory.SearchResult, error) {
	if model == "" || len(queryVector) == 0 {
		return nil, fmt.Errorf("VALIDATION_FAILED: embedding model and query vector are required")
	}
	if limit <= 0 {
		limit = req.Limit
	}
	if limit <= 0 {
		limit = 10
	}
	query := `select m.id, m.memory_type, m.scope, coalesce(m.title, ''), m.content,
		m.confidence, m.state, m.tier, me.embedding
		from memory_embedding me
		join memory_item m on m.id = me.memory_id
		where me.embedding_model = ? and me.embedding_dim = ?`
	args := []any{model, len(queryVector)}
	query, args = appendSearchFilters(query, args, req, false)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()

	ranked := make([]memory.SearchResult, 0)
	for rows.Next() {
		var result memory.SearchResult
		var embedding []byte
		if err := rows.Scan(
			&result.MemoryID,
			&result.MemoryType,
			&result.Scope,
			&result.Title,
			&result.Content,
			&result.Confidence,
			&result.State,
			&result.Tier,
			&embedding,
		); err != nil {
			return nil, storageErr(err)
		}
		vector, ok := float32VectorFromBlob(embedding)
		if !ok || len(vector) != len(queryVector) {
			continue
		}
		result.Score = cosineSimilarity(queryVector, vector)
		result.WhyIncluded = []string{"vector_seed"}
		if req.IncludeEvidence {
			result.EvidenceRefs = s.loadEvidenceRefs(ctx, result.MemoryID)
		}
		ranked = append(ranked, result)
	}
	if err := rows.Err(); err != nil {
		return nil, storageErr(err)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].MemoryID < ranked[j].MemoryID
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

func float32VectorBlob(vector []float32) []byte {
	blob := make([]byte, 4*len(vector))
	for i, value := range vector {
		binary.LittleEndian.PutUint32(blob[i*4:(i+1)*4], math.Float32bits(value))
	}
	return blob
}

func float32VectorFromBlob(blob []byte) ([]float32, bool) {
	if len(blob) == 0 || len(blob)%4 != 0 {
		return nil, false
	}
	vector := make([]float32, len(blob)/4)
	for i := range vector {
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4 : (i+1)*4]))
	}
	return vector, true
}

func cosineSimilarity(left, right []float32) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for i := range left {
		l := float64(left[i])
		r := float64(right[i])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return clamp(dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm)))
}
