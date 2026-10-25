package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v4：情绪系统（每个用户一份情绪状态与变化记录）。以下为 v4 的快照结构体，不得修改。

type v4EmotionState struct {
	UserID        uint64 `gorm:"primaryKey;autoIncrement:false"`
	Joy           float64
	Sadness       float64
	Anger         float64
	Worry         float64
	Surprise      float64
	Impatience    float64
	Longing       float64
	Mood          float64
	Affinity      float64
	Trust         float64
	Cause         string `gorm:"size:255"`
	Pace          string `gorm:"size:4096"`
	LastMessageAt tsPtr
	UpdatedAt     ts
}

func (v4EmotionState) TableName() string { return "emotion_states" }

type v4EmotionEvent struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"index:idx_emo_user,priority:1;not null"`
	Kind      string `gorm:"size:16;not null"`
	Cause     string `gorm:"size:255"`
	Changes   string `gorm:"size:1024"`
	CreatedAt ts     `gorm:"index:idx_emo_user,priority:2"`
}

func (v4EmotionEvent) TableName() string { return "emotion_events" }

func migrationV4Emotions() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610250004_emotions",
		Migrate: func(tx *gorm.DB) error {
			return autoMigrate(tx, &v4EmotionState{}, &v4EmotionEvent{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(&v4EmotionEvent{}, &v4EmotionState{})
		},
	}
}
