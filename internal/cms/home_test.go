package cms_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/cms"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/httpapi"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func TestHomeMigrationV28Idempotent(t *testing.T) {
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
			if st.ID == "202611280028_cms_site_settings" {
				found = st.Applied
			}
		}
		if !found {
			t.Fatal("未找到已应用的 202611280028_cms_site_settings 迁移记录")
		}
		if !db.Gorm().Migrator().HasTable("cms_site_settings") {
			t.Fatal("迁移后缺少 cms_site_settings 表")
		}
	})
}

func TestDefaultHomeConfigContentRules(t *testing.T) {
	cfg := cms.DefaultHomeConfig("布谷", "", "")

	if cfg.Theme.CanvasColor != "#faf9f5" ||
		cfg.Theme.AccentColor != "#5b7a4a" ||
		cfg.Theme.HeroGlowPrimary != "#8fa67f" ||
		cfg.Theme.HeroGlowSecondary != "#bcd1ca" ||
		cfg.Theme.FontStyle != "sans" ||
		cfg.Theme.HeroHeight != "full" ||
		!cfg.Theme.HeaderBlur ||
		cfg.Theme.CardRadius != 24 {
		t.Fatalf("默认主题配置不匹配布谷原色: %+v", cfg.Theme)
	}
	if cfg.Hero.Title != "布谷，一个真的\n记得你的 AI 朋友" {
		t.Fatalf("默认主标题不符预期: %q", cfg.Hero.Title)
	}
	if len(cfg.Sections) != 7 {
		t.Fatalf("默认区块数量应为 7，实际=%d", len(cfg.Sections))
	}
	if len(cfg.Header.NavLinks) != 4 {
		t.Fatalf("默认顶栏导航应为 4 项，实际=%d", len(cfg.Header.NavLinks))
	}
	if len(cfg.Showcase.Cards) != 4 {
		t.Fatalf("默认焦点竖卡应为 4 张，实际=%d", len(cfg.Showcase.Cards))
	}
	if len(cfg.Capabilities.Items) != 5 {
		t.Fatalf("默认能力项应为 5 项，实际=%d", len(cfg.Capabilities.Items))
	}
	if len(cfg.Spotlight.Items) != 4 {
		t.Fatalf("默认手风琴应为 4 项，实际=%d", len(cfg.Spotlight.Items))
	}
	if len(cfg.Footer.Columns) != 3 {
		t.Fatalf("默认页脚链接栏应为 3 栏，实际=%d", len(cfg.Footer.Columns))
	}

	// 验证 fallbackTitle 优先
	withFallback := cms.DefaultHomeConfig("小谷", "自定义初始标题", "自定义初始副标题")
	if withFallback.Hero.Title != "自定义初始标题" || withFallback.Hero.Subtitle != "自定义初始副标题" {
		t.Fatalf("fallbackTitle/fallbackTagline 未生效: %+v", withFallback.Hero)
	}

	// 验证全部默认文案不含 emoji、感叹号、他/她
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"他", "她", "!", "！"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("默认首页配置包含禁用字符 %q", forbidden)
		}
	}
	for _, r := range text {
		if r >= 0x1F300 && r <= 0x1FAFF {
			t.Fatalf("默认首页配置不应包含 emoji: %U", r)
		}
	}
	if !strings.Contains(text, "https://arena.buugoo.com") || !strings.Contains(text, "https://arena.buugoo.com/arena") {
		t.Fatal("默认首页配置缺少 BuugooArena 链接")
	}
}

func TestHomeConfigSaveGetResetAndPartialFallback(t *testing.T) {
	forEachDB(t, func(t *testing.T, db *gormstore.DB) {
		ctx := context.Background()
		db.Gorm().Exec("DELETE FROM cms_site_settings")
		svc := cms.New(db.Gorm())

		// 1. 初始读取返回默认配置且 isDefault = true
		cfg, isDef, err := svc.GetHomeConfig(ctx, "布谷", "默认标题", "默认副标题")
		if err != nil {
			t.Fatal(err)
		}
		if !isDef || cfg.Hero.Title != "默认标题" {
			t.Fatalf("初始读取异常: isDef=%v title=%q", isDef, cfg.Hero.Title)
		}

		// 2. 自定义修改并保存（故意只传部分 sections 并带重复项，验证去重与补全）
		cfg.Theme.CanvasColor = "#FAF8F2"
		cfg.Theme.CardRadius = 28
		cfg.Theme.FontStyle = "serif"
		cfg.Hero.Title = "新的主标题\n第二行文案"
		cfg.Hero.BgOverlay = 35
		cfg.News.Limit = 6
		cfg.Sections = []cms.HomeSection{
			{ID: "news", Label: "动态", Visible: true},
			{ID: "hero", Label: "主视觉", Visible: true},
			{ID: "news", Label: "重复项应忽略", Visible: false},
			{ID: "showcase", Label: "展示卡", Visible: false},
		}
		cfg.CustomBlocks = []cms.HomeCustomBlock{
			{
				Eyebrow:  "社区共建",
				Title:    "开放自托管体验",
				Subtitle: "单二进制部署",
				BodyMD:   "支持 **SQLite** 与 **MySQL 8.4**。",
				Theme:    "card",
				CTALabel: "查看文档",
				CTAURL:   "/posts",
			},
		}

		saved, err := svc.SaveHomeConfig(ctx, cfg)
		if err != nil {
			t.Fatalf("首次保存失败: %v", err)
		}
		if saved.Theme.CanvasColor != "#faf8f2" {
			t.Fatalf("颜色应统一归一化为小写: got=%q", saved.Theme.CanvasColor)
		}
		if len(saved.Sections) != 7 {
			t.Fatalf("保存后区块应去重并补齐为 7 个，实际=%d", len(saved.Sections))
		}
		if saved.Sections[0].ID != "news" || saved.Sections[1].ID != "hero" || saved.Sections[2].ID != "showcase" || saved.Sections[2].Visible {
			t.Fatalf("区块顺序与可见性未正确保留: %+v", saved.Sections)
		}
		if len(saved.CustomBlocks) != 1 || saved.CustomBlocks[0].ID == "" {
			t.Fatalf("自定义区块应自动生成 ID: %+v", saved.CustomBlocks)
		}

		// 3. 再次读取返回自定义配置且 isDefault = false
		loaded, isDef, err := svc.GetHomeConfig(ctx, "布谷", "默认标题", "默认副标题")
		if err != nil {
			t.Fatal(err)
		}
		if isDef || loaded.Hero.Title != "新的主标题\n第二行文案" || loaded.Theme.CardRadius != 28 || len(loaded.CustomBlocks) != 1 {
			t.Fatalf("读取自定义配置异常: isDef=%v loaded=%+v", isDef, loaded.Hero)
		}

		// 4. 覆盖保存（测试 OnConflict 更新路径）
		loaded.Hero.Title = "二次更新主标题"
		if _, err := svc.SaveHomeConfig(ctx, loaded); err != nil {
			t.Fatalf("二次覆盖保存失败: %v", err)
		}
		loaded2, isDef, err := svc.GetHomeConfig(ctx, "布谷", "", "")
		if err != nil || isDef || loaded2.Hero.Title != "二次更新主标题" {
			t.Fatalf("二次更新读取异常: isDef=%v title=%q err=%v", isDef, loaded2.Hero.Title, err)
		}

		// 5. 老数据 / 缺字段 / 含非法值的数据读取永不报错且自动兜底
		if err := db.Gorm().Exec(
			"UPDATE cms_site_settings SET value_json = ? WHERE `key` = ?",
			`{"theme":{"canvas_color":"not-a-color","card_radius":999},"hero":{"title":"老版本标题","primary_url":"javascript:alert(1)"}}`,
			"home",
		).Error; err != nil {
			t.Fatal(err)
		}
		legacy, isDef, err := svc.GetHomeConfig(ctx, "布谷", "", "")
		if err != nil {
			t.Fatalf("读取部分缺字段老数据不应报错: %v", err)
		}
		if isDef {
			t.Fatal("存在合法 JSON 记录时 isDefault 应为 false")
		}
		if legacy.Hero.Title != "老版本标题" {
			t.Fatalf("老数据已有字段应保留: %q", legacy.Hero.Title)
		}
		if legacy.Theme.CanvasColor != "#faf9f5" || legacy.Theme.CardRadius != 24 || legacy.Hero.PrimaryURL != "" || len(legacy.Sections) != 7 {
			t.Fatalf("老数据缺失或非法字段未兜底: theme=%+v hero=%+v sections=%d", legacy.Theme, legacy.Hero, len(legacy.Sections))
		}

		// 损坏 JSON 也不报错，回退默认
		if err := db.Gorm().Exec("UPDATE cms_site_settings SET value_json = ? WHERE `key` = ?", "{broken-json", "home").Error; err != nil {
			t.Fatal(err)
		}
		corrupted, isDef, err := svc.GetHomeConfig(ctx, "布谷", "兜底标题", "")
		if err != nil || !isDef || corrupted.Hero.Title != "兜底标题" {
			t.Fatalf("损坏 JSON 应静默回退默认配置: isDef=%v title=%q err=%v", isDef, corrupted.Hero.Title, err)
		}

		// 6. 重置恢复默认并验证幂等
		resetCfg, err := svc.ResetHomeConfig(ctx, "布谷", "恢复标题", "恢复副标题")
		if err != nil {
			t.Fatalf("重置失败: %v", err)
		}
		if resetCfg.Hero.Title != "恢复标题" {
			t.Fatalf("重置返回配置异常: %q", resetCfg.Hero.Title)
		}
		afterReset, isDef, err := svc.GetHomeConfig(ctx, "布谷", "恢复标题", "恢复副标题")
		if err != nil || !isDef || afterReset.Hero.Title != "恢复标题" {
			t.Fatalf("重置后读取应回到默认: isDef=%v title=%q err=%v", isDef, afterReset.Hero.Title, err)
		}
		if _, err := svc.ResetHomeConfig(ctx, "布谷", "恢复标题", "恢复副标题"); err != nil {
			t.Fatalf("重复重置应幂等: %v", err)
		}
	})
}

func TestHomeConfigValidationErrors(t *testing.T) {
	forEachDB(t, func(t *testing.T, db *gormstore.DB) {
		ctx := context.Background()
		svc := cms.New(db.Gorm())

		assertUserErr := func(name string, mutate func(c *cms.HomeConfig)) {
			t.Helper()
			cfg := cms.DefaultHomeConfig("布谷", "", "")
			mutate(&cfg)
			_, err := svc.SaveHomeConfig(ctx, cfg)
			var ue *cms.UserError
			if !errors.As(err, &ue) {
				t.Fatalf("用例 %s 应返回 UserError，实际=%v", name, err)
			}
		}

		// 非法颜色
		for _, badColor := range []string{"", "red", "#12", "#12345", "#1234567", "#gggggg", "rgb(0,0,0)", "faf9f5"} {
			c := badColor
			assertUserErr("canvas_color="+c, func(cfg *cms.HomeConfig) { cfg.Theme.CanvasColor = c })
			assertUserErr("accent_color="+c, func(cfg *cms.HomeConfig) { cfg.Theme.AccentColor = c })
			assertUserErr("hero_glow_primary="+c, func(cfg *cms.HomeConfig) { cfg.Theme.HeroGlowPrimary = c })
			assertUserErr("hero_glow_secondary="+c, func(cfg *cms.HomeConfig) { cfg.Theme.HeroGlowSecondary = c })
		}

		// 合法 3 位简写十六进制色值允许保存
		shortHex := cms.DefaultHomeConfig("布谷", "", "")
		shortHex.Theme.CanvasColor = "#fff"
		shortHex.Theme.AccentColor = "#5B7"
		if saved, err := svc.SaveHomeConfig(ctx, shortHex); err != nil || saved.Theme.AccentColor != "#5b7" {
			t.Fatalf("合法 3 位 hex 色值应允许保存: saved=%+v err=%v", saved.Theme, err)
		}

		// XSS 与非法 URL
		badURLs := []string{
			"javascript:alert(1)",
			"JavaScript:void(0)",
			"data:text/html,<script>alert(1)</script>",
			"vbscript:msgbox(1)",
			"//evil.example.com/a",
			"/\\evil.example.com/a",
			"ftp://example.com/file",
			"relative/no-slash",
			"https://example.com/has space",
		}
		for _, u := range badURLs {
			bad := u
			assertUserErr("nav_link="+bad, func(cfg *cms.HomeConfig) { cfg.Header.NavLinks = []cms.HomeLink{{Label: "坏链接", URL: bad}} })
			assertUserErr("hero_primary="+bad, func(cfg *cms.HomeConfig) { cfg.Hero.PrimaryURL = bad })
			assertUserErr("hero_bg="+bad, func(cfg *cms.HomeConfig) { cfg.Hero.BgImageURL = bad })
			assertUserErr("showcase_btn="+bad, func(cfg *cms.HomeConfig) { cfg.Showcase.Cards[0].PrimaryURL = bad })
			assertUserErr("cap_link="+bad, func(cfg *cms.HomeConfig) { cfg.Capabilities.Items[0].LearnMoreURL = bad })
			assertUserErr("spotlight_cta="+bad, func(cfg *cms.HomeConfig) { cfg.Spotlight.Items[0].StageCTAURL = bad })
			assertUserErr("footer_link="+bad, func(cfg *cms.HomeConfig) { cfg.Footer.Columns[0].Links[0].URL = bad })
		}
		// 图片链接不允许 #锚点
		assertUserErr("image_anchor", func(cfg *cms.HomeConfig) { cfg.Hero.BgImageURL = "#section" })

		// 枚举与数值范围校验
		assertUserErr("bad_hero_height", func(cfg *cms.HomeConfig) { cfg.Theme.HeroHeight = "giant" })
		assertUserErr("bad_font_style", func(cfg *cms.HomeConfig) { cfg.Theme.FontStyle = "mono" })
		assertUserErr("card_radius_low", func(cfg *cms.HomeConfig) { cfg.Theme.CardRadius = 15 })
		assertUserErr("card_radius_high", func(cfg *cms.HomeConfig) { cfg.Theme.CardRadius = 33 })
		assertUserErr("bg_overlay_low", func(cfg *cms.HomeConfig) { cfg.Hero.BgOverlay = -1 })
		assertUserErr("bg_overlay_high", func(cfg *cms.HomeConfig) { cfg.Hero.BgOverlay = 81 })
		assertUserErr("news_limit_low", func(cfg *cms.HomeConfig) { cfg.News.Limit = 2 })
		assertUserErr("news_limit_high", func(cfg *cms.HomeConfig) { cfg.News.Limit = 9 })
		assertUserErr("bad_section_id", func(cfg *cms.HomeConfig) { cfg.Sections = []cms.HomeSection{{ID: "unknown", Visible: true}} })
		assertUserErr("bad_showcase_theme", func(cfg *cms.HomeConfig) { cfg.Showcase.Cards[0].Theme = "neon" })
		assertUserErr("bad_cap_icon", func(cfg *cms.HomeConfig) { cfg.Capabilities.Items[0].Icon = "rocket" })
		assertUserErr("bad_stage_type", func(cfg *cms.HomeConfig) { cfg.Spotlight.Items[0].StageType = "video" })
		assertUserErr("bad_custom_theme", func(cfg *cms.HomeConfig) {
			cfg.CustomBlocks = []cms.HomeCustomBlock{{Title: "块", Theme: "neon"}}
		})

		// 数组数量上限校验
		assertUserErr("too_many_nav_links", func(cfg *cms.HomeConfig) {
			cfg.Header.NavLinks = make([]cms.HomeLink, cms.MaxHomeNavLinks+1)
		})
	})
}

func TestHomeConfigHTTPAPIAndDefaultPasswordGuard(t *testing.T) {
	db, err := gormstore.Open(config.DatabaseConfig{
		Driver: config.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "home-http.db"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := cms.New(db.Gorm())

	// 1. 默认密码 1228 必须返回 403 拦截后台首页配置接口
	weakCfg := config.Default()
	weakCfg.Admin.Password = config.DefaultAdminPassword
	weakHandler := httpapi.New(httpapi.Deps{Config: weakCfg, CMS: svc})
	weakTok := loginConsole(t, weakHandler, config.DefaultAdminPassword)

	defBody, err := json.Marshal(cms.DefaultHomeConfig("布谷", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/console/cms/home", ""},
		{http.MethodPut, "/api/v1/console/cms/home", string(defBody)},
		{http.MethodDelete, "/api/v1/console/cms/home", ""},
	} {
		rec := doReq(weakHandler, tc.method, tc.path, tc.body, weakTok)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("默认密码请求 %s %s 应返回 403，实际=%d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// 公开只读接口仍可正常访问
	rec := doReq(weakHandler, http.MethodGet, "/api/v1/cms/home", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("公开首页配置接口应返回 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}

	// 2. 强密码下的完整 CRUD 与重置流程
	strongCfg := config.Default()
	strongCfg.Admin.Password = "strong-home-pass"
	strongCfg.Home.Title = "配置兜底标题"
	h := httpapi.New(httpapi.Deps{Config: strongCfg, CMS: svc})

	if rec := doReq(h, http.MethodGet, "/api/v1/console/cms/home", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录访问管理接口应返回 401，实际=%d", rec.Code)
	}
	tok := loginConsole(t, h, "strong-home-pass")

	rec = doReq(h, http.MethodGet, "/api/v1/console/cms/home", "", tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("管理读取应返回 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var getResp struct {
		Config    cms.HomeConfig `json:"config"`
		IsDefault bool           `json:"is_default"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp); err != nil {
		t.Fatal(err)
	}
	if !getResp.IsDefault || getResp.Config.Hero.Title != "配置兜底标题" {
		t.Fatalf("初始管理读取结果异常: isDef=%v title=%q", getResp.IsDefault, getResp.Config.Hero.Title)
	}

	// 提交非法颜色返回 400
	badCfg := getResp.Config
	badCfg.Theme.AccentColor = "javascript:alert(1)"
	badBody, _ := json.Marshal(badCfg)
	if rec := doReq(h, http.MethodPut, "/api/v1/console/cms/home", string(badBody), tok); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法色值应返回 400，实际=%d body=%s", rec.Code, rec.Body.String())
	}

	// 提交合法自定义配置返回 200
	custom := getResp.Config
	custom.Hero.Title = "自定义首页标题"
	custom.Hero.Subtitle = "自定义首页副标题"
	customBody, _ := json.Marshal(custom)
	rec = doReq(h, http.MethodPut, "/api/v1/console/cms/home", string(customBody), tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存合法配置应返回 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}

	// 公开接口立刻反映自定义配置且 is_default = false
	rec = doReq(h, http.MethodGet, "/api/v1/cms/home", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("公开读取失败: %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp); err != nil {
		t.Fatal(err)
	}
	if getResp.IsDefault || getResp.Config.Hero.Title != "自定义首页标题" {
		t.Fatalf("公开接口未反映保存结果: isDef=%v title=%q", getResp.IsDefault, getResp.Config.Hero.Title)
	}

	// 删除重置返回默认配置
	rec = doReq(h, http.MethodDelete, "/api/v1/console/cms/home", "", tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("重置配置应返回 200，实际=%d body=%s", rec.Code, rec.Body.String())
	}
	var resetResp struct {
		Config cms.HomeConfig `json:"config"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resetResp); err != nil {
		t.Fatal(err)
	}
	if resetResp.Config.Hero.Title != "配置兜底标题" {
		t.Fatalf("重置后未恢复默认标题: %q", resetResp.Config.Hero.Title)
	}

	rec = doReq(h, http.MethodGet, "/api/v1/cms/home", "", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp); err != nil {
		t.Fatal(err)
	}
	if !getResp.IsDefault {
		t.Fatal("重置后公开接口 is_default 应恢复为 true")
	}
}
