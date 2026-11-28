// Package cms 提供布谷官网文章的内容管理与公开读取能力。
package cms

import (
	"errors"
	"time"
)

// 文章发布状态。
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"
)

// 允许的封面几何主题。
const (
	ThemeObsidian = "obsidian"
	ThemeAurora   = "aurora"
	ThemeCobalt   = "cobalt"
	ThemeSage     = "sage"
	ThemeSand     = "sand"
)

// 字段长度与数量限制。
const (
	MaxSlugLen      = 120
	MaxCategoryLen  = 48
	MaxTitleLen     = 160
	MaxSummaryLen   = 500
	MaxContentRunes = 200000
	MaxAuthorLen    = 64
	MaxCoverURLLen  = 512
	MaxTags         = 20
	MaxTagLen       = 32
	MaxPublicLimit  = 100
	relatedLimit    = 3
)

// ErrNotFound 表示文章不存在或尚未发布。
var ErrNotFound = errors.New("没有这篇文章")

// ErrConflict 表示文章标识已被占用。
var ErrConflict = errors.New("文章标识已被占用，请更换后再试")

// UserError 表示提交的内容或参数不符合规范。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// postRow 是 cms_posts 表对应的持久化模型。
type postRow struct {
	ID          uint64     `gorm:"primaryKey"`
	Slug        string     `gorm:"size:120;uniqueIndex;not null"`
	Category    string     `gorm:"size:48;index;not null;default:''"`
	TagsJSON    string     `gorm:"column:tags_json;type:text;not null"`
	Title       string     `gorm:"size:160;not null"`
	Summary     string     `gorm:"size:500;not null;default:''"`
	Content     string     `gorm:"type:mediumtext;not null"`
	Author      string     `gorm:"size:64;not null;default:''"`
	CoverURL    string     `gorm:"column:cover_url;size:512;not null;default:''"`
	CoverTheme  string     `gorm:"column:cover_theme;size:32;not null;default:'obsidian'"`
	Status      string     `gorm:"size:16;index;not null;default:'draft'"`
	Featured    bool       `gorm:"not null;default:false"`
	Pinned      bool       `gorm:"not null;default:false"`
	SortOrder   int        `gorm:"not null;default:0"`
	PublishedAt *time.Time `gorm:"index"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (postRow) TableName() string { return "cms_posts" }

// PostSummary 是文章列表和关联推荐返回的摘要视图。
type PostSummary struct {
	ID          uint64     `json:"id"`
	Slug        string     `json:"slug"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Author      string     `json:"author"`
	CoverURL    string     `json:"cover_url"`
	CoverTheme  string     `json:"cover_theme"`
	Status      string     `json:"status"`
	Featured    bool       `json:"featured"`
	Pinned      bool       `json:"pinned"`
	SortOrder   int        `json:"sort_order"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// PostDetail 是单篇文章与管理后台返回的完整视图。
type PostDetail struct {
	PostSummary
	Content string `json:"content"`
}

// PostInput 是新建或编辑文章时的输入参数。
type PostInput struct {
	Slug        string     `json:"slug"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Content     string     `json:"content"`
	Author      string     `json:"author"`
	CoverURL    string     `json:"cover_url"`
	CoverTheme  string     `json:"cover_theme"`
	Status      string     `json:"status"`
	Featured    bool       `json:"featured"`
	Pinned      bool       `json:"pinned"`
	SortOrder   int        `json:"sort_order"`
	PublishedAt *time.Time `json:"published_at"`
}

// PublicListFilter 是公开文章列表的查询条件。
type PublicListFilter struct {
	Category string
	Tag      string
	Query    string
	Featured bool
	Limit    int
}

// PublicListResult 是公开文章列表接口的返回结构。
type PublicListResult struct {
	Posts      []PostSummary `json:"posts"`
	Categories []string      `json:"categories"`
	Tags       []string      `json:"tags"`
}

// PublicDetailResult 是公开文章详情接口的返回结构。
type PublicDetailResult struct {
	Post    PostDetail    `json:"post"`
	Related []PostSummary `json:"related"`
}

// AdminListFilter 是管理后台文章列表的查询条件。
type AdminListFilter struct {
	Status   string
	Category string
	Query    string
}
