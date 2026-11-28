package cms_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/cms"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/httpapi"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func forEachDB(t *testing.T, fn func(t *testing.T, db *gormstore.DB)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		db, err := gormstore.Open(config.DatabaseConfig{
			Driver: config.DriverSQLite,
			DSN:    filepath.Join(t.TempDir(), "cms.db"),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if err := db.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		fn(t, db)
	})
	if dsn := os.Getenv("BUUGOO_TEST_MYSQL_DSN"); dsn != "" {
		t.Run("mysql", func(t *testing.T) {
			db, err := gormstore.Open(config.DatabaseConfig{
				Driver:       config.DriverMySQL,
				DSN:          dsn,
				MaxOpenConns: 5,
				MaxIdleConns: 2,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if err := db.Migrate(context.Background()); err != nil {
				t.Fatal(err)
			}
			db.Gorm().Exec("DELETE FROM cms_posts")
			fn(t, db)
		})
	}
}

func TestMigrationIdempotent(t *testing.T) {
	forEachDB(t, func(t *testing.T, db *gormstore.DB) {
		ctx := context.Background()
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("重复执行迁移失败: %v", err)
		}
		pending, err := db.Pending(ctx)
		if err != nil || pending {
			t.Fatalf("迁移后不应有未完成项: pending=%v err=%v", pending, err)
		}
		statuses, err := db.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, st := range statuses {
			if st.ID == "202611280027_cms_posts" {
				found = st.Applied
			}
		}
		if !found {
			t.Fatal("未找到已应用的 202611280027_cms_posts 迁移记录")
		}
	})
}

func TestValidationSlugAndCoverURL(t *testing.T) {
	forEachDB(t, func(t *testing.T, db *gormstore.DB) {
		ctx := context.Background()
		svc := cms.New(db.Gorm())

		badSlugs := []string{
			"",
			"   ",
			"Hello-World",
			"post_1",
			"post 1",
			"../etc/passwd",
			"中文标识",
			"---",
			strings.Repeat("a", 121),
		}
		for _, slug := range badSlugs {
			_, err := svc.Create(ctx, cms.PostInput{Slug: slug, Title: "测试标题"})
			var ue *cms.UserError
			if !errors.As(err, &ue) {
				t.Fatalf("非法 slug %q 应返回 UserError，实际得到 %v", slug, err)
			}
		}

		badURLs := []string{
			"javascript:alert(1)",
			"JavaScript:alert(1)",
			"data:text/html,<script>alert(1)</script>",
			"vbscript:msgbox(1)",
			"//evil.example.com/a.png",
			"/\\evil.example.com/a.png",
			"ftp://example.com/a.png",
			"relative/path.png",
			"https://example.com/a b.png",
			"http:///missing-host",
		}
		for i, u := range badURLs {
			_, err := svc.Create(ctx, cms.PostInput{
				Slug:     fmt.Sprintf("bad-url-%d", i),
				Title:    "测试封面校验",
				CoverURL: u,
			})
			var ue *cms.UserError
			if !errors.As(err, &ue) {
				t.Fatalf("非法 cover_url %q 应被拦截，实际得到 %v", u, err)
			}
		}

		goodURLs := []string{
			"",
			"/assets/cover.png",
			"http://example.com/cover.jpg",
			"https://cdn.example.com/path/to/img.webp?v=1",
		}
		for i, u := range goodURLs {
			p, err := svc.Create(ctx, cms.PostInput{
				Slug:     fmt.Sprintf("good-url-%d", i),
				Title:    "合法封面",
				CoverURL: u,
			})
			if err != nil {
				t.Fatalf("合法 cover_url %q 创建失败: %v", u, err)
			}
			if p.CoverURL != u {
				t.Fatalf("封面链接未保留: got=%q want=%q", p.CoverURL, u)
			}
			if p.CoverTheme != cms.ThemeObsidian || p.Status != cms.StatusDraft {
				t.Fatalf("默认值不对: theme=%q status=%q", p.CoverTheme, p.Status)
			}
		}

		// 非法封面主题与状态
		if _, err := svc.Create(ctx, cms.PostInput{Slug: "bad-theme", Title: "标题", CoverTheme: "neon"}); err == nil {
			t.Fatal("非法 cover_theme 应报错")
		}
		if _, err := svc.Create(ctx, cms.PostInput{Slug: "bad-status", Title: "标题", Status: "deleted"}); err == nil {
			t.Fatal("非法 status 应报错")
		}

		// 重复 slug 返回 ErrConflict
		if _, err := svc.Create(ctx, cms.PostInput{Slug: "good-url-0", Title: "重复标识"}); !errors.Is(err, cms.ErrConflict) {
			t.Fatalf("重复 slug 应返回 ErrConflict，得到 %v", err)
		}
	})
}

func TestDraftIsolationPublishedAtAndOrdering(t *testing.T) {
	forEachDB(t, func(t *testing.T, db *gormstore.DB) {
		ctx := context.Background()
		svc := cms.New(db.Gorm())
		base := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
		svc.SetNow(func() time.Time { return base })

		// 草稿不会出现在公开接口中，也不会自动填 published_at
		draft, err := svc.Create(ctx, cms.PostInput{
			Slug:     "draft-post",
			Category: "技术",
			Tags:     []string{"草稿标签"},
			Title:    "尚未发布的草稿",
			Content:  "草稿正文",
			Status:   cms.StatusDraft,
		})
		if err != nil {
			t.Fatal(err)
		}
		if draft.PublishedAt != nil {
			t.Fatalf("草稿不应有发布时间: %v", draft.PublishedAt)
		}

		// 归档文章也不会出现在公开接口中
		if _, err := svc.Create(ctx, cms.PostInput{
			Slug:     "archived-post",
			Category: "动态",
			Title:    "已归档文章",
			Status:   cms.StatusArchived,
		}); err != nil {
			t.Fatal(err)
		}

		if _, err := svc.GetPublicBySlug(ctx, "draft-post"); !errors.Is(err, cms.ErrNotFound) {
			t.Fatalf("草稿公开访问应返回 ErrNotFound，得到 %v", err)
		}
		if _, err := svc.GetPublicBySlug(ctx, "archived-post"); !errors.Is(err, cms.ErrNotFound) {
			t.Fatalf("归档公开访问应返回 ErrNotFound，得到 %v", err)
		}

		// 草稿首次发布时自动填当前 UTC 时间
		publishTime := base.Add(2 * time.Hour)
		svc.SetNow(func() time.Time { return publishTime })
		published, err := svc.Update(ctx, draft.ID, cms.PostInput{
			Slug:       "draft-post",
			Category:   "技术",
			Tags:       []string{"长期记忆", "检索"},
			Title:      "已发布文章一",
			Summary:    "介绍语义检索机制",
			Content:    "正文包含向量细节",
			CoverTheme: cms.ThemeCobalt,
			Status:     cms.StatusPublished,
			SortOrder:  20,
		})
		if err != nil {
			t.Fatal(err)
		}
		if published.PublishedAt == nil || !published.PublishedAt.Equal(publishTime) {
			t.Fatalf("首次发布应自动填当前 UTC 时间: got=%v want=%v", published.PublishedAt, publishTime)
		}

		// 再次编辑已发布文章且不传 published_at 时保留原发布时间
		svc.SetNow(func() time.Time { return publishTime.Add(3 * time.Hour) })
		edited, err := svc.Update(ctx, draft.ID, cms.PostInput{
			Slug:       "draft-post",
			Category:   "技术",
			Tags:       []string{"长期记忆", "检索"},
			Title:      "已发布文章一（修订）",
			Summary:    "介绍语义检索机制",
			Content:    "正文包含向量细节",
			CoverTheme: cms.ThemeCobalt,
			Status:     cms.StatusPublished,
			SortOrder:  20,
		})
		if err != nil {
			t.Fatal(err)
		}
		if edited.PublishedAt == nil || !edited.PublishedAt.Equal(publishTime) {
			t.Fatalf("修订不应丢失原发布时间: got=%v want=%v", edited.PublishedAt, publishTime)
		}

		// 插入多篇用于验证排序与筛选
		tEarly := base.Add(-10 * time.Hour)
		tLate := base.Add(-1 * time.Hour)
		posts := []cms.PostInput{
			{
				Slug:        "pinned-post",
				Category:    "动态",
				Tags:        []string{"发布"},
				Title:       "置顶文章",
				Status:      cms.StatusPublished,
				Pinned:      true,
				Featured:    false,
				SortOrder:   50,
				PublishedAt: &tEarly,
			},
			{
				Slug:        "featured-a",
				Category:    "产品",
				Tags:        []string{"情绪", "检索"},
				Title:       "精选靠前",
				Status:      cms.StatusPublished,
				Featured:    true,
				SortOrder:   5,
				PublishedAt: &tEarly,
			},
			{
				Slug:        "featured-b",
				Category:    "产品",
				Tags:        []string{"性格"},
				Title:       "精选靠后",
				Status:      cms.StatusPublished,
				Featured:    true,
				SortOrder:   10,
				PublishedAt: &tLate,
			},
		}
		for _, p := range posts {
			if _, err := svc.Create(ctx, p); err != nil {
				t.Fatal(err)
			}
		}

		// 排序验证：pinned DESC, featured DESC, sort_order ASC, published_at DESC, id DESC
		res, err := svc.ListPublic(ctx, cms.PublicListFilter{})
		if err != nil {
			t.Fatal(err)
		}
		wantOrder := []string{"pinned-post", "featured-a", "featured-b", "draft-post"}
		if len(res.Posts) != len(wantOrder) {
			t.Fatalf("公开文章数量不对: got=%d want=%d", len(res.Posts), len(wantOrder))
		}
		for i, slug := range wantOrder {
			if res.Posts[i].Slug != slug {
				t.Fatalf("排序第 %d 位不对: got=%s want=%s", i, res.Posts[i].Slug, slug)
			}
		}

		// 筛选 featured=true 且 limit=1，但 categories/tags 仍覆盖全部已发布文章
		filtered, err := svc.ListPublic(ctx, cms.PublicListFilter{Featured: true, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(filtered.Posts) != 1 || filtered.Posts[0].Slug != "featured-a" {
			t.Fatalf("精选筛选+limit 不对: %+v", filtered.Posts)
		}
		if len(filtered.Categories) != 3 || len(filtered.Tags) != 5 {
			t.Fatalf("分类和标签应基于全部已发布文章汇总: categories=%v tags=%v", filtered.Categories, filtered.Tags)
		}

		// 按标签与关键词筛选
		byTag, err := svc.ListPublic(ctx, cms.PublicListFilter{Tag: "检索"})
		if err != nil || len(byTag.Posts) != 2 {
			t.Fatalf("按标签筛选不对: count=%d err=%v", len(byTag.Posts), err)
		}
		byQuery, err := svc.ListPublic(ctx, cms.PublicListFilter{Query: "向量细节"})
		if err != nil || len(byQuery.Posts) != 1 || byQuery.Posts[0].Slug != "draft-post" {
			t.Fatalf("按正文关键词筛选不对: %+v err=%v", byQuery.Posts, err)
		}
	})
}

func TestRelatedPostsAndSeedIdempotent(t *testing.T) {
	forEachDB(t, func(t *testing.T, db *gormstore.DB) {
		ctx := context.Background()
		svc := cms.New(db.Gorm())

		// 预先创建一条同名 slug，验证 SeedDemo 不覆盖已有文章
		existing, err := svc.Create(ctx, cms.PostInput{
			Slug:       "buugoo-memory-architecture",
			Category:   "技术",
			Title:      "自定义保留文章",
			Content:    "保留原内容",
			CoverTheme: cms.ThemeSand,
			Status:     cms.StatusPublished,
		})
		if err != nil {
			t.Fatal(err)
		}

		created, err := svc.SeedDemo(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if created != 4 {
			t.Fatalf("已有 1 篇同名 slug，应新增 4 篇示例文章，实际=%d", created)
		}
		createdAgain, err := svc.SeedDemo(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if createdAgain != 0 {
			t.Fatalf("重复执行 SeedDemo 应返回 0，实际=%d", createdAgain)
		}

		// 确认已有 slug 未被覆盖
		detail, err := svc.GetPublicBySlug(ctx, existing.Slug)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Post.Title != "自定义保留文章" || detail.Post.Content != "保留原内容" {
			t.Fatalf("SeedDemo 不应覆盖已有文章: %+v", detail.Post)
		}

		// 关联推荐：最多 3 篇，不含自身，优先同分类
		guideDetail, err := svc.GetPublicBySlug(ctx, "getting-started-with-buugoo")
		if err != nil {
			t.Fatal(err)
		}
		if len(guideDetail.Related) != 3 {
			t.Fatalf("关联推荐应返回 3 篇，实际=%d", len(guideDetail.Related))
		}
		if guideDetail.Related[0].Slug != "buugoo-self-hosting-backup-guide" {
			t.Fatalf("关联推荐首位应优先同分类文章，实际=%s", guideDetail.Related[0].Slug)
		}
		for _, r := range guideDetail.Related {
			if r.Slug == guideDetail.Post.Slug {
				t.Fatal("关联推荐不应包含文章自身")
			}
		}

		// 验证全新库 SeedDemo 载入的 5 篇示例文章内容规范（无感叹号、无他/她、无 emoji、含公式）
		db.Gorm().Exec("DELETE FROM cms_posts")
		allCreated, err := svc.SeedDemo(ctx)
		if err != nil || allCreated != 5 {
			t.Fatalf("全新载入应创建 5 篇，实际=%d err=%v", allCreated, err)
		}
		adminList, err := svc.ListAdmin(ctx, cms.AdminListFilter{})
		if err != nil || len(adminList) != 5 {
			t.Fatalf("后台应能查到 5 篇示例文章，实际=%d err=%v", len(adminList), err)
		}
		hasMath := false
		for _, p := range adminList {
			combined := p.Title + "\n" + p.Summary + "\n" + p.Content + "\n" + p.Author + "\n" + p.Category + "\n" + strings.Join(p.Tags, ",")
			for _, forbidden := range []string{"他", "她", "!", "！"} {
				if strings.Contains(combined, forbidden) {
					t.Fatalf("示例文章 %s 含禁用字符 %q", p.Slug, forbidden)
				}
			}
			for _, r := range combined {
				if r >= 0x1F300 && r <= 0x1FAFF {
					t.Fatalf("示例文章 %s 不应包含 emoji: %U", p.Slug, r)
				}
			}
			if strings.Contains(p.Content, "$$") {
				hasMath = true
			}
		}
		if !hasMath {
			t.Fatal("示例文章应包含 Markdown 公式示例")
		}
	})
}

func TestHTTPAPIContractAndDefaultPasswordGuard(t *testing.T) {
	db, err := gormstore.Open(config.DatabaseConfig{
		Driver: config.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "http.db"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := cms.New(db.Gorm())

	// 1. 默认密码 1228 必须拦截管理后台 CMS 写接口（返回 403）
	weakCfg := config.Default()
	weakCfg.Admin.Password = config.DefaultAdminPassword
	weakHandler := httpapi.New(httpapi.Deps{Config: weakCfg, CMS: svc})
	weakTok := loginConsole(t, weakHandler, config.DefaultAdminPassword)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/console/cms/posts", ""},
		{http.MethodPost, "/api/v1/console/cms/posts", `{"slug":"p1","title":"标题"}`},
		{http.MethodPut, "/api/v1/console/cms/posts/1", `{"slug":"p1","title":"标题"}`},
		{http.MethodDelete, "/api/v1/console/cms/posts/1", ""},
		{http.MethodPost, "/api/v1/console/cms/seed", ""},
	} {
		rec := doReq(weakHandler, tc.method, tc.path, tc.body, weakTok)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("默认密码请求 %s %s 应返回 403，实际=%d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// 2. 强密码下的完整管理接口与公开接口契约验证
	strongCfg := config.Default()
	strongCfg.Admin.Password = "strong-test-pass"
	h := httpapi.New(httpapi.Deps{Config: strongCfg, CMS: svc})

	// 未登录管理后台返回 401
	if rec := doReq(h, http.MethodGet, "/api/v1/console/cms/posts", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录后台应返回 401，实际=%d", rec.Code)
	}

	tok := loginConsole(t, h, "strong-test-pass")

	// 新建草稿 -> 201
	rec := doReq(h, http.MethodPost, "/api/v1/console/cms/posts",
		`{"slug":"hello-cms","category":"产品","tags":["入门"],"title":"第一篇文章","summary":"简要说明","content":"# 标题\n正文","author":"布谷","cover_theme":"aurora","status":"draft","featured":true,"published_at":""}`, tok)
	if rec.Code != http.StatusCreated {
		t.Fatalf("新建文章应返回 201，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var createdResp struct {
		Post cms.PostDetail `json:"post"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createdResp); err != nil {
		t.Fatal(err)
	}
	if createdResp.Post.ID == 0 || createdResp.Post.PublishedAt != nil {
		t.Fatalf("新建草稿响应异常: %+v", createdResp.Post)
	}

	// 公开接口不可见草稿 -> 404
	if rec := doReq(h, http.MethodGet, "/api/v1/cms/posts/hello-cms", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("未发布草稿公开详情应返回 404，实际=%d", rec.Code)
	}

	// 重复 slug -> 409
	if rec := doReq(h, http.MethodPost, "/api/v1/console/cms/posts",
		`{"slug":"hello-cms","title":"冲突文章"}`, tok); rec.Code != http.StatusConflict {
		t.Fatalf("重复 slug 应返回 409，实际=%d body=%s", rec.Code, rec.Body.String())
	}

	// XSS 封面链接 -> 400
	if rec := doReq(h, http.MethodPost, "/api/v1/console/cms/posts",
		`{"slug":"xss-test","title":"测试","cover_url":"javascript:alert(1)"}`, tok); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法封面链接应返回 400，实际=%d body=%s", rec.Code, rec.Body.String())
	}

	// 更新为已发布 -> 200 且自动填 published_at
	updatePath := fmt.Sprintf("/api/v1/console/cms/posts/%d", createdResp.Post.ID)
	rec = doReq(h, http.MethodPut, updatePath,
		`{"slug":"hello-cms","category":"产品","tags":["入门"],"title":"第一篇文章（正式版）","summary":"简要说明","content":"# 标题\n正文","author":"布谷","cover_theme":"aurora","status":"published","featured":true,"published_at":""}`, tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("更新文章应返回 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var updatedResp struct {
		Post cms.PostDetail `json:"post"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updatedResp); err != nil {
		t.Fatal(err)
	}
	if updatedResp.Post.PublishedAt == nil {
		t.Fatal("发布后应自动生成 published_at")
	}

	// Seed 接口 -> 200 { created: 5 }，重复调用 -> { created: 0 }
	rec = doReq(h, http.MethodPost, "/api/v1/console/cms/seed", "", tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("seed 应返回 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var seedResp struct {
		Created int `json:"created"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &seedResp); err != nil || seedResp.Created != 5 {
		t.Fatalf("首次 seed 应返回 created=5，实际=%s", rec.Body.String())
	}
	rec = doReq(h, http.MethodPost, "/api/v1/console/cms/seed", "", tok)
	if err := json.Unmarshal(rec.Body.Bytes(), &seedResp); err != nil || seedResp.Created != 0 {
		t.Fatalf("重复 seed 应返回 created=0，实际=%s", rec.Body.String())
	}

	// 公开列表与详情接口（无需任何登录态）
	rec = doReq(h, http.MethodGet, "/api/v1/cms/posts?featured=1&limit=2", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("公开列表应返回 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var listResp cms.PublicListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Posts) != 2 || len(listResp.Categories) == 0 || len(listResp.Tags) == 0 {
		t.Fatalf("公开列表返回不完整: %+v", listResp)
	}

	rec = doReq(h, http.MethodGet, "/api/v1/cms/posts/hello-cms", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("公开详情应返回 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var detailResp cms.PublicDetailResult
	if err := json.Unmarshal(rec.Body.Bytes(), &detailResp); err != nil {
		t.Fatal(err)
	}
	if detailResp.Post.Content == "" || len(detailResp.Related) != 3 {
		t.Fatalf("公开详情返回不完整: post=%+v related=%d", detailResp.Post, len(detailResp.Related))
	}

	// 后台列表筛选
	rec = doReq(h, http.MethodGet, "/api/v1/console/cms/posts?status=published&category=%E4%BA%A7%E5%93%81&q=%E6%AD%A3%E5%BC%8F%E7%89%88", "", tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("后台列表应返回 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var adminListResp struct {
		Posts []cms.PostDetail `json:"posts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &adminListResp); err != nil || len(adminListResp.Posts) != 1 {
		t.Fatalf("后台列表筛选结果不对: %s", rec.Body.String())
	}

	// 删除文章 -> 204，再删 -> 404
	if rec := doReq(h, http.MethodDelete, updatePath, "", tok); rec.Code != http.StatusNoContent {
		t.Fatalf("删除文章应返回 204，实际=%d", rec.Code)
	}
	if rec := doReq(h, http.MethodDelete, updatePath, "", tok); rec.Code != http.StatusNotFound {
		t.Fatalf("重复删除应返回 404，实际=%d", rec.Code)
	}
}

func loginConsole(t *testing.T, h http.Handler, password string) string {
	t.Helper()
	rec := doReq(h, http.MethodPost, "/api/v1/console/login", fmt.Sprintf(`{"password":%q}`, password), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("管理后台登录失败: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Token == "" {
		t.Fatalf("未返回 token: %s", rec.Body.String())
	}
	return resp.Token
}

func doReq(h http.Handler, method, path, body, consoleToken string) *httptest.ResponseRecorder {
	var r *bytes.Reader
	if body != "" {
		r = bytes.NewReader([]byte(body))
	} else {
		r = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if consoleToken != "" {
		req.Header.Set("X-Console-Token", consoleToken)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
