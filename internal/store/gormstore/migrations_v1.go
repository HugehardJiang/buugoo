package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// 以下为 v1 初始表结构的快照，不得修改（修改表结构请追加新迁移）。

type v1User struct {
	ID           uint64  `gorm:"primaryKey"`
	Username     string  `gorm:"size:64;uniqueIndex;not null"`
	PasswordHash string  `gorm:"size:255;not null;default:''"`
	DisplayName  string  `gorm:"size:64;not null;default:''"`
	IsAdmin      bool    `gorm:"not null;default:false"`
	TokenVersion int     `gorm:"not null;default:0"`
	InviteID     *uint64 `gorm:"index"`
	CreatedAt    ts
	UpdatedAt    ts
}

func (v1User) TableName() string { return "users" }

type v1Invite struct {
	ID        uint64 `gorm:"primaryKey"`
	Code      string `gorm:"size:32;uniqueIndex;not null"`
	CreatedBy uint64 `gorm:"index;not null"`
	MaxUses   int    `gorm:"not null;default:1"`
	UsedCount int    `gorm:"not null;default:0"`
	ExpiresAt tsPtr
	Note      string `gorm:"size:255;not null;default:''"`
	CreatedAt ts
}

func (v1Invite) TableName() string { return "invites" }

type v1Conversation struct {
	ID            uint64 `gorm:"primaryKey"`
	UserID        uint64 `gorm:"index:idx_conv_user_last,priority:1;not null"`
	Title         string `gorm:"size:255;not null;default:''"`
	CreatedAt     ts
	UpdatedAt     ts
	LastMessageAt tsPtr `gorm:"index:idx_conv_user_last,priority:2"`
}

func (v1Conversation) TableName() string { return "conversations" }

type v1Message struct {
	ID               uint64 `gorm:"primaryKey"`
	ConversationID   uint64 `gorm:"index:idx_msg_conv,priority:1;not null"`
	UserID           uint64 `gorm:"index;not null"`
	Role             string `gorm:"size:16;not null"`
	Content          string `gorm:"size:65536;not null"`
	Model            string `gorm:"size:128;not null;default:''"`
	PromptTokens     int    `gorm:"not null;default:0"`
	CompletionTokens int    `gorm:"not null;default:0"`
	Status           string `gorm:"size:16;not null;default:'complete'"`
	CreatedAt        ts
}

func (v1Message) TableName() string { return "messages" }

type v1Memory struct {
	ID              uint64  `gorm:"primaryKey"`
	UserID          uint64  `gorm:"index:idx_mem_user_del,priority:1;index:idx_mem_user_type,priority:1;index:idx_mem_user_model,priority:1;not null"`
	Type            string  `gorm:"size:16;index:idx_mem_user_type,priority:2;not null"`
	Content         string  `gorm:"size:65536;not null"`
	Embedding       []byte  `gorm:"size:65536"`
	EmbeddingModel  string  `gorm:"size:128;index:idx_mem_user_model,priority:2;not null;default:''"`
	EmbeddingDim    int     `gorm:"not null;default:0"`
	Importance      int     `gorm:"not null;default:5"`
	Emotion         float64 `gorm:"not null;default:0"`
	Strength        float64 `gorm:"not null;default:1"`
	Source          string  `gorm:"size:16;not null"`
	SourceMessageID *uint64 `gorm:"index"`
	ConversationID  *uint64
	SupersededBy    *uint64 `gorm:"index"`
	ConsolidatedAt  tsPtr
	Metadata        string `gorm:"size:65536;not null"`
	CreatedAt       ts
	UpdatedAt       ts
	LastAccessedAt  tsPtr
	AccessCount     int            `gorm:"not null;default:0"`
	DeletedAt       gorm.DeletedAt `gorm:"index:idx_mem_user_del,priority:2"`
}

func (v1Memory) TableName() string { return "memories" }

type v1MemoryUsage struct {
	ID        uint64  `gorm:"primaryKey"`
	MessageID uint64  `gorm:"index;not null"`
	MemoryID  uint64  `gorm:"index;not null"`
	UserID    uint64  `gorm:"index;not null"`
	Score     float64 `gorm:"not null;default:0"`
	CreatedAt ts
}

func (v1MemoryUsage) TableName() string { return "memory_usages" }

type v1UsageDaily struct {
	ID               uint64 `gorm:"primaryKey"`
	UserID           uint64 `gorm:"uniqueIndex:idx_usage_user_day,priority:1;not null"`
	Day              string `gorm:"size:10;uniqueIndex:idx_usage_user_day,priority:2;not null"`
	Messages         int    `gorm:"not null;default:0"`
	PromptTokens     int    `gorm:"not null;default:0"`
	CompletionTokens int    `gorm:"not null;default:0"`
	UpdatedAt        ts
}

func (v1UsageDaily) TableName() string { return "usage_daily" }

type v1JobRun struct {
	ID         uint64 `gorm:"primaryKey"`
	JobName    string `gorm:"size:64;index:idx_job_name_start,priority:1;not null"`
	StartedAt  ts     `gorm:"index:idx_job_name_start,priority:2;not null"`
	FinishedAt tsPtr
	Status     string `gorm:"size:16;not null"`
	Error      string `gorm:"size:4096;not null;default:''"`
}

func (v1JobRun) TableName() string { return "job_runs" }

type v1AppMeta struct {
	Key       string `gorm:"column:meta_key;primaryKey;size:64"`
	Value     string `gorm:"size:4096;not null"`
	UpdatedAt ts
}

func (v1AppMeta) TableName() string { return "app_meta" }

func migrationV1Init() *gormigrate.Migration {
	tables := []any{
		&v1User{}, &v1Invite{}, &v1Conversation{}, &v1Message{}, &v1Memory{},
		&v1MemoryUsage{}, &v1UsageDaily{}, &v1JobRun{}, &v1AppMeta{},
	}
	return &gormigrate.Migration{
		ID: "202610250001_init",
		Migrate: func(tx *gorm.DB) error {
			return autoMigrate(tx, tables...)
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(tables...)
		},
	}
}
