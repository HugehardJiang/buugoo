package memory

import (
	"context"
	"log/slog"
)

// ConsolidationJobName 是每日记忆巩固任务的名称。
const ConsolidationJobName = "memory.consolidation"

// ConsolidationJob 是每日"睡眠"记忆巩固的定时任务。
//
// 第一阶段只注册空任务：Consolidator 为空时直接返回。第二阶段实现 Consolidator 后，
// 在这里遍历近期活跃用户并调用 Consolidate。
type ConsolidationJob struct {
	spec         string
	consolidator Consolidator
	log          *slog.Logger
}

// NewConsolidationJob 创建巩固任务，consolidator 可为 nil。
func NewConsolidationJob(spec string, c Consolidator, log *slog.Logger) *ConsolidationJob {
	if log == nil {
		log = slog.Default()
	}
	return &ConsolidationJob{spec: spec, consolidator: c, log: log}
}

// Name 实现 scheduler.Job。
func (j *ConsolidationJob) Name() string { return ConsolidationJobName }

// Spec 实现 scheduler.Job。
func (j *ConsolidationJob) Spec() string { return j.spec }

// Run 实现 scheduler.Job。
func (j *ConsolidationJob) Run(ctx context.Context) error {
	if j.consolidator == nil {
		j.log.Debug("记忆巩固尚未实现（第二阶段），跳过")
		return nil
	}
	return nil
}
