package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v14：按价格扣余额。套餐可以同时限消息、token、搜索次数；新增钱包、每日消费、计费设置；旧的余额包停用。
// 以下为 v14 的快照结构体，不得修改。

type v14Usage struct {
	Searches int `gorm:"not null;default:0"`
}

func (v14Usage) TableName() string { return "usage_daily" }

type v14Plan struct {
	QuotaMessages int64 `gorm:"not null;default:0"`
	QuotaTokens   int64 `gorm:"not null;default:0"`
	QuotaSearches int64 `gorm:"not null;default:0"`
	Amount        int64 `gorm:"not null;default:0"`
}

func (v14Plan) TableName() string { return "plans" }

type v14Subscription struct {
	QuotaMessages int64 `gorm:"not null;default:0"`
	QuotaTokens   int64 `gorm:"not null;default:0"`
	QuotaSearches int64 `gorm:"not null;default:0"`
}

func (v14Subscription) TableName() string { return "subscriptions" }

type v14Wallet struct {
	UserID    uint64 `gorm:"primaryKey;autoIncrement:false"`
	Balance   int64  `gorm:"not null;default:0"`
	UpdatedAt ts
}

func (v14Wallet) TableName() string { return "wallets" }

type v14CostDaily struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"uniqueIndex:idx_cost_user_day_key,priority:1;not null"`
	Day       string `gorm:"size:10;uniqueIndex:idx_cost_user_day_key,priority:2;not null"`
	Key       string `gorm:"size:48;uniqueIndex:idx_cost_user_day_key,priority:3;not null"`
	Amount    int64  `gorm:"not null;default:0"`
	Calls     int    `gorm:"not null;default:0"`
	Tokens    int64  `gorm:"not null;default:0"`
	UpdatedAt ts
}

func (v14CostDaily) TableName() string { return "cost_dailies" }

type v14BillingSetting struct {
	Key       string `gorm:"primaryKey;size:64"`
	Value     string `gorm:"type:text"`
	UpdatedAt ts
}

func (v14BillingSetting) TableName() string { return "billing_settings" }

func addColumns(m gorm.Migrator, model any, cols ...string) error {
	for _, c := range cols {
		if !m.HasColumn(model, c) {
			if err := m.AddColumn(model, c); err != nil {
				return err
			}
		}
	}
	return nil
}

func migrationV14() *gormigrate.Migration {
	quotaCols := []string{"QuotaMessages", "QuotaTokens", "QuotaSearches"}
	return &gormigrate.Migration{
		ID: "202610280014_wallet",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if err := addColumns(m, &v14Usage{}, "Searches"); err != nil {
				return err
			}
			if err := addColumns(m, &v14Plan{}, append(quotaCols, "Amount")...); err != nil {
				return err
			}
			if err := addColumns(m, &v14Subscription{}, quotaCols...); err != nil {
				return err
			}
			if err := autoMigrate(tx, &v14Wallet{}, &v14CostDaily{}, &v14BillingSetting{}); err != nil {
				return err
			}
			// 旧的单项额度搬到对应的列；旧余额包隐藏（已停用）。
			for _, table := range []string{"plans", "subscriptions"} {
				for unit, col := range map[string]string{"messages": "quota_messages", "tokens": "quota_tokens"} {
					q := tx.Table(table).Where("unit = ?", unit)
					if table == "plans" {
						q = q.Where("kind = ?", "period")
					}
					if err := q.Update(col, gorm.Expr("quota")).Error; err != nil {
						return err
					}
				}
			}
			return tx.Table("plans").Where("kind = ?", "pack").Update("visible", false).Error
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, t := range []any{&v14Wallet{}, &v14CostDaily{}, &v14BillingSetting{}} {
				if err := m.DropTable(t); err != nil {
					return err
				}
			}
			drop := func(model any, cols ...string) error {
				for _, c := range cols {
					if m.HasColumn(model, c) {
						if err := m.DropColumn(model, c); err != nil {
							return err
						}
					}
				}
				return nil
			}
			if err := drop(&v14Usage{}, "Searches"); err != nil {
				return err
			}
			if err := drop(&v14Plan{}, append(quotaCols, "Amount")...); err != nil {
				return err
			}
			return drop(&v14Subscription{}, quotaCols...)
		},
	}
}
