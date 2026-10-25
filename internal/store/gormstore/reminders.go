package gormstore

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/store"
)

type reminderRepo struct{ db *gorm.DB }

func (r reminderRepo) Create(ctx context.Context, rm *store.Reminder) error {
	if rm.Status == "" {
		rm.Status = store.ReminderPending
	}
	if rm.Kind == "" {
		rm.Kind = store.KindRemind
	}
	return mapErr(r.db.WithContext(ctx).Create(rm).Error)
}

var activeReminder = []string{store.ReminderPending, store.ReminderPaused}

func (r reminderRepo) ListPending(ctx context.Context, userID uint64) ([]store.Reminder, error) {
	var list []store.Reminder
	err := r.db.WithContext(ctx).Where("user_id = ? AND status IN ?", userID, activeReminder).Order("due_at").Find(&list).Error
	return list, mapErr(err)
}

func (r reminderRepo) Get(ctx context.Context, userID, id uint64) (*store.Reminder, error) {
	var rm store.Reminder
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ? AND status IN ?", id, userID, activeReminder).Take(&rm).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &rm, nil
}

func (r reminderRepo) Update(ctx context.Context, userID, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&store.Reminder{}).Where("id = ? AND user_id = ? AND status IN ?", id, userID, activeReminder).Updates(fields)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r reminderRepo) Cancel(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Model(&store.Reminder{}).Where("id = ? AND user_id = ? AND status IN ?", id, userID, activeReminder).
		Update("status", store.ReminderCanceled)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r reminderRepo) Due(ctx context.Context, now time.Time, limit int) ([]store.Reminder, error) {
	var list []store.Reminder
	err := r.db.WithContext(ctx).Where("status = ? AND due_at <= ?", store.ReminderPending, now).Order("due_at").Limit(limit).Find(&list).Error
	return list, mapErr(err)
}

func (r reminderRepo) Fired(ctx context.Context, id uint64, at time.Time, next *time.Time) error {
	fields := map[string]any{"fired_at": at}
	if next == nil {
		fields["status"] = store.ReminderDone
	} else {
		fields["due_at"] = *next
	}
	// 执行期间被取消的不再改回来。
	return mapErr(r.db.WithContext(ctx).Model(&store.Reminder{}).Where("id = ? AND status IN ?", id, activeReminder).Updates(fields).Error)
}

type bindingRepo struct{ db *gorm.DB }

func (r bindingRepo) Create(ctx context.Context, b *store.ChannelBinding) error {
	return mapErr(r.db.WithContext(ctx).Create(b).Error)
}

func (r bindingRepo) GetByPeer(ctx context.Context, channel, peerID string) (*store.ChannelBinding, error) {
	var b store.ChannelBinding
	if err := r.db.WithContext(ctx).Where("channel = ? AND peer_id = ?", channel, peerID).First(&b).Error; err != nil {
		return nil, mapErr(err)
	}
	return &b, nil
}

func (r bindingRepo) ListByUser(ctx context.Context, userID uint64) ([]store.ChannelBinding, error) {
	var list []store.ChannelBinding
	// 没聊过的排在最后（NULL 在不同数据库中的排序不同，单独处理）。
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("CASE WHEN last_message_at IS NULL THEN 1 ELSE 0 END, last_message_at DESC, id DESC").Find(&list).Error
	return list, mapErr(err)
}

func (r bindingRepo) Delete(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&store.ChannelBinding{})
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r bindingRepo) Update(ctx context.Context, id uint64, fields map[string]any) error {
	return mapErr(r.db.WithContext(ctx).Model(&store.ChannelBinding{}).Where("id = ?", id).Updates(fields).Error)
}
