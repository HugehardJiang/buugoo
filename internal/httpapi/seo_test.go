package httpapi

import (
	"compress/gzip"
	"context"
	"encoding/xml"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/HugehardJiang/buugoo/internal/cms"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func setupSEOTestServer(t *testing.T, mode string) (http.Handler, *cms.Service) {
	t.Helper()

	rawIndex, err := os.ReadFile(filepath.Join("..", "..", "web", "index.html"))
	if err != nil {
		t.Fatalf("读取 web/index.html 失败: %v", err)
	}
	origFS, origBuilt := loadWebFS, isWebBuilt
	loadWebFS = func() fs.FS {
		return fstest.MapFS{
			"index.html":        &fstest.MapFile{Data: rawIndex},
			"assets/app-123.js": &fstest.MapFile{Data: []byte("console.log('ok');")},
		}
	}
	isWebBuilt = func() bool { return true }
	t.Cleanup(func() {
		loadWebFS = origFS
		isWebBuilt = origBuilt
	})

	db, err := gormstore.Open(config.DatabaseConfig{
		Driver: config.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "seo.db"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	svc := cms.New(db.Gorm())
	cfg := config.Default()
	cfg.Mode = mode
	cfg.Home.Title = "布谷官网测试标题"
	cfg.Home.Tagline = "有记忆、有情绪的长期陪伴体验"
	h := New(Deps{Config: cfg, CMS: svc})
	return h, svc
}

func doSEOReq(h http.Handler, path, host, proto, acceptEnc string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if host != "" {
		req.Host = host
	}
	if proto != "" {
		req.Header.Set("X-Forwarded-Proto", proto)
	}
	if acceptEnc != "" {
		req.Header.Set("Accept-Encoding", acceptEnc)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSEORobotsSitemapAndRSS(t *testing.T) {
	h, svc := setupSEOTestServer(t, config.ModeMulti)
	ctx := context.Background()
	pubTime := time.Date(2026, 11, 28, 8, 30, 0, 0, time.UTC)

	if _, err := svc.Create(ctx, cms.PostInput{
		Slug:        "pub-article",
		Category:    "技术",
		Tags:        []string{"记忆", "SEO"},
		Title:       "公开技术文章",
		Summary:     "这是一篇公开文章摘要",
		Content:     "正文内容",
		Author:      "布谷团队",
		Status:      cms.StatusPublished,
		PublishedAt: &pubTime,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, cms.PostInput{
		Slug:   "secret-draft",
		Title:  "未发布草稿",
		Status: cms.StatusDraft,
	}); err != nil {
		t.Fatal(err)
	}

	// 1. robots.txt（合法 Host + HTTPS）
	rec := doSEOReq(h, "/robots.txt", "buugoo.example.com", "https", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("robots.txt 状态码错误: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("robots.txt Content-Type 错误: %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Allow: /",
		"Allow: /welcome",
		"Allow: /posts",
		"Disallow: /api/",
		"Disallow: /v1/",
		"Disallow: /console",
		"Disallow: /login",
		"Disallow: /memories",
		"Disallow: /channels",
		"Sitemap: https://buugoo.example.com/sitemap.xml",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("robots.txt 缺少 %q:\n%s", want, body)
		}
	}

	// 恶意 Host 头部注入应被拦截，不输出 Sitemap
	badRec := doSEOReq(h, "/robots.txt", "evil.com\r\nX-Injected: 1", "javascript", "")
	if strings.Contains(badRec.Body.String(), "Sitemap:") || strings.Contains(badRec.Body.String(), "evil.com") {
		t.Fatalf("非法 Host 不应出现在 robots.txt:\n%s", badRec.Body.String())
	}

	// 2. sitemap.xml
	rec = doSEOReq(h, "/sitemap.xml", "buugoo.example.com", "https", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sitemap.xml 状态码错误: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/xml") {
		t.Fatalf("sitemap.xml Content-Type 错误: %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != seoCacheHeader {
		t.Fatalf("sitemap.xml Cache-Control 错误: %q", cc)
	}
	var sm sitemapURLSet
	if err := xml.Unmarshal(rec.Body.Bytes(), &sm); err != nil {
		t.Fatalf("sitemap.xml 不是合法 XML: %v\n%s", err, rec.Body.String())
	}
	locs := map[string]bool{}
	for _, u := range sm.URLs {
		locs[u.Loc] = true
	}
	if !locs["https://buugoo.example.com/welcome"] || !locs["https://buugoo.example.com/posts"] || !locs["https://buugoo.example.com/posts/pub-article"] {
		t.Fatalf("sitemap.xml 缺少预期路径: %+v", locs)
	}
	if locs["https://buugoo.example.com/posts/secret-draft"] {
		t.Fatal("sitemap.xml 不应包含草稿文章")
	}

	// 3. rss.xml 与 feed.xml
	for _, feedPath := range []string{"/rss.xml", "/feed.xml"} {
		rec = doSEOReq(h, feedPath, "buugoo.example.com", "https", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 状态码错误: %d", feedPath, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/rss+xml") {
			t.Fatalf("%s Content-Type 错误: %q", feedPath, ct)
		}
		var doc rssDocument
		if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("%s 不是合法 RSS XML: %v\n%s", feedPath, err, rec.Body.String())
		}
		if len(doc.Channel.Items) != 1 {
			t.Fatalf("%s 应仅包含 1 篇已发布文章，实际=%d", feedPath, len(doc.Channel.Items))
		}
		item := doc.Channel.Items[0]
		if item.Title != "公开技术文章" || item.Link != "https://buugoo.example.com/posts/pub-article" || item.Category != "技术" {
			t.Fatalf("%s 条目内容不对: %+v", feedPath, item)
		}
		if _, err := time.Parse(time.RFC1123Z, item.PubDate); err != nil {
			t.Fatalf("%s pubDate 不是 RFC1123Z: %q (%v)", feedPath, item.PubDate, err)
		}
	}
}

func TestSEOSingleModeSitemapRoot(t *testing.T) {
	h, _ := setupSEOTestServer(t, config.ModeSingle)
	rec := doSEOReq(h, "/sitemap.xml", "solo.example.com", "https", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码错误: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<loc>https://solo.example.com/</loc>") {
		t.Fatalf("single 模式 sitemap 首页应为 /: %s", rec.Body.String())
	}
}

func TestSEOHtmlInjectionXSSAnd404(t *testing.T) {
	h, svc := setupSEOTestServer(t, config.ModeMulti)
	ctx := context.Background()
	pubTime := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

	// 标题、摘要和正文包含 </script> 与 HTML 特殊字符，验证防 XSS 逃逸
	xssTitle := `安全测试 </script><script>alert("xss")</script> $100`
	xssSummary := `包含 "引号" & </script> 标签的摘要`
	if _, err := svc.Create(ctx, cms.PostInput{
		Slug:        "xss-safe-post",
		Category:    "安全",
		Tags:        []string{"防护", `</script>`},
		Title:       xssTitle,
		Summary:     xssSummary,
		Content:     "## 标题\n正文含 <img src=x onerror=alert(1)> 与 </script> 内容。",
		Author:      "安全团队",
		CoverURL:    "/covers/hero.png",
		Status:      cms.StatusPublished,
		PublishedAt: &pubTime,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, cms.PostInput{
		Slug:   "hidden-draft",
		Title:  "草稿标题",
		Status: cms.StatusDraft,
	}); err != nil {
		t.Fatal(err)
	}

	// 1. /welcome 首页注入与实时 gzip
	rec := doSEOReq(h, "/welcome", "buugoo.example.com", "https", "gzip")
	if rec.Code != http.StatusOK {
		t.Fatalf("/welcome 状态码错误: %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("HTML 应返回 no-cache，实际=%q", rec.Header().Get("Cache-Control"))
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("支持 gzip 时应实时压缩 HTML，实际=%q", rec.Header().Get("Content-Encoding"))
	}
	gzr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	htmlBytes, err := io.ReadAll(gzr)
	_ = gzr.Close()
	if err != nil {
		t.Fatal(err)
	}
	homeHTML := string(htmlBytes)
	for _, want := range []string{
		`<html lang="zh-CN">`,
		`<link rel="alternate" type="application/rss+xml"`,
		`<title>布谷官网测试标题</title>`,
		`<link rel="canonical" href="https://buugoo.example.com/welcome" />`,
		`property="og:type" content="website"`,
		`property="og:locale" content="zh_CN"`,
		`name="twitter:card" content="summary_large_image"`,
		`"@type":"WebSite"`,
		`"@type":"SoftwareApplication"`,
		`"@type":"Organization"`,
		`<noscript><main><h1>布谷官网测试标题</h1>`,
		`/posts/xss-safe-post`,
	} {
		if !strings.Contains(homeHTML, want) {
			t.Fatalf("/welcome HTML 缺少 %q", want)
		}
	}
	if strings.Count(homeHTML, "<title>") != 1 {
		t.Fatalf("/welcome 不应出现重复 title 标签")
	}
	if strings.Contains(homeHTML, seoHeadPlaceholder) || strings.Contains(homeHTML, seoNoscriptPlaceholder) {
		t.Fatal("占位符应全部被替换")
	}

	// 2. /posts 列表页注入
	rec = doSEOReq(h, "/posts", "buugoo.example.com", "https", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("/posts 状态码错误: %d", rec.Code)
	}
	postsHTML := rec.Body.String()
	for _, want := range []string{
		`<title>文章与动态 - 布谷</title>`,
		`<link rel="canonical" href="https://buugoo.example.com/posts" />`,
		`"@type":"CollectionPage"`,
		`"@type":"BreadcrumbList"`,
		`<noscript><main><h1>文章与动态</h1>`,
		`/posts/xss-safe-post`,
	} {
		if !strings.Contains(postsHTML, want) {
			t.Fatalf("/posts HTML 缺少 %q", want)
		}
	}
	if strings.Contains(postsHTML, "hidden-draft") {
		t.Fatal("/posts noscript 不应泄露草稿文章")
	}

	// 3. /posts/:slug 已发布文章（含 XSS 逃逸防护验证）
	rec = doSEOReq(h, "/posts/xss-safe-post", "buugoo.example.com", "https", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("/posts/xss-safe-post 状态码错误: %d", rec.Code)
	}
	detailHTML := rec.Body.String()
	for _, want := range []string{
		`property="og:type" content="article"`,
		`property="og:image" content="https://buugoo.example.com/covers/hero.png"`,
		`property="article:published_time" content="2026-11-28T09:00:00Z"`,
		`property="article:section" content="安全"`,
		`property="article:tag" content="防护"`,
		`"@type":"BlogPosting"`,
		`"@type":"BreadcrumbList"`,
		`<noscript><main><article>`,
		`$100`,
	} {
		if !strings.Contains(detailHTML, want) {
			t.Fatalf("/posts/xss-safe-post HTML 缺少 %q", want)
		}
	}
	// 严禁出现未转义的攻击标签或闭合 script
	if strings.Contains(detailHTML, `<script>alert("xss")</script>`) || strings.Contains(detailHTML, `<img src=x onerror=alert(1)>`) {
		t.Fatalf("文章 HTML 存在未转义 XSS 载荷:\n%s", detailHTML)
	}
	// JSON-LD script 块中不得出现原始 </script>
	if idx := strings.Index(detailHTML, `data-buugoo-seo="ssr">`); idx >= 0 {
		rest := detailHTML[idx:]
		end := strings.Index(rest, `</script>`)
		if end < 0 || strings.Contains(rest[:end], `alert("xss")`) && strings.Contains(rest[:end], `</`) {
			t.Fatalf("JSON-LD 块存在 </script> 逃逸风险:\n%s", rest[:min(len(rest), 400)])
		}
	}

	// 4. 草稿与不存在的文章返回真正 HTTP 404 + noindex（杜绝 soft-404）
	for _, p := range []string{"/posts/hidden-draft", "/posts/does-not-exist"} {
		rec = doSEOReq(h, p, "buugoo.example.com", "https", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s 应返回 HTTP 404，实际=%d", p, rec.Code)
		}
		b := rec.Body.String()
		if !strings.Contains(b, `<meta name="robots" content="noindex, nofollow" />`) || !strings.Contains(b, `<title>文章不存在 - 布谷</title>`) {
			t.Fatalf("%s 404 页面缺少 noindex 或标题:\n%s", p, b)
		}
	}

	// 5. 私有与管理后台路由必须带 noindex, nofollow
	for _, p := range []string{"/login", "/console", "/settings", "/profile", "/memories", "/memos", "/files", "/tasks", "/bgtasks", "/channels"} {
		rec = doSEOReq(h, p, "buugoo.example.com", "https", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("私有路由 %s 应返回 200 SPA 壳，实际=%d", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `<meta name="robots" content="noindex, nofollow" />`) {
			t.Fatalf("私有路由 %s 缺少 noindex, nofollow", p)
		}
	}
}

func TestSEOHomeSyncsCustomCMSConfig(t *testing.T) {
	h, svc := setupSEOTestServer(t, config.ModeMulti)
	ctx := context.Background()

	// 未自定义首页配置前，保持原有兜底标题与描述
	for _, p := range []string{"/", "/welcome"} {
		rec := doSEOReq(h, p, "buugoo.example.com", "https", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 状态码错误: %d", p, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `<title>布谷官网测试标题</title>`) ||
			!strings.Contains(body, `name="description" content="有记忆、有情绪的长期陪伴体验"`) {
			t.Fatalf("%s 未自定义前应使用原兜底文案:\n%s", p, body)
		}
	}

	// 保存自定义首页配置（含换行与背景图），验证 / 与 /welcome 的 SSR Title、Description、OG、JSON-LD 全部同步且去除换行符
	cfg := cms.DefaultHomeConfig("布谷", "", "")
	cfg.Hero.Title = "星辰伙伴\n记住每一段认真聊过的话"
	cfg.Hero.Subtitle = "自定义官网副标题\n支持多端同步与长期记忆"
	cfg.Hero.BgImageURL = "/covers/custom-hero.png"
	if _, err := svc.SaveHomeConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	wantTitle := "星辰伙伴 记住每一段认真聊过的话 - 布谷"
	wantHeading := "星辰伙伴 记住每一段认真聊过的话"
	wantDesc := "自定义官网副标题 支持多端同步与长期记忆"

	for _, p := range []string{"/", "/welcome"} {
		rec := doSEOReq(h, p, "buugoo.example.com", "https", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("自定义后 %s 状态码错误: %d", p, rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{
			`<title>` + wantTitle + `</title>`,
			`<meta name="description" content="` + wantDesc + `" />`,
			`<meta property="og:title" content="` + wantTitle + `" />`,
			`<meta property="og:description" content="` + wantDesc + `" />`,
			`<meta property="og:image" content="https://buugoo.example.com/covers/custom-hero.png" />`,
			`<meta name="twitter:title" content="` + wantTitle + `" />`,
			`"description":"` + wantDesc + `"`,
			`<noscript><main><h1>` + wantHeading + `</h1>`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s 自定义后缺少 %q", p, want)
			}
		}
	}

	// 重置首页配置后，恢复原有兜底标题与描述
	if _, err := svc.ResetHomeConfig(ctx, "布谷", "布谷官网测试标题", "有记忆、有情绪的长期陪伴体验"); err != nil {
		t.Fatal(err)
	}
	rec := doSEOReq(h, "/welcome", "buugoo.example.com", "https", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("重置后 /welcome 状态码错误: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `<title>布谷官网测试标题</title>`) {
		t.Fatalf("重置后 /welcome 应恢复原有兜底标题")
	}
}
