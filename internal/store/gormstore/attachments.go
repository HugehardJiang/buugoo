package gormstore

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/store"
)

type attachmentRepo struct{ db *gorm.DB }

func (r attachmentRepo) Create(ctx context.Context, a *store.Attachment) error {
	return mapErr(r.db.WithContext(ctx).Create(a).Error)
}

func (r attachmentRepo) Get(ctx context.Context, userID, id uint64) (*store.Attachment, error) {
	var a store.Attachment
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&a).Error; err != nil {
		return nil, mapErr(err)
	}
	return &a, nil
}

func (r attachmentRepo) ListPending(ctx context.Context, userID uint64, ids []uint64) ([]store.Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []store.Attachment
	if err := r.db.WithContext(ctx).Where("user_id = ? AND id IN ? AND message_id IS NULL", userID, ids).Find(&list).Error; err != nil {
		return nil, mapErr(err)
	}
	byID := make(map[uint64]store.Attachment, len(list))
	for _, a := range list {
		byID[a.ID] = a
	}
	out := make([]store.Attachment, 0, len(list))
	for _, id := range ids {
		if a, ok := byID[id]; ok {
			out = append(out, a)
			delete(byID, id)
		}
	}
	return out, nil
}

func (r attachmentRepo) Attach(ctx context.Context, userID uint64, ids []uint64, conversationID, messageID uint64) error {
	if len(ids) == 0 {
		return nil
	}
	// 全部符合条件才关联，避免部分附件被关联到消息上。
	return mapErr(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&store.Attachment{}).
			Where("user_id = ? AND id IN ? AND message_id IS NULL", userID, ids).
			Updates(map[string]any{"conversation_id": conversationID, "message_id": messageID})
		if res.Error != nil {
			return res.Error
		}
		if int(res.RowsAffected) != len(ids) {
			return store.ErrNotFound
		}
		return nil
	}))
}

func (r attachmentRepo) ListByMessages(ctx context.Context, userID uint64, messageIDs []uint64) ([]store.Attachment, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	var list []store.Attachment
	err := r.db.WithContext(ctx).Where("user_id = ? AND message_id IN ?", userID, messageIDs).Order("id").Find(&list).Error
	return list, mapErr(err)
}

func (r attachmentRepo) ListByConversation(ctx context.Context, userID, conversationID uint64) ([]store.Attachment, error) {
	var list []store.Attachment
	err := r.db.WithContext(ctx).Where("user_id = ? AND conversation_id = ?", userID, conversationID).Order("id").Find(&list).Error
	return list, mapErr(err)
}

func (r attachmentRepo) ListStale(ctx context.Context, before time.Time, limit int) ([]store.Attachment, error) {
	var list []store.Attachment
	err := r.db.WithContext(ctx).Where("message_id IS NULL AND created_at < ?", before).Order("id").Limit(limit).Find(&list).Error
	return list, mapErr(err)
}

func (r attachmentRepo) TotalSize(ctx context.Context, userID uint64) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&store.Attachment{}).Where("user_id = ?", userID).
		Select("COALESCE(SUM(size), 0)").Scan(&total).Error
	return total, mapErr(err)
}

func (r attachmentRepo) SetDescription(ctx context.Context, id uint64, desc string) error {
	return mapErr(r.db.WithContext(ctx).Model(&store.Attachment{}).Where("id = ?", id).Update("description", desc).Error)
}

func (r attachmentRepo) Delete(ctx context.Context, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	return mapErr(r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&store.Attachment{}).Error)
}
