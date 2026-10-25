// Package vector 提供向量编解码、相似度计算与 VectorStore 抽象。
//
// 向量以 float32 小端序存入数据库 BLOB 字段，写入前统一归一化，
// 因此余弦相似度等于点积。
package vector

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// 向量相关错误。
var (
	ErrZeroVector   = errors.New("零向量无法归一化")
	ErrDimMismatch  = errors.New("向量维度不一致")
	ErrInvalidBytes = errors.New("向量字节长度不是 4 的倍数")
	ErrNotFinite    = errors.New("向量包含 NaN 或 Inf")
)

// Normalize 返回 v 的单位向量副本。
func Normalize(v []float32) ([]float32, error) {
	var sum float64
	for _, x := range v {
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, ErrNotFinite
		}
		sum += f * f
	}
	if sum == 0 {
		return nil, ErrZeroVector
	}
	norm := math.Sqrt(sum)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out, nil
}

// Encode 把向量编码为 float32 小端序字节。
func Encode(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// Decode 把 float32 小端序字节解码为向量。
func Decode(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, ErrInvalidBytes
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v, nil
}

// Dot 计算点积（float64 累加以减小误差）。
func Dot(a, b []float32) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("%w: %d != %d", ErrDimMismatch, len(a), len(b))
	}
	return dot(a, b), nil
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// Cosine 计算任意两个向量的余弦相似度，任一为零向量时返回 0。
func Cosine(a, b []float32) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("%w: %d != %d", ErrDimMismatch, len(a), len(b))
	}
	var ab, aa, bb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		ab += x * y
		aa += x * x
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return 0, nil
	}
	return ab / (math.Sqrt(aa) * math.Sqrt(bb)), nil
}

// Hit 是一条检索结果。
type Hit struct {
	ID         uint64
	Similarity float64
}

// Store 是向量检索抽象。当前实现是"内存暴力计算 + 活跃用户缓存"（memstore），
// 以后可替换为专门的向量数据库。
//
// 向量按 (userID, model) 隔离：检索时只比较同一模型生成的向量。
// 对于 memstore，向量的持久化由 MemoryRepo 负责，Upsert/Delete 只维护缓存；
// 换成外部向量库时，Upsert/Delete 负责写入外部存储。
type Store interface {
	// Search 返回与 query 最相似的至多 k 条结果，按相似度降序。query 无需预先归一化。
	Search(ctx context.Context, userID uint64, model string, query []float32, k int) ([]Hit, error)
	// Upsert 写入或更新一条已归一化的向量。
	Upsert(ctx context.Context, userID uint64, model string, id uint64, vec []float32) error
	// Delete 删除一条向量（所有模型下）。
	Delete(ctx context.Context, userID, id uint64) error
	// Invalidate 丢弃该用户的缓存。
	Invalidate(userID uint64)
	// InvalidateAll 丢弃全部缓存（例如批量重新计算向量之后）。
	InvalidateAll()
}
