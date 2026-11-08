package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v26：模型供应商（一个地址和 Key 下面挂多个模型）。以下为 v26 的快照结构体，不得修改。
// 已有的模型在启动时按「所有者 + 地址 + Key」自动归组（Key 是加密的，要在程序里解密比较）。

type v26ModelProvider struct {
	ID        uint64 `gorm:"primaryKey"`
	OwnerID   uint64 `gorm:"index;not null;default:0"`
	Name      string `gorm:"size:64;not null"`
	BaseURL   string `gorm:"size:512;not null"`
	APIKey    string `gorm:"column:api_key_enc;size:2048;not null;default:''"`
	CreatedAt ts
	UpdatedAt ts
}

func (v26ModelProvider) TableName() string { return "model_providers" }

type v26ModelConfig struct {
	ProviderID *uint64 `gorm:"index"`
}

func (v26ModelConfig) TableName() string { return "model_configs" }

func migrationV26() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202611090026_model_providers",
		Migrate: func(tx *gorm.DB) error {
			if err := autoMigrate(tx, &v26ModelProvider{}); err != nil {
				return err
			}
			return addColumns(tx.Migrator(), &v26ModelConfig{}, "ProviderID")
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasColumn(&v26ModelConfig{}, "ProviderID") {
				if err := m.DropColumn(&v26ModelConfig{}, "ProviderID"); err != nil {
					return err
				}
			}
			return m.DropTable(&v26ModelProvider{})
		},
	}
}
