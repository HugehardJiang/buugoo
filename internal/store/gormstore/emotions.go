package gormstore

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/HugehardJiang/buugoo/internal/store"
)

type emotionRepo struct{ db *gorm.DB }

func (r emotionRepo) Get(ctx context.Context, userID uint64) (*store.EmotionState, error) {
	var s store.EmotionState
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&s).Error; err != nil {
		return nil, mapErr(err)
	}
	return &s, nil
}

func (r emotionRepo) Save(ctx context.Context, s *store.EmotionState) error {
	return mapErr(r.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(s).Error)
}

func (r emotionRepo) AddEvent(ctx context.Context, e *store.EmotionEvent) error {
	return mapErr(r.db.WithContext(ctx).Create(e).Error)
}

func (r emotionRepo) Events(ctx context.Context, userID uint64, limit int) ([]store.EmotionEvent, error) {
	var list []store.EmotionEvent
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("id DESC").Limit(limit).Find(&list).Error
	return list, mapErr(err)
}

func (r emotionRepo) Reset(ctx context.Context, userID uint64) error {
	return mapErr(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&store.EmotionEvent{}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).Delete(&store.EmotionState{}).Error
	}))
}
