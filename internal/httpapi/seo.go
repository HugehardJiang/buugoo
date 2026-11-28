package httpapi

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/cms"
	"github.com/HugehardJiang/buugoo/internal/config"
)

const (
	seoHeadPlaceholder     = "<!--buugoo:seo-head-->"
	seoNoscriptPlaceholder = "<!--buugoo:seo-noscript-->"
	seoCacheHeader         = "public, max-age=300"
	seoDescMaxRunes        = 160
	seoRSSLimit            = 50
)

var (
	reHTMLTitle   = regexp.MustCompile(`(?is)<title>.*?</title>`)
	reHTMLDesc    = regexp.MustCompile(`(?i)<meta\s+name=["']description["'][^>]*>\s*`)
	reMDCodeFence = regexp.MustCompile("(?s)```.*?```")
	reMDMathBlock = regexp.MustCompile(`(?s)\$\$.*?\$\$`)
	reMDImage     = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	reMDLink      = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reMDInline    = regexp.MustCompile("[`*_~>#]+")
)

var privateSEORoutes = []string{
	"/login",
	"/console",
	"/settings",
	"/profile",
	"/memories",
	"/memos",
	"/files",
	"/tasks",
	"/bgtasks",
	"/channels",
}

// routeSEO 注册搜索引擎与订阅相关的公开服务端接口。
func (s *server) routeSEO(e *echo.Echo) {
	e.GET("/robots.txt", s.seoRobots)
	e.GET("/sitemap.xml", s.seoSitemap)
	e.GET("/rss.xml", s.seoRSS)
	e.GET("/feed.xml", s.seoRSS)
}

// seoRobots 返回标准的 robots.txt，限制抓取私有与应用内路由，并在 Host 合法时附带 Sitemap。
func (s *server) seoRobots(c echo.Context) error {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	b.WriteString("Allow: /\n")
	b.WriteString("Allow: /welcome\n")
	b.WriteString("Allow: /posts\n")
	b.WriteString("Disallow: /api/\n")
	b.WriteString("Disallow: /v1/\n")
	for _, p := range privateSEORoutes {
		b.WriteString("Disallow: ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	if origin := requestOrigin(c.Request()); origin != "" {
		b.WriteString("\nSitemap: ")
		b.WriteString(origin)
		b.WriteString("/sitemap.xml\n")
	}
	c.Response().Header().Set("Cache-Control", seoCacheHeader)
	return c.String(http.StatusOK, b.String())
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

// seoSitemap 返回标准 XML Sitemap，收录首页、文章列表及全部已发布文章。
func (s *server) seoSitemap(c echo.Context) error {
	origin := requestOrigin(c.Request())
	posts := s.listPublishedPosts(c.Request().Context(), 0)

	var latestMod time.Time
	for i := range posts {
		t := postLastMod(&posts[i])
		if t.After(latestMod) {
			latestMod = t
		}
	}
	latestStr := ""
	if !latestMod.IsZero() {
		latestStr = latestMod.UTC().Format(time.RFC3339)
	}

	urls := make([]sitemapURL, 0, len(posts)+2)
	urls = append(urls,
		sitemapURL{
			Loc:        joinOrigin(origin, s.homePath()),
			LastMod:    latestStr,
			ChangeFreq: "daily",
			Priority:   "1.0",
		},
		sitemapURL{
			Loc:        joinOrigin(origin, "/posts"),
			LastMod:    latestStr,
			ChangeFreq: "daily",
			Priority:   "0.9",
		},
	)
	for i := range posts {
		p := &posts[i]
		mod := postLastMod(p)
		modStr := ""
		if !mod.IsZero() {
			modStr = mod.UTC().Format(time.RFC3339)
		}
		urls = append(urls, sitemapURL{
			Loc:        joinOrigin(origin, "/posts/"+url.PathEscape(p.Slug)),
			LastMod:    modStr,
			ChangeFreq: "weekly",
			Priority:   "0.8",
		})
	}

	doc := sitemapURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	c.Response().Header().Set("Cache-Control", seoCacheHeader)
	return c.Blob(http.StatusOK, "application/xml; charset=utf-8", append([]byte(xml.Header), out...))
}

type rssDocument struct {
	XMLName   xml.Name   `xml:"rss"`
	Version   string     `xml:"version,attr"`
	XmlnsAtom string     `xml:"xmlns:atom,attr"`
	Channel   rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string       `xml:"title"`
	Link          string       `xml:"link"`
	Description   string       `xml:"description"`
	Language      string       `xml:"language"`
	LastBuildDate string       `xml:"lastBuildDate,omitempty"`
	AtomLink      *rssAtomLink `xml:"atom:link,omitempty"`
	Items         []rssItem    `xml:"item"`
}

type rssAtomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	GUID        rssGUID `xml:"guid"`
	PubDate     string  `xml:"pubDate"`
	Description string  `xml:"description"`
	Category    string  `xml:"category,omitempty"`
}

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

// seoRSS 返回标准 RSS 2.0 订阅源，输出最新 50 篇已发布文章。
func (s *server) seoRSS(c echo.Context) error {
	origin := requestOrigin(c.Request())
	posts := s.listPublishedPosts(c.Request().Context(), 0)

	// RSS 按发布时间倒序取最新 50 篇，确保订阅阅读器优先看到最新文章。
	slices.SortStableFunc(posts, func(a, b cms.PostSummary) int {
		ta, tb := postPubTime(&a), postPubTime(&b)
		if ta.After(tb) {
			return -1
		}
		if ta.Before(tb) {
			return 1
		}
		if a.ID > b.ID {
			return -1
		}
		if a.ID < b.ID {
			return 1
		}
		return 0
	})
	if len(posts) > seoRSSLimit {
		posts = posts[:seoRSSLimit]
	}

	siteName := s.seoSiteName()
	items := make([]rssItem, 0, len(posts))
	var lastBuild time.Time
	for i := range posts {
		p := &posts[i]
		pub := postPubTime(p).UTC()
		if pub.After(lastBuild) {
			lastBuild = pub
		}
		link := joinOrigin(origin, "/posts/"+url.PathEscape(p.Slug))
		guid := rssGUID{IsPermaLink: "true", Value: link}
		if origin == "" {
			guid = rssGUID{IsPermaLink: "false", Value: p.Slug}
		}
		desc := strings.TrimSpace(p.Summary)
		if desc == "" {
			desc = p.Title
		}
		items = append(items, rssItem{
			Title:       p.Title,
			Link:        link,
			GUID:        guid,
			PubDate:     pub.Format(time.RFC1123Z),
			Description: desc,
			Category:    p.Category,
		})
	}

	ch := rssChannel{
		Title:       siteName + " - 文章与动态",
		Link:        joinOrigin(origin, "/posts"),
		Description: s.seoHomeDescription(siteName),
		Language:    "zh-CN",
		Items:       items,
	}
	if !lastBuild.IsZero() {
		ch.LastBuildDate = lastBuild.UTC().Format(time.RFC1123Z)
	}
	if origin != "" {
		ch.AtomLink = &rssAtomLink{
			Href: origin + "/rss.xml",
			Rel:  "self",
			Type: "application/rss+xml",
		}
	}

	doc := rssDocument{
		Version:   "2.0",
		XmlnsAtom: "http://www.w3.org/2005/Atom",
		Channel:   ch,
	}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	c.Response().Header().Set("Cache-Control", seoCacheHeader)
	return c.Blob(http.StatusOK, "application/rss+xml; charset=utf-8", append([]byte(xml.Header), out...))
}

// renderSEOHTML 按当前请求路径为 SPA HTML 动态注入 Meta、OG、Twitter Card、JSON-LD 与 noscript 语义内容。
func (s *server) renderSEOHTML(c echo.Context, rawIndex []byte) (int, []byte) {
	cleanPath := path.Clean("/" + c.Request().URL.Path)
	origin := requestOrigin(c.Request())
	siteName := s.seoSiteName()

	switch {
	case cleanPath == "/" || cleanPath == "/welcome":
		return http.StatusOK, s.renderHomeSEO(c.Request().Context(), rawIndex, origin, siteName)
	case cleanPath == "/posts":
		return http.StatusOK, s.renderPostsIndexSEO(c.Request().Context(), rawIndex, origin, siteName)
	case strings.HasPrefix(cleanPath, "/posts/"):
		slug := strings.TrimPrefix(cleanPath, "/posts/")
		if slug != "" && !strings.Contains(slug, "/") {
			return s.renderPostDetailSEO(c.Request().Context(), rawIndex, origin, siteName, slug)
		}
		return http.StatusNotFound, s.renderNotFoundSEO(rawIndex, siteName)
	case isPrivateSEORoute(cleanPath):
		return http.StatusOK, injectSEO(rawIndex, siteName, `<meta name="robots" content="noindex, nofollow" />`, "")
	default:
		return http.StatusOK, injectSEO(rawIndex, siteName, `<meta name="robots" content="noindex, nofollow" />`, "")
	}
}

func (s *server) renderHomeSEO(ctx context.Context, rawIndex []byte, origin, siteName string) []byte {
	heading, desc, customImg := s.seoHomeMeta(ctx, siteName)
	title := heading
	if !strings.Contains(title, siteName) {
		title = heading + " - " + siteName
	}
	canonical := joinOrigin(origin, s.homePath())
	image := resolveSEOImage(origin, customImg)

	var head strings.Builder
	writeMetaTag(&head, "name", "description", desc)
	writeMetaTag(&head, "name", "robots", "index, follow")
	if origin != "" {
		writeLinkTag(&head, "canonical", canonical)
	}
	writeMetaTag(&head, "property", "og:type", "website")
	writeMetaTag(&head, "property", "og:site_name", siteName)
	writeMetaTag(&head, "property", "og:locale", "zh_CN")
	writeMetaTag(&head, "property", "og:title", title)
	writeMetaTag(&head, "property", "og:description", desc)
	if origin != "" {
		writeMetaTag(&head, "property", "og:url", canonical)
	}
	writeMetaTag(&head, "property", "og:image", image)
	writeMetaTag(&head, "name", "twitter:card", "summary_large_image")
	writeMetaTag(&head, "name", "twitter:title", title)
	writeMetaTag(&head, "name", "twitter:description", desc)
	writeMetaTag(&head, "name", "twitter:image", image)

	ld := map[string]any{
		"@context": "https://schema.org",
		"@graph": []any{
			map[string]any{
				"@type":       "WebSite",
				"name":        siteName,
				"url":         canonical,
				"description": desc,
				"inLanguage":  "zh-CN",
			},
			map[string]any{
				"@type":               "SoftwareApplication",
				"name":                siteName,
				"applicationCategory": "CommunicationApplication",
				"operatingSystem":     "Web, Linux, Windows, macOS",
				"description":         desc,
				"url":                 canonical,
			},
			map[string]any{
				"@type": "Organization",
				"name":  siteName,
				"url":   canonical,
				"logo":  image,
			},
		},
	}
	writeJSONLD(&head, ld)

	recent := s.listPublishedPosts(ctx, 5)
	var ns strings.Builder
	ns.WriteString(`<noscript><main><h1>`)
	ns.WriteString(html.EscapeString(heading))
	ns.WriteString(`</h1><p>`)
	ns.WriteString(html.EscapeString(desc))
	ns.WriteString(`</p><section><h2>核心能力</h2><ul>`)
	ns.WriteString(`<li>长期记忆：结合语义检索与时间衰减，记住你的习惯与偏好。</li>`)
	ns.WriteString(`<li>情绪与性格：具备连贯的情绪起伏与可定制人格预设。</li>`)
	ns.WriteString(`<li>提醒与待办：在对话中自然创建定时提醒与待办事项。</li>`)
	ns.WriteString(`<li>备忘录与多端随处可聊：支持网页端及主流聊天软件接入。</li>`)
	ns.WriteString(`</ul></section>`)
	if len(recent) > 0 {
		ns.WriteString(`<section><h2>最新文章</h2><ul>`)
		for i := range recent {
			p := &recent[i]
			ns.WriteString(`<li><a href="/posts/`)
			ns.WriteString(html.EscapeString(url.PathEscape(p.Slug)))
			ns.WriteString(`">`)
			ns.WriteString(html.EscapeString(p.Title))
			ns.WriteString(`</a>`)
			if p.Summary != "" {
				ns.WriteString(`<p>`)
				ns.WriteString(html.EscapeString(p.Summary))
				ns.WriteString(`</p>`)
			}
			ns.WriteString(`</li>`)
		}
		ns.WriteString(`</ul></section>`)
	}
	ns.WriteString(`<nav><a href="/posts">浏览全部文章</a> <a href="/rss.xml">RSS 订阅</a></nav></main></noscript>`)

	return injectSEO(rawIndex, title, head.String(), ns.String())
}

func (s *server) renderPostsIndexSEO(ctx context.Context, rawIndex []byte, origin, siteName string) []byte {
	title := "文章与动态 - " + siteName
	desc := siteName + "的官方文章、技术架构解析、产品动态与使用指南。"
	canonical := joinOrigin(origin, "/posts")
	homeURL := joinOrigin(origin, s.homePath())
	image := seoDefaultImage(origin)

	var head strings.Builder
	writeMetaTag(&head, "name", "description", desc)
	writeMetaTag(&head, "name", "robots", "index, follow")
	if origin != "" {
		writeLinkTag(&head, "canonical", canonical)
	}
	writeMetaTag(&head, "property", "og:type", "website")
	writeMetaTag(&head, "property", "og:site_name", siteName)
	writeMetaTag(&head, "property", "og:locale", "zh_CN")
	writeMetaTag(&head, "property", "og:title", title)
	writeMetaTag(&head, "property", "og:description", desc)
	if origin != "" {
		writeMetaTag(&head, "property", "og:url", canonical)
	}
	writeMetaTag(&head, "property", "og:image", image)
	writeMetaTag(&head, "name", "twitter:card", "summary_large_image")
	writeMetaTag(&head, "name", "twitter:title", title)
	writeMetaTag(&head, "name", "twitter:description", desc)
	writeMetaTag(&head, "name", "twitter:image", image)

	ld := map[string]any{
		"@context": "https://schema.org",
		"@graph": []any{
			map[string]any{
				"@type":       "CollectionPage",
				"name":        title,
				"description": desc,
				"url":         canonical,
				"inLanguage":  "zh-CN",
			},
			map[string]any{
				"@type": "BreadcrumbList",
				"itemListElement": []any{
					map[string]any{"@type": "ListItem", "position": 1, "name": siteName, "item": homeURL},
					map[string]any{"@type": "ListItem", "position": 2, "name": "文章与动态", "item": canonical},
				},
			},
		},
	}
	writeJSONLD(&head, ld)

	posts := s.listPublishedPosts(ctx, 30)
	var ns strings.Builder
	ns.WriteString(`<noscript><main><h1>文章与动态</h1><p>`)
	ns.WriteString(html.EscapeString(desc))
	ns.WriteString(`</p>`)
	if len(posts) > 0 {
		ns.WriteString(`<ul>`)
		for i := range posts {
			p := &posts[i]
			ns.WriteString(`<li><article><h2><a href="/posts/`)
			ns.WriteString(html.EscapeString(url.PathEscape(p.Slug)))
			ns.WriteString(`">`)
			ns.WriteString(html.EscapeString(p.Title))
			ns.WriteString(`</a></h2>`)
			if p.Summary != "" {
				ns.WriteString(`<p>`)
				ns.WriteString(html.EscapeString(p.Summary))
				ns.WriteString(`</p>`)
			}
			ns.WriteString(`</article></li>`)
		}
		ns.WriteString(`</ul>`)
	}
	ns.WriteString(`<nav><a href="`)
	ns.WriteString(html.EscapeString(s.homePath()))
	ns.WriteString(`">返回首页</a> <a href="/rss.xml">RSS 订阅</a></nav></main></noscript>`)

	return injectSEO(rawIndex, title, head.String(), ns.String())
}

func (s *server) renderPostDetailSEO(ctx context.Context, rawIndex []byte, origin, siteName, slug string) (int, []byte) {
	if s.CMS == nil {
		return http.StatusNotFound, s.renderNotFoundSEO(rawIndex, siteName)
	}
	res, err := s.CMS.GetPublicBySlug(ctx, slug)
	if err != nil {
		if !errors.Is(err, cms.ErrNotFound) {
			s.log.Warn("SEO 查询文章详情失败", "slug", slug, "err", err)
		}
		return http.StatusNotFound, s.renderNotFoundSEO(rawIndex, siteName)
	}

	p := &res.Post
	title := p.Title + " - " + siteName
	desc := strings.TrimSpace(p.Summary)
	if desc == "" {
		desc = summarizeMarkdown(p.Content, seoDescMaxRunes)
	}
	if desc == "" {
		desc = p.Title
	}
	canonical := joinOrigin(origin, "/posts/"+url.PathEscape(p.Slug))
	homeURL := joinOrigin(origin, s.homePath())
	postsURL := joinOrigin(origin, "/posts")
	image := resolveSEOImage(origin, p.CoverURL)
	author := strings.TrimSpace(p.Author)
	if author == "" {
		author = siteName
	}

	var head strings.Builder
	writeMetaTag(&head, "name", "description", desc)
	writeMetaTag(&head, "name", "robots", "index, follow")
	if origin != "" {
		writeLinkTag(&head, "canonical", canonical)
	}
	writeMetaTag(&head, "property", "og:type", "article")
	writeMetaTag(&head, "property", "og:site_name", siteName)
	writeMetaTag(&head, "property", "og:locale", "zh_CN")
	writeMetaTag(&head, "property", "og:title", p.Title)
	writeMetaTag(&head, "property", "og:description", desc)
	if origin != "" {
		writeMetaTag(&head, "property", "og:url", canonical)
	}
	writeMetaTag(&head, "property", "og:image", image)
	if p.PublishedAt != nil {
		writeMetaTag(&head, "property", "article:published_time", p.PublishedAt.UTC().Format(time.RFC3339))
	}
	if !p.UpdatedAt.IsZero() {
		writeMetaTag(&head, "property", "article:modified_time", p.UpdatedAt.UTC().Format(time.RFC3339))
	}
	if p.Category != "" {
		writeMetaTag(&head, "property", "article:section", p.Category)
	}
	for _, tag := range p.Tags {
		writeMetaTag(&head, "property", "article:tag", tag)
	}
	writeMetaTag(&head, "name", "twitter:card", "summary_large_image")
	writeMetaTag(&head, "name", "twitter:title", p.Title)
	writeMetaTag(&head, "name", "twitter:description", desc)
	writeMetaTag(&head, "name", "twitter:image", image)

	posting := map[string]any{
		"@type":            "BlogPosting",
		"headline":         p.Title,
		"description":      desc,
		"url":              canonical,
		"mainEntityOfPage": canonical,
		"image":            image,
		"inLanguage":       "zh-CN",
		"author": map[string]any{
			"@type": "Person",
			"name":  author,
		},
		"publisher": map[string]any{
			"@type": "Organization",
			"name":  siteName,
			"logo": map[string]any{
				"@type": "ImageObject",
				"url":   seoDefaultImage(origin),
			},
		},
	}
	if p.PublishedAt != nil {
		posting["datePublished"] = p.PublishedAt.UTC().Format(time.RFC3339)
	}
	if !p.UpdatedAt.IsZero() {
		posting["dateModified"] = p.UpdatedAt.UTC().Format(time.RFC3339)
	}
	if p.Category != "" {
		posting["articleSection"] = p.Category
	}
	if len(p.Tags) > 0 {
		posting["keywords"] = strings.Join(p.Tags, ", ")
	}

	ld := map[string]any{
		"@context": "https://schema.org",
		"@graph": []any{
			posting,
			map[string]any{
				"@type": "BreadcrumbList",
				"itemListElement": []any{
					map[string]any{"@type": "ListItem", "position": 1, "name": siteName, "item": homeURL},
					map[string]any{"@type": "ListItem", "position": 2, "name": "文章与动态", "item": postsURL},
					map[string]any{"@type": "ListItem", "position": 3, "name": p.Title, "item": canonical},
				},
			},
		},
	}
	writeJSONLD(&head, ld)

	var ns strings.Builder
	ns.WriteString(`<noscript><main><article><header><h1>`)
	ns.WriteString(html.EscapeString(p.Title))
	ns.WriteString(`</h1><p>`)
	ns.WriteString(html.EscapeString(author))
	if p.Category != "" {
		ns.WriteString(` · `)
		ns.WriteString(html.EscapeString(p.Category))
	}
	if p.PublishedAt != nil {
		iso := p.PublishedAt.UTC().Format(time.RFC3339)
		ns.WriteString(` · <time datetime="`)
		ns.WriteString(html.EscapeString(iso))
		ns.WriteString(`">`)
		ns.WriteString(html.EscapeString(p.PublishedAt.UTC().Format("2006-01-02")))
		ns.WriteString(`</time>`)
	}
	ns.WriteString(`</p>`)
	if p.Summary != "" {
		ns.WriteString(`<p>`)
		ns.WriteString(html.EscapeString(p.Summary))
		ns.WriteString(`</p>`)
	}
	ns.WriteString(`</header><div>`)
	if p.Content != "" {
		ns.WriteString(`<pre style="white-space:pre-wrap;font:inherit">`)
		ns.WriteString(html.EscapeString(p.Content))
		ns.WriteString(`</pre>`)
	}
	ns.WriteString(`</div>`)
	if len(res.Related) > 0 {
		ns.WriteString(`<section><h2>相关文章</h2><ul>`)
		for i := range res.Related {
			r := &res.Related[i]
			ns.WriteString(`<li><a href="/posts/`)
			ns.WriteString(html.EscapeString(url.PathEscape(r.Slug)))
			ns.WriteString(`">`)
			ns.WriteString(html.EscapeString(r.Title))
			ns.WriteString(`</a></li>`)
		}
		ns.WriteString(`</ul></section>`)
	}
	ns.WriteString(`<nav><a href="/posts">返回文章列表</a></nav></article></main></noscript>`)

	return http.StatusOK, injectSEO(rawIndex, title, head.String(), ns.String())
}

func (s *server) renderNotFoundSEO(rawIndex []byte, siteName string) []byte {
	title := "文章不存在 - " + siteName
	var head strings.Builder
	writeMetaTag(&head, "name", "description", "没有找到对应的公开文章，请返回文章列表浏览其他内容。")
	writeMetaTag(&head, "name", "robots", "noindex, nofollow")
	ns := `<noscript><main><h1>文章不存在</h1><p>没有找到对应的公开文章，请返回<a href="/posts">文章列表</a>浏览其他内容。</p></main></noscript>`
	return injectSEO(rawIndex, title, head.String(), ns)
}

func (s *server) listPublishedPosts(ctx context.Context, limit int) []cms.PostSummary {
	if s.CMS == nil {
		return nil
	}
	res, err := s.CMS.ListPublic(ctx, cms.PublicListFilter{Limit: limit})
	if err != nil {
		s.log.Warn("SEO 读取文章列表失败", "err", err)
		return nil
	}
	return res.Posts
}

func (s *server) homePath() string {
	if s.Config != nil && s.Config.Mode == config.ModeMulti {
		return "/welcome"
	}
	return "/"
}

func (s *server) seoSiteName() string {
	if s.Persona != nil {
		if name := strings.TrimSpace(s.Persona.Current().Name); name != "" {
			return name
		}
	}
	return "布谷"
}

func (s *server) seoHomeHeading(siteName string) string {
	if s.Config != nil {
		if t := strings.TrimSpace(s.Config.Home.Title); t != "" {
			return t
		}
	}
	return siteName + " - 有记忆、有情绪的 AI 朋友"
}

func (s *server) seoHomeDescription(siteName string) string {
	if s.Config != nil {
		if t := strings.TrimSpace(s.Config.Home.Tagline); t != "" {
			return t
		}
	}
	return siteName + "是有记忆、有情绪、能主动关心你的 AI 朋友，支持自托管、长期记忆与多端随处可聊。"
}

// seoHomeMeta 优先使用管理员在首页 CMS 中自定义的标题、副标题与背景图，未自定义时沿用原有默认逻辑。
func (s *server) seoHomeMeta(ctx context.Context, siteName string) (heading, desc, image string) {
	heading = s.seoHomeHeading(siteName)
	desc = s.seoHomeDescription(siteName)
	if s.CMS == nil {
		return heading, desc, ""
	}
	var fallbackTitle, fallbackTagline string
	if s.Config != nil {
		fallbackTitle = s.Config.Home.Title
		fallbackTagline = s.Config.Home.Tagline
	}
	cfg, isDefault, err := s.CMS.GetHomeConfig(ctx, siteName, fallbackTitle, fallbackTagline)
	if err != nil || isDefault {
		return heading, desc, ""
	}
	if customTitle := flattenSingleLine(cfg.Hero.Title); customTitle != "" {
		heading = customTitle
	}
	if customSubtitle := flattenSingleLine(cfg.Hero.Subtitle); customSubtitle != "" {
		desc = customSubtitle
	}
	return heading, desc, strings.TrimSpace(cfg.Hero.BgImageURL)
}

func flattenSingleLine(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}

// injectSEO 将动态标题、头部标签与 noscript 内容注入 SPA HTML，避免重复 title/description 并防止 $ 替换误解析。
func injectSEO(rawIndex []byte, title, headExtra, noscriptHTML string) []byte {
	doc := string(rawIndex)

	if title != "" {
		titleTag := "<title>" + html.EscapeString(title) + "</title>"
		if loc := reHTMLTitle.FindStringIndex(doc); loc != nil {
			doc = doc[:loc[0]] + titleTag + doc[loc[1]:]
		} else {
			headExtra = titleTag + "\n" + headExtra
		}
	}

	if strings.Contains(headExtra, `name="description"`) {
		if loc := reHTMLDesc.FindStringIndex(doc); loc != nil {
			doc = doc[:loc[0]] + doc[loc[1]:]
		}
	}

	if strings.Contains(doc, seoHeadPlaceholder) {
		doc = strings.Replace(doc, seoHeadPlaceholder, headExtra, 1)
	} else if idx := strings.Index(strings.ToLower(doc), "</head>"); idx >= 0 {
		doc = doc[:idx] + headExtra + "\n" + doc[idx:]
	}

	if strings.Contains(doc, seoNoscriptPlaceholder) {
		doc = strings.Replace(doc, seoNoscriptPlaceholder, noscriptHTML, 1)
	} else if noscriptHTML != "" {
		if idx := strings.Index(strings.ToLower(doc), "</body>"); idx >= 0 {
			doc = doc[:idx] + noscriptHTML + "\n" + doc[idx:]
		}
	}

	return []byte(doc)
}

func writeMetaTag(b *strings.Builder, attrName, attrVal, content string) {
	b.WriteString(`<meta `)
	b.WriteString(attrName)
	b.WriteString(`="`)
	b.WriteString(html.EscapeString(attrVal))
	b.WriteString(`" content="`)
	b.WriteString(html.EscapeString(content))
	b.WriteString(`" />`)
	b.WriteString("\n")
}

func writeLinkTag(b *strings.Builder, rel, href string) {
	b.WriteString(`<link rel="`)
	b.WriteString(html.EscapeString(rel))
	b.WriteString(`" href="`)
	b.WriteString(html.EscapeString(href))
	b.WriteString(`" />`)
	b.WriteString("\n")
}

func writeJSONLD(b *strings.Builder, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	// Go 默认转义 < 与 > 为 \u003c / \u003e；额外防御性替换 </ 为 <\/，杜绝 </script> 逃逸。
	safe := strings.ReplaceAll(string(raw), "</", `<\/`)
	safe = strings.ReplaceAll(safe, "<!--", `<\!--`)
	b.WriteString(`<script type="application/ld+json" data-buugoo-seo="ssr">`)
	b.WriteString(safe)
	b.WriteString(`</script>`)
	b.WriteString("\n")
}

// requestOrigin 严格校验 Host 与 X-Forwarded-Proto，防止响应头与链接注入。不合法时返回空串。
func requestOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
		first, _, _ := strings.Cut(xfp, ",")
		switch strings.ToLower(strings.TrimSpace(first)) {
		case "http":
			scheme = "http"
		case "https":
			scheme = "https"
		}
	}

	host := sanitizeHost(r.Host)
	if host == "" {
		return ""
	}
	if scheme == "http" && strings.HasSuffix(host, ":80") {
		host = strings.TrimSuffix(host, ":80")
	} else if scheme == "https" && strings.HasSuffix(host, ":443") {
		host = strings.TrimSuffix(host, ":443")
	}
	return scheme + "://" + host
}

func sanitizeHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 260 {
		return ""
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f {
			return ""
		}
		switch r {
		case '/', '\\', '?', '#', '@', '<', '>', '"', '\'', ';', ',', '`':
			return ""
		}
	}

	host := raw
	port := ""
	if strings.HasPrefix(raw, "[") {
		if strings.Contains(raw, "]:") {
			h, p, err := net.SplitHostPort(raw)
			if err != nil {
				return ""
			}
			host, port = h, p
		} else if strings.HasSuffix(raw, "]") {
			host = raw[1 : len(raw)-1]
		} else {
			return ""
		}
		if net.ParseIP(host) == nil {
			return ""
		}
		if port != "" {
			if !validPort(port) {
				return ""
			}
			return "[" + host + "]:" + port
		}
		return "[" + host + "]"
	}

	if strings.Contains(raw, ":") {
		h, p, err := net.SplitHostPort(raw)
		if err != nil {
			return ""
		}
		host, port = h, p
		if !validPort(port) {
			return ""
		}
	}
	if !validHostnameOrIP(host) {
		return ""
	}
	if port != "" {
		return host + ":" + port
	}
	return host
}

func validPort(p string) bool {
	if p == "" || len(p) > 5 {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < '0' || p[i] > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}

func validHostnameOrIP(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return false
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func joinOrigin(origin, p string) string {
	if origin == "" {
		return p
	}
	return origin + p
}

func seoDefaultImage(origin string) string {
	return joinOrigin(origin, "/icon-512.png")
}

func resolveSEOImage(origin, coverURL string) string {
	u := strings.TrimSpace(coverURL)
	if u == "" {
		return seoDefaultImage(origin)
	}
	if strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//") && !strings.Contains(u, "\\") {
		return joinOrigin(origin, u)
	}
	parsed, err := url.Parse(u)
	if err == nil && parsed.Host != "" {
		scheme := strings.ToLower(parsed.Scheme)
		if scheme == "http" || scheme == "https" {
			return u
		}
	}
	return seoDefaultImage(origin)
}

func isPrivateSEORoute(cleanPath string) bool {
	for _, prefix := range privateSEORoutes {
		if cleanPath == prefix || strings.HasPrefix(cleanPath, prefix+"/") {
			return true
		}
	}
	return false
}

func postLastMod(p *cms.PostSummary) time.Time {
	t := p.UpdatedAt
	if p.PublishedAt != nil && p.PublishedAt.After(t) {
		t = *p.PublishedAt
	}
	return t
}

func postPubTime(p *cms.PostSummary) time.Time {
	if p.PublishedAt != nil {
		return *p.PublishedAt
	}
	if !p.CreatedAt.IsZero() {
		return p.CreatedAt
	}
	return p.UpdatedAt
}

func summarizeMarkdown(md string, maxRunes int) string {
	s := reMDCodeFence.ReplaceAllString(md, " ")
	s = reMDMathBlock.ReplaceAllString(s, " ")
	s = reMDImage.ReplaceAllString(s, "$1")
	s = reMDLink.ReplaceAllString(s, "$1")
	s = reMDInline.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
}
