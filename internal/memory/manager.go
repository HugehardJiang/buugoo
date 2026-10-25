package memory

import (
	"context"
	"fmt"

	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/vector"
)

// Manager 提供面向用户的记忆管理（查看、删除）。
type Manager struct {
	repo store.MemoryRepo
	vs   vector.Store
}

// NewManager 创建记忆管理器。
func NewManager(repo store.MemoryRepo, vs vector.Store) *Manager {
	return &Manager{repo: repo, vs: vs}
}

// List 分页列出用户的记忆。
func (m *Manager) List(ctx context.Context, userID uint64, f store.MemoryFilter, limit, offset int) ([]store.Memory, int64, error) {
	return m.repo.List(ctx, userID, f, limit, offset)
}

// Get 获取一条记忆。
func (m *Manager) Get(ctx context.Context, userID, id uint64) (*store.Memory, error) {
	return m.repo.Get(ctx, userID, id)
}

// Delete 删除一条记忆：立即清空内容与向量，不可恢复。
func (m *Manager) Delete(ctx context.Context, userID, id uint64) error {
	if err := m.repo.Delete(ctx, userID, id); err != nil {
		return err
	}
	if err := m.vs.Delete(ctx, userID, id); err != nil {
		// 数据库中的向量已清空，缓存失效后不会再被检索到。
		m.vs.Invalidate(userID)
		return fmt.Errorf("从向量缓存删除记忆: %w", err)
	}
	return nil
}
