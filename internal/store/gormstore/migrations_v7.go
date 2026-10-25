package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v7：对话标记来自哪个聊天软件；提醒加上定时任务（到点执行一件事）与更多重复方式（每月、每隔一段时间、cron）。
// 以下为 v7 的快照结构体，不得修改。

type v7Conversation struct {
	Channel string `gorm:"size:16;not null;default:''"`
}

func (v7Conversation) TableName() string { return "conversations" }

type v7Reminder struct {
	Kind  string `gorm:"size:16;not null;default:'remind'"`
	Every int    `gorm:"not null;default:0"`
	Cron  string `gorm:"size:64;not null;default:''"`
}

func (v7Reminder) TableName() string { return "reminders" }

func migrationV7() *gormigrate.Migration {
	cols := []struct {
		model any
		names []string
	}{
		{&v7Conversation{}, []string{"Channel"}},
		{&v7Reminder{}, []string{"Kind", "Every", "Cron"}},
	}
	return &gormigrate.Migration{
		ID: "202610250007_channel_tasks",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, c := range cols {
				for _, col := range c.names {
					if !m.HasColumn(c.model, col) {
						if err := m.AddColumn(c.model, col); err != nil {
							return err
						}
					}
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, c := range cols {
				for _, col := range c.names {
					if m.HasColumn(c.model, col) {
						if err := m.DropColumn(c.model, col); err != nil {
							return err
						}
					}
				}
			}
			return nil
		},
	}
}
