package gormstore

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v12：会员套餐、卡密、用户停用。以下为 v12 的快照结构体，不得修改。

type v12User struct {
	DisabledAt     *time.Time
	DisabledReason string `gorm:"size:200;not null;default:''"`
}

func (v12User) TableName() string { return "users" }

type v12Message struct {
	Error string `gorm:"type:text"`
}

func (v12Message) TableName() string { return "messages" }

type v12Plan struct {
	ID          uint64 `gorm:"primaryKey"`
	Name        string `gorm:"size:64;not null"`
	Description string `gorm:"type:text"`
	Price       string `gorm:"size:64;not null;default:''"`
	Kind        string `gorm:"size:16;not null"`
	Unit        string `gorm:"size:16;not null"`
	Days        int    `gorm:"not null;default:0"`
	Cycle       string `gorm:"size:16;not null;default:''"`
	Quota       int64  `gorm:"not null;default:0"`
	ValidDays   int    `gorm:"not null;default:0"`
	BuyURL      string `gorm:"size:512;not null;default:''"`
	Visible     bool   `gorm:"not null;default:true"`
	Sort        int    `gorm:"not null;default:0"`
	CreatedAt   ts
	UpdatedAt   ts
}

func (v12Plan) TableName() string { return "plans" }

type v12Subscription struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"index;not null"`
	PlanID    uint64 `gorm:"not null"`
	PlanName  string `gorm:"size:64;not null"`
	Unit      string `gorm:"size:16;not null"`
	Cycle     string `gorm:"size:16;not null"`
	Quota     int64  `gorm:"not null"`
	ExpiresAt ts
	CreatedAt ts
	UpdatedAt ts
}

func (v12Subscription) TableName() string { return "subscriptions" }

type v12CreditPack struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"index;not null"`
	PlanID    uint64 `gorm:"not null;default:0"`
	Name      string `gorm:"size:64;not null"`
	Unit      string `gorm:"size:16;not null"`
	Total     int64  `gorm:"not null"`
	Remaining int64  `gorm:"not null"`
	ExpiresAt *time.Time
	CreatedAt ts
	UpdatedAt ts
}

func (v12CreditPack) TableName() string { return "credit_packs" }

type v12CardKey struct {
	ID        uint64 `gorm:"primaryKey"`
	CodeHash  string `gorm:"size:64;uniqueIndex;not null"`
	Hint      string `gorm:"size:64;not null;default:''"`
	PlanID    uint64 `gorm:"index;not null"`
	Batch     string `gorm:"size:32;index;not null"`
	Note      string `gorm:"size:200;not null;default:''"`
	Status    string `gorm:"size:16;index;not null;default:'unused'"`
	ExpiresAt *time.Time
	UsedBy    *uint64 `gorm:"index"`
	UsedAt    *time.Time
	CreatedAt ts
}

func (v12CardKey) TableName() string { return "card_keys" }

type v12BillingLog struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"index;not null"`
	Kind      string `gorm:"size:16;not null"`
	Detail    string `gorm:"size:500;not null"`
	CreatedAt ts
}

func (v12BillingLog) TableName() string { return "billing_logs" }

func migrationV12() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610270012_billing",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range []string{"DisabledAt", "DisabledReason"} {
				if !m.HasColumn(&v12User{}, col) {
					if err := m.AddColumn(&v12User{}, col); err != nil {
						return err
					}
				}
			}
			if !m.HasColumn(&v12Message{}, "Error") {
				if err := m.AddColumn(&v12Message{}, "Error"); err != nil {
					return err
				}
			}
			return autoMigrate(tx, &v12Plan{}, &v12Subscription{}, &v12CreditPack{}, &v12CardKey{}, &v12BillingLog{})
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasColumn(&v12Message{}, "Error") {
				if err := m.DropColumn(&v12Message{}, "Error"); err != nil {
					return err
				}
			}
			if err := m.DropTable(&v12BillingLog{}, &v12CardKey{}, &v12CreditPack{}, &v12Subscription{}, &v12Plan{}); err != nil {
				return err
			}
			for _, col := range []string{"DisabledAt", "DisabledReason"} {
				if m.HasColumn(&v12User{}, col) {
					if err := m.DropColumn(&v12User{}, col); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}
