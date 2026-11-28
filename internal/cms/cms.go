package cms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Service 管理官网文章的读写、筛选、关联推荐与示例载入。
type Service struct {
	db  *gorm.DB
	now func() time.Time
}

// New 创建文章管理服务。
func New(db *gorm.DB) *Service {
	return &Service{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// SetNow 替换当前时间获取函数，仅用于测试场景控制时间。
func (s *Service) SetNow(fn func() time.Time) {
	if fn != nil {
		s.now = fn
	}
}

var summaryColumns = []string{
	"id", "slug", "category", "tags_json", "title", "summary", "author",
	"cover_url", "cover_theme", "status", "featured", "pinned", "sort_order",
	"published_at", "created_at", "updated_at",
}

const (
	publicOrderSQL = "pinned DESC, featured DESC, sort_order ASC, published_at DESC, id DESC"
	adminOrderSQL  = "pinned DESC, featured DESC, sort_order ASC, updated_at DESC, id DESC"
)

// ListPublic 返回公开已发布文章列表，以及基于全部已发布文章汇总去重排序后的分类和标签。
func (s *Service) ListPublic(ctx context.Context, filter PublicListFilter) (*PublicListResult, error) {
	q := strings.ToLower(strings.TrimSpace(filter.Query))
	catFilter := strings.TrimSpace(filter.Category)
	tagFilter := strings.TrimSpace(filter.Tag)

	db := s.db.WithContext(ctx).Model(&postRow{}).Where("status = ?", StatusPublished).Order(publicOrderSQL)
	if q == "" {
		db = db.Select(summaryColumns)
	}
	var rows []postRow
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}

	catSet := map[string]struct{}{}
	tagSet := map[string]struct{}{}
	posts := make([]PostSummary, 0, len(rows))

	for i := range rows {
		r := &rows[i]
		tags := decodeTags(r.TagsJSON)
		if r.Category != "" {
			catSet[r.Category] = struct{}{}
		}
		for _, t := range tags {
			tagSet[t] = struct{}{}
		}

		if filter.Featured && !r.Featured {
			continue
		}
		if catFilter != "" && r.Category != catFilter {
			continue
		}
		if tagFilter != "" && !hasTag(tags, tagFilter) {
			continue
		}
		if q != "" && !matchesQuery(r, tags, q) {
			continue
		}
		posts = append(posts, toSummary(r, tags))
	}

	if filter.Limit > 0 {
		limit := min(filter.Limit, MaxPublicLimit)
		if len(posts) > limit {
			posts = posts[:limit]
		}
	}

	categories := sortedKeys(catSet)
	tags := sortedKeys(tagSet)
	return &PublicListResult{
		Posts:      posts,
		Categories: categories,
		Tags:       tags,
	}, nil
}

// GetPublicBySlug 按标识返回单篇已发布文章和最多 3 篇关联推荐。未发布或不存在时返回 ErrNotFound。
func (s *Service) GetPublicBySlug(ctx context.Context, slug string) (*PublicDetailResult, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, ErrNotFound
	}
	var row postRow
	err := s.db.WithContext(ctx).Where("slug = ? AND status = ?", slug, StatusPublished).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	related, err := s.findRelated(ctx, &row)
	if err != nil {
		return nil, err
	}
	return &PublicDetailResult{
		Post:    toDetail(&row, decodeTags(row.TagsJSON)),
		Related: related,
	}, nil
}

func (s *Service) findRelated(ctx context.Context, current *postRow) ([]PostSummary, error) {
	related := make([]PostSummary, 0, relatedLimit)
	seen := map[uint64]struct{}{current.ID: {}}

	if current.Category != "" {
		var sameCat []postRow
		err := s.db.WithContext(ctx).
			Select(summaryColumns).
			Where("status = ? AND id <> ? AND category = ?", StatusPublished, current.ID, current.Category).
			Order("published_at DESC, id DESC").
			Limit(relatedLimit).
			Find(&sameCat).Error
		if err != nil {
			return nil, err
		}
		for i := range sameCat {
			r := &sameCat[i]
			seen[r.ID] = struct{}{}
			related = append(related, toSummary(r, decodeTags(r.TagsJSON)))
		}
	}

	if len(related) < relatedLimit {
		need := relatedLimit - len(related)
		var latest []postRow
		err := s.db.WithContext(ctx).
			Select(summaryColumns).
			Where("status = ? AND id <> ?", StatusPublished, current.ID).
			Order("published_at DESC, id DESC").
			Limit(relatedLimit + len(seen)).
			Find(&latest).Error
		if err != nil {
			return nil, err
		}
		for i := range latest {
			if need <= 0 {
				break
			}
			r := &latest[i]
			if _, ok := seen[r.ID]; ok {
				continue
			}
			seen[r.ID] = struct{}{}
			related = append(related, toSummary(r, decodeTags(r.TagsJSON)))
			need--
		}
	}
	return related, nil
}

// ListAdmin 返回管理后台的全部文章（含草稿、已发布与已归档），支持按状态、分类与关键词筛选。
func (s *Service) ListAdmin(ctx context.Context, filter AdminListFilter) ([]PostDetail, error) {
	status := strings.ToLower(strings.TrimSpace(filter.Status))
	category := strings.TrimSpace(filter.Category)
	q := strings.ToLower(strings.TrimSpace(filter.Query))

	db := s.db.WithContext(ctx).Model(&postRow{}).Order(adminOrderSQL)
	if status != "" {
		if !validStatus(status) {
			return nil, &UserError{"文章状态筛选无效，可选值：draft、published、archived"}
		}
		db = db.Where("status = ?", status)
	}
	if category != "" {
		db = db.Where("category = ?", category)
	}

	var rows []postRow
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]PostDetail, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		tags := decodeTags(r.TagsJSON)
		if q != "" && !matchesQuery(r, tags, q) {
			continue
		}
		out = append(out, toDetail(r, tags))
	}
	return out, nil
}

// Create 新建一篇文章。直接以 published 状态创建且未填写发布时间时，自动填入当前 UTC 时间。
func (s *Service) Create(ctx context.Context, in PostInput) (*PostDetail, error) {
	cleaned, tags, err := cleanInput(in)
	if err != nil {
		return nil, err
	}
	if err := s.checkSlugUnique(ctx, cleaned.Slug, 0); err != nil {
		return nil, err
	}

	var publishedAt *time.Time
	if cleaned.PublishedAt != nil {
		t := cleaned.PublishedAt.UTC()
		publishedAt = &t
	} else if cleaned.Status == StatusPublished {
		t := s.now().UTC()
		publishedAt = &t
	}

	row := postRow{
		Slug:        cleaned.Slug,
		Category:    cleaned.Category,
		TagsJSON:    encodeTags(tags),
		Title:       cleaned.Title,
		Summary:     cleaned.Summary,
		Content:     cleaned.Content,
		Author:      cleaned.Author,
		CoverURL:    cleaned.CoverURL,
		CoverTheme:  cleaned.CoverTheme,
		Status:      cleaned.Status,
		Featured:    cleaned.Featured,
		Pinned:      cleaned.Pinned,
		SortOrder:   cleaned.SortOrder,
		PublishedAt: publishedAt,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	detail := toDetail(&row, tags)
	return &detail, nil
}

// Update 更新指定文章。首次变为 published 且发布时间为空时自动填入当前 UTC 时间。
func (s *Service) Update(ctx context.Context, id uint64, in PostInput) (*PostDetail, error) {
	cleaned, tags, err := cleanInput(in)
	if err != nil {
		return nil, err
	}
	var row postRow
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if cleaned.Slug != row.Slug {
		if err := s.checkSlugUnique(ctx, cleaned.Slug, id); err != nil {
			return nil, err
		}
	}

	if cleaned.PublishedAt != nil {
		t := cleaned.PublishedAt.UTC()
		row.PublishedAt = &t
	} else if cleaned.Status == StatusPublished && row.PublishedAt == nil {
		t := s.now().UTC()
		row.PublishedAt = &t
	}

	row.Slug = cleaned.Slug
	row.Category = cleaned.Category
	row.TagsJSON = encodeTags(tags)
	row.Title = cleaned.Title
	row.Summary = cleaned.Summary
	row.Content = cleaned.Content
	row.Author = cleaned.Author
	row.CoverURL = cleaned.CoverURL
	row.CoverTheme = cleaned.CoverTheme
	row.Status = cleaned.Status
	row.Featured = cleaned.Featured
	row.Pinned = cleaned.Pinned
	row.SortOrder = cleaned.SortOrder

	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	detail := toDetail(&row, tags)
	return &detail, nil
}

// Delete 删除指定文章。不存在时返回 ErrNotFound。
func (s *Service) Delete(ctx context.Context, id uint64) error {
	res := s.db.WithContext(ctx).Delete(&postRow{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) checkSlugUnique(ctx context.Context, slug string, excludeID uint64) error {
	var count int64
	q := s.db.WithContext(ctx).Model(&postRow{}).Where("slug = ?", slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrConflict
	}
	return nil
}

func cleanInput(in PostInput) (PostInput, []string, error) {
	in.Slug = strings.TrimSpace(in.Slug)
	if err := validateSlug(in.Slug); err != nil {
		return in, nil, err
	}

	in.Title = strings.Join(strings.Fields(in.Title), " ")
	switch n := utf8.RuneCountInString(in.Title); {
	case n == 0:
		return in, nil, &UserError{"标题不能为空"}
	case n > MaxTitleLen:
		return in, nil, &UserError{fmt.Sprintf("标题最多 %d 个字符", MaxTitleLen)}
	}

	in.Category = strings.Join(strings.Fields(in.Category), " ")
	if utf8.RuneCountInString(in.Category) > MaxCategoryLen {
		return in, nil, &UserError{fmt.Sprintf("分类最多 %d 个字符", MaxCategoryLen)}
	}

	in.Summary = strings.TrimSpace(strings.ReplaceAll(in.Summary, "\r\n", "\n"))
	if utf8.RuneCountInString(in.Summary) > MaxSummaryLen {
		return in, nil, &UserError{fmt.Sprintf("摘要最多 %d 个字符", MaxSummaryLen)}
	}

	in.Content = strings.TrimSpace(strings.ReplaceAll(in.Content, "\r\n", "\n"))
	if utf8.RuneCountInString(in.Content) > MaxContentRunes {
		return in, nil, &UserError{fmt.Sprintf("正文最多 %d 个字符", MaxContentRunes)}
	}

	in.Author = strings.Join(strings.Fields(in.Author), " ")
	if utf8.RuneCountInString(in.Author) > MaxAuthorLen {
		return in, nil, &UserError{fmt.Sprintf("作者名称最多 %d 个字符", MaxAuthorLen)}
	}

	in.CoverURL = strings.TrimSpace(in.CoverURL)
	if err := validateCoverURL(in.CoverURL); err != nil {
		return in, nil, err
	}

	in.CoverTheme = strings.ToLower(strings.TrimSpace(in.CoverTheme))
	if in.CoverTheme == "" {
		in.CoverTheme = ThemeObsidian
	}
	if !validCoverTheme(in.CoverTheme) {
		return in, nil, &UserError{"封面主题无效，可选值：obsidian、aurora、cobalt、sage、sand"}
	}

	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	if in.Status == "" {
		in.Status = StatusDraft
	}
	if !validStatus(in.Status) {
		return in, nil, &UserError{"文章状态无效，可选值：draft、published、archived"}
	}

	tags, err := cleanTags(in.Tags)
	if err != nil {
		return in, nil, err
	}
	in.Tags = tags
	return in, tags, nil
}

func validateSlug(slug string) error {
	if len(slug) == 0 {
		return &UserError{"文章标识不能为空"}
	}
	if len(slug) > MaxSlugLen {
		return &UserError{fmt.Sprintf("文章标识最多 %d 个字符", MaxSlugLen)}
	}
	hasAlphaNum := false
	for i := 0; i < len(slug); i++ {
		c := slug[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			hasAlphaNum = true
			continue
		}
		if c == '-' {
			continue
		}
		return &UserError{"文章标识只能包含小写字母、数字和连字符"}
	}
	if !hasAlphaNum {
		return &UserError{"文章标识不能只包含连字符"}
	}
	return nil
}

func validateCoverURL(raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > MaxCoverURLLen {
		return &UserError{fmt.Sprintf("封面链接最多 %d 个字符", MaxCoverURLLen)}
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f {
			return &UserError{"封面链接不能包含空白或控制字符"}
		}
	}
	if strings.HasPrefix(raw, "/") {
		if strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") {
			return &UserError{"站内封面路径无效，不能以 // 开头或包含反斜杠"}
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return &UserError{"封面链接格式无效"}
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Host == "" {
		return &UserError{"封面链接仅支持站内路径（以 / 开头）或 http/https 网址"}
	}
	return nil
}

func validCoverTheme(theme string) bool {
	switch theme {
	case ThemeObsidian, ThemeAurora, ThemeCobalt, ThemeSage, ThemeSand:
		return true
	default:
		return false
	}
}

func validStatus(status string) bool {
	switch status {
	case StatusDraft, StatusPublished, StatusArchived:
		return true
	default:
		return false
	}
}

func cleanTags(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, t := range raw {
		t = strings.Join(strings.Fields(t), " ")
		if t == "" {
			continue
		}
		if utf8.RuneCountInString(t) > MaxTagLen {
			return nil, &UserError{fmt.Sprintf("单个标签最多 %d 个字符", MaxTagLen)}
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
		if len(out) > MaxTags {
			return nil, &UserError{fmt.Sprintf("最多添加 %d 个标签", MaxTags)}
		}
	}
	return out, nil
}

func encodeTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil || tags == nil {
		return []string{}
	}
	return tags
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}

func matchesQuery(r *postRow, tags []string, q string) bool {
	if strings.Contains(strings.ToLower(r.Title), q) ||
		strings.Contains(strings.ToLower(r.Summary), q) ||
		strings.Contains(strings.ToLower(r.Slug), q) ||
		strings.Contains(strings.ToLower(r.Category), q) ||
		strings.Contains(strings.ToLower(r.Author), q) ||
		strings.Contains(strings.ToLower(r.Content), q) {
		return true
	}
	for _, t := range tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

func toSummary(r *postRow, tags []string) PostSummary {
	if tags == nil {
		tags = []string{}
	}
	return PostSummary{
		ID:          r.ID,
		Slug:        r.Slug,
		Category:    r.Category,
		Tags:        tags,
		Title:       r.Title,
		Summary:     r.Summary,
		Author:      r.Author,
		CoverURL:    r.CoverURL,
		CoverTheme:  r.CoverTheme,
		Status:      r.Status,
		Featured:    r.Featured,
		Pinned:      r.Pinned,
		SortOrder:   r.SortOrder,
		PublishedAt: r.PublishedAt,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func toDetail(r *postRow, tags []string) PostDetail {
	return PostDetail{
		PostSummary: toSummary(r, tags),
		Content:     r.Content,
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func isUniqueViolation(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "Duplicate entry") ||
		strings.Contains(msg, "Error 1062")
}
