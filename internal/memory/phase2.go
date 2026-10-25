package memory

import (
	"context"
	"time"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// 以下接口为第二阶段预留，第一阶段不实现。设计说明见 docs/memory.md。

// Forgetter 计算记忆的保持率（遗忘曲线）：
//
//	R = exp(-t / (S · (1 + k·E)))
//
// 其中 t 为距上次访问的时间，S 为记忆强度（strength），E 为情绪强度（emotion），
// k 为情绪对遗忘的减缓系数。保持率过低的记忆在检索中降权或被归档。
type Forgetter interface {
	Retention(m *store.Memory, now time.Time) float64
}

// Consolidator 执行每日"睡眠"巩固：把相近的情景记忆提炼为语义记忆、
// 合并重复记忆、处理冲突（旧记忆写入 superseded_by 指向新记忆）。
type Consolidator interface {
	Consolidate(ctx context.Context, userID uint64, since time.Time) (ConsolidationReport, error)
}

// ConsolidationReport 是一次巩固的结果。
type ConsolidationReport struct {
	Created    int
	Merged     int
	Superseded int
}
