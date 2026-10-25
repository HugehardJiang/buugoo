package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v6：长对话摘要（较早的消息整理成摘要放进提示词）、提醒与主动消息、聊天软件（微信、QQ、Telegram）绑定。
// 以下为 v6 的快照结构体，不得修改。

type v6Conversation struct {
	Summary      string `gorm:"size:4000"`
	SummaryUntil uint64 `gorm:"not null;default:0"`
}

func (v6Conversation) TableName() string { return "conversations" }

type v6User struct {
	Proactive       bool `gorm:"not null;default:true"`
	LastProactiveAt tsPtr
}

func (v6User) TableName() string { return "users" }

type v6Reminder struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"index;not null"`
	Content   string `gorm:"size:512;not null"`
	DueAt     ts     `gorm:"index:idx_reminder_due,priority:2;not null"`
	Repeat    string `gorm:"size:16;not null;default:''"`
	Status    string `gorm:"size:16;index:idx_reminder_due,priority:1;not null;default:'pending'"`
	FiredAt   tsPtr
	CreatedAt ts
}

func (v6Reminder) TableName() string { return "reminders" }

type v6ChannelBinding struct {
	ID             uint64 `gorm:"primaryKey"`
	UserID         uint64 `gorm:"index;not null"`
	Channel        string `gorm:"size:16;not null;uniqueIndex:idx_binding_peer,priority:1"`
	PeerID         string `gorm:"size:128;not null;uniqueIndex:idx_binding_peer,priority:2"`
	PeerName       string `gorm:"size:128;not null;default:''"`
	ConversationID *uint64
	CreatedAt      ts
	LastMessageAt  tsPtr
}

func (v6ChannelBinding) TableName() string { return "channel_bindings" }

func migrationV6() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610250006_summary_reminders_channels",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range []string{"Summary", "SummaryUntil"} {
				if !m.HasColumn(&v6Conversation{}, col) {
					if err := m.AddColumn(&v6Conversation{}, col); err != nil {
						return err
					}
				}
			}
			for _, col := range []string{"Proactive", "LastProactiveAt"} {
				if !m.HasColumn(&v6User{}, col) {
					if err := m.AddColumn(&v6User{}, col); err != nil {
						return err
					}
				}
			}
			return autoMigrate(tx, &v6Reminder{}, &v6ChannelBinding{})
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if err := m.DropTable(&v6ChannelBinding{}, &v6Reminder{}); err != nil {
				return err
			}
			for _, col := range []string{"Proactive", "LastProactiveAt"} {
				if m.HasColumn(&v6User{}, col) {
					if err := m.DropColumn(&v6User{}, col); err != nil {
						return err
					}
				}
			}
			for _, col := range []string{"Summary", "SummaryUntil"} {
				if m.HasColumn(&v6Conversation{}, col) {
					if err := m.DropColumn(&v6Conversation{}, col); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}
