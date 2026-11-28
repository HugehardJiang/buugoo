package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v27：文章 CMS（cms_posts 表）。以下为 v27 的快照结构体，不得修改。

type v27CmsPost struct {
	ID          uint64 `gorm:"primaryKey"`
	Slug        string `gorm:"size:120;uniqueIndex;not null"`
	Category    string `gorm:"size:48;index;not null;default:''"`
	TagsJSON    string `gorm:"column:tags_json;type:text;not null"`
	Title       string `gorm:"size:160;not null"`
	Summary     string `gorm:"size:500;not null;default:''"`
	Content     string `gorm:"type:mediumtext;not null"`
	Author      string `gorm:"size:64;not null;default:''"`
	CoverURL    string `gorm:"column:cover_url;size:512;not null;default:''"`
	CoverTheme  string `gorm:"column:cover_theme;size:32;not null;default:'obsidian'"`
	Status      string `gorm:"size:16;index;not null;default:'draft'"`
	Featured    bool   `gorm:"not null;default:false"`
	Pinned      bool   `gorm:"not null;default:false"`
	SortOrder   int    `gorm:"not null;default:0"`
	PublishedAt tsPtr  `gorm:"index"`
	CreatedAt   ts
	UpdatedAt   ts
}

func (v27CmsPost) TableName() string { return "cms_posts" }

func migrationV27() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202611280027_cms_posts",
		Migrate: func(tx *gorm.DB) error {
			return autoMigrate(tx, &v27CmsPost{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(&v27CmsPost{})
		},
	}
}
