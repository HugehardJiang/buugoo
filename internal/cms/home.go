package cms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const homeSettingKey = "home"

// 官网首页配置的数组数量、数值区间与文本长度上限。
const (
	MaxHomeNavLinks         = 12
	MaxHomeShowcaseCards    = 12
	MaxHomeCapabilities     = 16
	MaxHomeCapabilityLinks  = 8
	MaxHomeSpotlightItems   = 12
	MaxHomeCustomBlocks     = 12
	MaxHomeFooterColumns    = 6
	MaxHomeFooterColLinks   = 12
	MaxHomeFooterBottomLink = 10

	MinHomeCardRadius = 16
	MaxHomeCardRadius = 32
	DefaultCardRadius = 24

	MinHomeBgOverlay = 0
	MaxHomeBgOverlay = 80

	MinHomeNewsLimit     = 3
	MaxHomeNewsLimit     = 8
	DefaultHomeNewsLimit = 5

	MaxHomeIDLen       = 64
	MaxHomeLabelLen    = 80
	MaxHomeTitleLen    = 200
	MaxHomeSubtitleLen = 600
	MaxHomeBodyRunes   = 20000
	MaxHomeURLLen      = 512
	MaxHomeIconLen     = 32
)

// 官网首页内置区块标识与默认顺序。
const (
	SectionHero         = "hero"
	SectionShowcase     = "showcase"
	SectionNews         = "news"
	SectionCapabilities = "capabilities"
	SectionSpotlight    = "spotlight"
	SectionCustom       = "custom"
	SectionFooter       = "footer"
)

var defaultHomeSectionOrder = []HomeSection{
	{ID: SectionHero, Label: "首屏主视觉", Visible: true},
	{ID: SectionShowcase, Label: "焦点展示卡", Visible: true},
	{ID: SectionNews, Label: "最新动态", Visible: true},
	{ID: SectionCapabilities, Label: "核心能力", Visible: true},
	{ID: SectionSpotlight, Label: "真实相处", Visible: true},
	{ID: SectionCustom, Label: "自定义区块", Visible: true},
	{ID: SectionFooter, Label: "页脚", Visible: true},
}

// siteSettingRow 是 cms_site_settings 表对应的持久化模型。
type siteSettingRow struct {
	Key       string `gorm:"column:key;size:64;primaryKey"`
	ValueJSON string `gorm:"column:value_json;type:mediumtext;not null"`
	UpdatedAt time.Time
}

func (siteSettingRow) TableName() string { return "cms_site_settings" }

// HomeConfig 是官网首页高自由度编排配置。
type HomeConfig struct {
	Theme        HomeTheme         `json:"theme"`
	Header       HomeHeader        `json:"header"`
	Sections     []HomeSection     `json:"sections"`
	Hero         HomeHero          `json:"hero"`
	Showcase     HomeShowcase      `json:"showcase"`
	News         HomeNews          `json:"news"`
	Capabilities HomeCapabilities  `json:"capabilities"`
	Spotlight    HomeSpotlight     `json:"spotlight"`
	CustomBlocks []HomeCustomBlock `json:"custom_blocks"`
	Footer       HomeFooter        `json:"footer"`
}

// HomeTheme 定义官网视觉色板、字体风格与圆角尺寸。
type HomeTheme struct {
	CanvasColor       string `json:"canvas_color"`
	AccentColor       string `json:"accent_color"`
	HeroGlowPrimary   string `json:"hero_glow_primary"`
	HeroGlowSecondary string `json:"hero_glow_secondary"`
	HeroHeight        string `json:"hero_height"`
	HeaderBlur        bool   `json:"header_blur"`
	FontStyle         string `json:"font_style"`
	CardRadius        int    `json:"card_radius"`
}

// HomeLink 表示通用导航或按钮链接。
type HomeLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// HomeHeader 定义顶栏品牌名、导航项与右侧行动按钮。
type HomeHeader struct {
	BrandName         string     `json:"brand_name"`
	NavLinks          []HomeLink `json:"nav_links"`
	PrimaryCTALabel   string     `json:"primary_cta_label"`
	PrimaryCTAURL     string     `json:"primary_cta_url"`
	SecondaryCTALabel string     `json:"secondary_cta_label"`
	SecondaryCTAURL   string     `json:"secondary_cta_url"`
}

// HomeSection 定义首页区块顺序与显示开关。
type HomeSection struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Visible bool   `json:"visible"`
}

// HomeHero 定义首屏主视觉文案、按钮与可选背景图。
type HomeHero struct {
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle"`
	PrimaryLabel   string `json:"primary_label"`
	PrimaryURL     string `json:"primary_url"`
	SecondaryLabel string `json:"secondary_label"`
	SecondaryURL   string `json:"secondary_url"`
	BgImageURL     string `json:"bg_image_url"`
	BgOverlay      int    `json:"bg_overlay"`
}

// HomeShowcase 定义 2:3 竖版焦点展示卡轮播。
type HomeShowcase struct {
	Cards []HomeShowcaseCard `json:"cards"`
}

// HomeShowcaseCard 是单张竖版焦点展示卡。
type HomeShowcaseCard struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle"`
	Theme          string `json:"theme"`
	ImageURL       string `json:"image_url"`
	PrimaryLabel   string `json:"primary_label"`
	PrimaryURL     string `json:"primary_url"`
	SecondaryLabel string `json:"secondary_label"`
	SecondaryURL   string `json:"secondary_url"`
}

// HomeNews 定义最新动态文章板块。
type HomeNews struct {
	Title        string `json:"title"`
	ViewAllLabel string `json:"view_all_label"`
	ViewAllURL   string `json:"view_all_url"`
	Category     string `json:"category"`
	Limit        int    `json:"limit"`
}

// HomeCapabilities 定义多列图标能力与生态书架板块。
type HomeCapabilities struct {
	Title    string               `json:"title"`
	Subtitle string               `json:"subtitle"`
	CTALabel string               `json:"cta_label"`
	CTAURL   string               `json:"cta_url"`
	Items    []HomeCapabilityItem `json:"items"`
}

// HomeCapabilityItem 是单张能力或生态卡片。
type HomeCapabilityItem struct {
	ID             string     `json:"id"`
	Icon           string     `json:"icon"`
	Badge          string     `json:"badge"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	LinksHeading   string     `json:"links_heading"`
	Links          []HomeLink `json:"links"`
	LearnMoreLabel string     `json:"learn_more_label"`
	LearnMoreURL   string     `json:"learn_more_url"`
}

// HomeSpotlight 定义左侧手风琴与右侧交互舞台板块。
type HomeSpotlight struct {
	Title    string              `json:"title"`
	Subtitle string              `json:"subtitle"`
	CTALabel string              `json:"cta_label"`
	CTAURL   string              `json:"cta_url"`
	Items    []HomeSpotlightItem `json:"items"`
}

// HomeSpotlightItem 是手风琴的单个条目及对应舞台配置。
type HomeSpotlightItem struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	StageType     string `json:"stage_type"`
	ImageURL      string `json:"image_url"`
	StageTitle    string `json:"stage_title"`
	StageBody     string `json:"stage_body"`
	StageCTALabel string `json:"stage_cta_label"`
	StageCTAURL   string `json:"stage_cta_url"`
}

// HomeCustomBlock 是管理员自由扩展的自定义内容区块。
type HomeCustomBlock struct {
	ID             string `json:"id"`
	Eyebrow        string `json:"eyebrow"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle"`
	BodyMD         string `json:"body_md"`
	ImageURL       string `json:"image_url"`
	Theme          string `json:"theme"`
	CTALabel       string `json:"cta_label"`
	CTAURL         string `json:"cta_url"`
	SecondaryLabel string `json:"secondary_label"`
	SecondaryURL   string `json:"secondary_url"`
}

// HomeFooter 定义页脚行动条、使命宣言、分组链接与底部说明。
type HomeFooter struct {
	ShowCTABar     bool               `json:"show_cta_bar"`
	CTAHeading     string             `json:"cta_heading"`
	CTANote        string             `json:"cta_note"`
	CTAPlaceholder string             `json:"cta_placeholder"`
	CTAButtonLabel string             `json:"cta_button_label"`
	CTAButtonURL   string             `json:"cta_button_url"`
	Mission        string             `json:"mission"`
	Columns        []HomeFooterColumn `json:"columns"`
	BottomNote     string             `json:"bottom_note"`
	BottomLinks    []HomeLink         `json:"bottom_links"`
}

// HomeFooterColumn 是页脚单栏链接分组。
type HomeFooterColumn struct {
	Title string           `json:"title"`
	Links []HomeFooterLink `json:"links"`
}

// HomeFooterLink 是页脚分组内的单条链接。
type HomeFooterLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Icon  string `json:"icon"`
}

// DefaultHomeConfig 返回保留布谷原色与完整中文内容的默认官网首页配置。
func DefaultHomeConfig(siteName, fallbackTitle, fallbackTagline string) HomeConfig {
	name := strings.TrimSpace(siteName)
	if name == "" {
		name = "布谷"
	}
	heroTitle := strings.TrimSpace(strings.ReplaceAll(fallbackTitle, "\r\n", "\n"))
	if heroTitle == "" {
		heroTitle = name + "，一个真的\n记得你的 AI 朋友"
	}
	heroSubtitle := strings.TrimSpace(strings.ReplaceAll(fallbackTagline, "\r\n", "\n"))
	if heroSubtitle == "" {
		heroSubtitle = "记得你说过的话，有自己的情绪起伏，也能在网页、微信与 QQ 里陪你处理日常事务。"
	}

	sections := make([]HomeSection, len(defaultHomeSectionOrder))
	copy(sections, defaultHomeSectionOrder)

	return HomeConfig{
		Theme: HomeTheme{
			CanvasColor:       "#faf9f5",
			AccentColor:       "#5b7a4a",
			HeroGlowPrimary:   "#8fa67f",
			HeroGlowSecondary: "#bcd1ca",
			HeroHeight:        "full",
			HeaderBlur:        true,
			FontStyle:         "sans",
			CardRadius:        DefaultCardRadius,
		},
		Header: HomeHeader{
			BrandName: name,
			NavLinks: []HomeLink{
				{Label: "能力", URL: "#capabilities"},
				{Label: "文章", URL: "/posts"},
				{Label: "真实相处", URL: "#spotlight"},
				{Label: "盲测竞技场", URL: "https://arena.buugoo.com"},
			},
			PrimaryCTALabel:   "",
			PrimaryCTAURL:     "",
			SecondaryCTALabel: "",
			SecondaryCTAURL:   "",
		},
		Sections: sections,
		Hero: HomeHero{
			Title:          heroTitle,
			Subtitle:       heroSubtitle,
			PrimaryLabel:   "开始聊天",
			PrimaryURL:     "/login",
			SecondaryLabel: "了解核心能力",
			SecondaryURL:   "#capabilities",
			BgImageURL:     "",
			BgOverlay:      0,
		},
		Showcase: HomeShowcase{
			Cards: []HomeShowcaseCard{
				{
					ID:             "memory",
					Title:          "长期记忆引擎",
					Subtitle:       "跨对话自然记住你说过的细节与人际关系",
					Theme:          "dark",
					ImageURL:       "",
					PrimaryLabel:   "了解记忆架构",
					PrimaryURL:     "/posts",
					SecondaryLabel: "查看演示",
					SecondaryURL:   "#spotlight",
				},
				{
					ID:             "emotion",
					Title:          "有脾气的情绪系统",
					Subtitle:       "会开心、担心、想念，也能自由切换六种性格",
					Theme:          "olive",
					ImageURL:       "",
					PrimaryLabel:   "立即体验",
					PrimaryURL:     "/login",
					SecondaryLabel: "探索能力",
					SecondaryURL:   "#capabilities",
				},
				{
					ID:             "arena",
					Title:          "BuugooArena 盲测竞技场",
					Subtitle:       "布谷旗下免登录真人双盲评测站，测测谁更像活人",
					Theme:          "dark",
					ImageURL:       "",
					PrimaryLabel:   "访问榜单",
					PrimaryURL:     "https://arena.buugoo.com",
					SecondaryLabel: "参与盲测",
					SecondaryURL:   "https://arena.buugoo.com/arena",
				},
				{
					ID:             "channels",
					Title:          "微信、QQ 与多端同步",
					Subtitle:       "网页与聊天软件共用记忆、提醒和文件工具",
					Theme:          "mist",
					ImageURL:       "",
					PrimaryLabel:   "开始使用",
					PrimaryURL:     "/login",
					SecondaryLabel: "了解更多",
					SecondaryURL:   "#capabilities",
				},
			},
		},
		News: HomeNews{
			Title:        "最新动态",
			ViewAllLabel: "查看全部文章",
			ViewAllURL:   "/posts",
			Category:     "",
			Limit:        DefaultHomeNewsLimit,
		},
		Capabilities: HomeCapabilities{
			Title:    "为长期陪伴而设计的",
			Subtitle: "记忆、情绪与日常工具",
			CTALabel: "浏览技术文章",
			CTAURL:   "/posts",
			Items: []HomeCapabilityItem{
				{
					ID:             "memory",
					Icon:           "brain",
					Badge:          "",
					Title:          "长期记忆",
					Description:    "结合语义检索与时间衰减，把聊天里提到的习惯、偏好与重要日子自然留存。",
					LinksHeading:   "延伸阅读",
					Links:          []HomeLink{{Label: "记忆架构解析", URL: "/posts"}},
					LearnMoreLabel: "查看真实对话",
					LearnMoreURL:   "#spotlight",
				},
				{
					ID:             "emotion",
					Icon:           "sparkle",
					Badge:          "新",
					Title:          "情绪与性格",
					Description:    "具备连贯的情绪状态与六种性格预设，告别机械客服腔，说话更有分寸和温度。",
					LinksHeading:   "快速入口",
					Links:          []HomeLink{{Label: "登录后切换性格", URL: "/login"}},
					LearnMoreLabel: "立即体验",
					LearnMoreURL:   "/login",
				},
				{
					ID:             "reminders",
					Icon:           "clock",
					Badge:          "",
					Title:          "提醒与定时任务",
					Description:    "一句口语就能设好单次或周期提醒，到点经由网页、微信或 QQ 主动找到你。",
					LinksHeading:   "常见场景",
					Links:          []HomeLink{{Label: "主动关怀演示", URL: "#spotlight"}},
					LearnMoreLabel: "了解更多",
					LearnMoreURL:   "#spotlight",
				},
				{
					ID:             "memos",
					Icon:           "note",
					Badge:          "",
					Title:          "备忘录与待办",
					Description:    "聊着天随手记下灵感、清单与待办事项，自动归档并在需要时为你调取。",
					LinksHeading:   "协作能力",
					Links:          []HomeLink{{Label: "查看使用指南", URL: "/posts"}},
					LearnMoreLabel: "开始记录",
					LearnMoreURL:   "/login",
				},
				{
					ID:           "arena",
					Icon:         "globe",
					Badge:        "",
					Title:        "BuugooArena 评测站",
					Description:  "布谷旗下的中文拟人对话盲测竞技场，免登录对比主流模型谁更懂分寸、更像活人。",
					LinksHeading: "直达榜单",
					Links: []HomeLink{
						{Label: "网感与接梗", URL: "https://arena.buugoo.com/"},
						{Label: "说人话", URL: "https://arena.buugoo.com/?col=plain"},
						{Label: "直接参与盲测", URL: "https://arena.buugoo.com/arena"},
					},
					LearnMoreLabel: "前往 BuugooArena",
					LearnMoreURL:   "https://arena.buugoo.com",
				},
			},
		},
		Spotlight: HomeSpotlight{
			Title:    "不是问答工具",
			Subtitle: "是每天都能自然相处的伙伴",
			CTALabel: "进入对话",
			CTAURL:   "/login",
			Items: []HomeSpotlightItem{
				{
					ID:            "memory-recall",
					Title:         "上周说过的话，今天还记得",
					Description:   "你随口提过的工作节点、忌口习惯和正在忙的项目，都会沉淀进专属记忆库，下次聊天自然衔接，无需反复铺垫。",
					StageType:     "chat-demo",
					ImageURL:      "",
					StageTitle:    "跨对话自然回忆与主动提醒",
					StageBody:     "自动检索历史语义记忆，并在合适时机创建定时提醒。",
					StageCTALabel: "亲自试试",
					StageCTAURL:   "/login",
				},
				{
					ID:            "arena-eval",
					Title:         "BuugooArena 真人双盲评测",
					Description:   "开放透明的中文真人盲测竞技场，匿名对比两段回复并投票，实时沉淀各模型在网感、说人话与综合自然度上的真实表现。",
					StageType:     "arena-preview",
					ImageURL:      "",
					StageTitle:    "BuugooArena 盲测竞技场",
					StageBody:     "免登录匿名双盲对决，直观查看不同模型的真实对话表现与性价比曲线。",
					StageCTALabel: "前往盲测站",
					StageCTAURL:   "https://arena.buugoo.com",
				},
				{
					ID:            "proactive-care",
					Title:         "到了时间，它会主动找你",
					Description:   "从吃药、交方案到纪念日问候，只要聊过一遍，到了约定时间便会经由网页、微信或 QQ 轻声提醒，不打扰也绝不遗漏。",
					StageType:     "editorial",
					ImageURL:      "",
					StageTitle:    "有分寸的主动联系",
					StageBody:     "支持按单次、每天、每周或自定义周期触发，可在个人设置中随时暂停或调整免打扰时段。",
					StageCTALabel: "登录开启提醒",
					StageCTAURL:   "/login",
				},
				{
					ID:            "quick-start",
					Title:         "三步开始，认识新朋友",
					Description:   "注册或登录后，配置常用模型并挑选合拍的性格预设，就能在浏览器或常用聊天软件里开启持续陪伴。",
					StageType:     "editorial",
					ImageURL:      "",
					StageTitle:    "轻装上阵，三分钟完成配置",
					StageBody:     "1. 登录进入对话工作台\n2. 挑选或接入顺手的对话模型\n3. 按需绑定微信或 QQ，开启跨端同步",
					StageCTALabel: "立即开始",
					StageCTAURL:   "/login",
				},
			},
		},
		CustomBlocks: []HomeCustomBlock{},
		Footer: HomeFooter{
			ShowCTABar:     true,
			CTAHeading:     "准备好认识一个记得你的 AI 朋友了吗",
			CTANote:        "支持网页端与主流聊天软件多端同步，数据完全由你自己掌控。",
			CTAPlaceholder: "",
			CTAButtonLabel: "进入工作台",
			CTAButtonURL:   "/login",
			Mission:        name + "致力于做一位\n有记忆、有温度的 AI 朋友",
			Columns: []HomeFooterColumn{
				{
					Title: "产品与能力",
					Links: []HomeFooterLink{
						{Label: "长期记忆引擎", URL: "#capabilities", Icon: "brain"},
						{Label: "情绪与性格预设", URL: "#capabilities", Icon: "sparkle"},
						{Label: "提醒与待办工具", URL: "#spotlight", Icon: "clock"},
						{Label: "开始使用", URL: "/login", Icon: "message"},
					},
				},
				{
					Title: "文章与动态",
					Links: []HomeFooterLink{
						{Label: "全部文章", URL: "/posts", Icon: "note"},
						{Label: "最新动态", URL: "#news", Icon: "star"},
						{Label: "RSS 订阅", URL: "/rss.xml", Icon: "globe"},
					},
				},
				{
					Title: "旗下生态",
					Links: []HomeFooterLink{
						{Label: "BuugooArena 评测站", URL: "https://arena.buugoo.com", Icon: "globe"},
						{Label: "网感与接梗榜单", URL: "https://arena.buugoo.com/", Icon: "sparkle"},
						{Label: "说人话榜单", URL: "https://arena.buugoo.com/?col=plain", Icon: "message"},
						{Label: "直接参与盲测", URL: "https://arena.buugoo.com/arena", Icon: "star"},
					},
				},
			},
			BottomNote: "对话记录与个人记忆仅保存在当前实例数据库中，不会用于外部训练。",
			BottomLinks: []HomeLink{
				{Label: "文章与动态", URL: "/posts"},
				{Label: "RSS 订阅", URL: "/rss.xml"},
				{Label: "盲测竞技场", URL: "https://arena.buugoo.com"},
			},
		},
	}
}

// GetHomeConfig 读取官网首页配置；未配置或历史数据缺字段时以默认值补全，永不报错。
func (s *Service) GetHomeConfig(ctx context.Context, siteName, fallbackTitle, fallbackTagline string) (HomeConfig, bool, error) {
	def := DefaultHomeConfig(siteName, fallbackTitle, fallbackTagline)
	if s == nil || s.db == nil {
		return def, true, nil
	}

	var row siteSettingRow
	err := s.db.WithContext(ctx).Where(&siteSettingRow{Key: homeSettingKey}).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return def, true, nil
		}
		// 读取异常时回退默认配置，保证首页始终可用。
		return def, true, nil
	}

	raw := strings.TrimSpace(row.ValueJSON)
	if raw == "" {
		return def, true, nil
	}

	cfg := def
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return def, true, nil
	}
	normalizeLoadedHomeConfig(&cfg, def)
	return cfg, false, nil
}

// SaveHomeConfig 校验并持久化官网首页配置，兼容 SQLite 与 MySQL 8.4。
func (s *Service) SaveHomeConfig(ctx context.Context, cfg HomeConfig) (HomeConfig, error) {
	cleaned, err := cleanHomeConfig(cfg)
	if err != nil {
		return HomeConfig{}, err
	}
	raw, err := json.Marshal(cleaned)
	if err != nil {
		return HomeConfig{}, fmt.Errorf("编码首页配置: %w", err)
	}
	row := siteSettingRow{
		Key:       homeSettingKey,
		ValueJSON: string(raw),
		UpdatedAt: s.now().UTC(),
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value_json", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return HomeConfig{}, err
	}
	return cleaned, nil
}

// ResetHomeConfig 删除自定义的官网首页配置并返回默认配置。
func (s *Service) ResetHomeConfig(ctx context.Context, siteName, fallbackTitle, fallbackTagline string) (HomeConfig, error) {
	def := DefaultHomeConfig(siteName, fallbackTitle, fallbackTagline)
	if s == nil || s.db == nil {
		return def, nil
	}
	if err := s.db.WithContext(ctx).Where(&siteSettingRow{Key: homeSettingKey}).Delete(&siteSettingRow{}).Error; err != nil {
		return HomeConfig{}, err
	}
	return def, nil
}

func cleanHomeConfig(in HomeConfig) (HomeConfig, error) {
	var err error
	out := in

	// 1. Theme
	if out.Theme.CanvasColor, err = validateHexColor(out.Theme.CanvasColor, "画布底色"); err != nil {
		return HomeConfig{}, err
	}
	if out.Theme.AccentColor, err = validateHexColor(out.Theme.AccentColor, "主题点缀色"); err != nil {
		return HomeConfig{}, err
	}
	if out.Theme.HeroGlowPrimary, err = validateHexColor(out.Theme.HeroGlowPrimary, "主视觉主光晕色"); err != nil {
		return HomeConfig{}, err
	}
	if out.Theme.HeroGlowSecondary, err = validateHexColor(out.Theme.HeroGlowSecondary, "主视觉辅光晕色"); err != nil {
		return HomeConfig{}, err
	}
	out.Theme.HeroHeight = strings.ToLower(strings.TrimSpace(out.Theme.HeroHeight))
	if out.Theme.HeroHeight == "" {
		out.Theme.HeroHeight = "full"
	}
	if out.Theme.HeroHeight != "full" && out.Theme.HeroHeight != "compact" {
		return HomeConfig{}, &UserError{"主视觉高度无效，可选值：full、compact"}
	}
	out.Theme.FontStyle = strings.ToLower(strings.TrimSpace(out.Theme.FontStyle))
	if out.Theme.FontStyle == "" {
		out.Theme.FontStyle = "sans"
	}
	if out.Theme.FontStyle != "sans" && out.Theme.FontStyle != "serif" {
		return HomeConfig{}, &UserError{"字体风格无效，可选值：sans、serif"}
	}
	if out.Theme.CardRadius == 0 {
		out.Theme.CardRadius = DefaultCardRadius
	}
	if out.Theme.CardRadius < MinHomeCardRadius || out.Theme.CardRadius > MaxHomeCardRadius {
		return HomeConfig{}, &UserError{fmt.Sprintf("卡片圆角需在 %d 到 %d 之间", MinHomeCardRadius, MaxHomeCardRadius)}
	}

	// 2. Header
	if out.Header.BrandName, err = cleanSingleLine(out.Header.BrandName, MaxHomeLabelLen, "顶栏品牌名"); err != nil {
		return HomeConfig{}, err
	}
	if out.Header.NavLinks, err = cleanHomeLinks(out.Header.NavLinks, MaxHomeNavLinks, "顶栏导航链接"); err != nil {
		return HomeConfig{}, err
	}
	if out.Header.PrimaryCTALabel, err = cleanSingleLine(out.Header.PrimaryCTALabel, MaxHomeLabelLen, "顶栏主按钮文案"); err != nil {
		return HomeConfig{}, err
	}
	if out.Header.PrimaryCTAURL, err = validateHomeURL(out.Header.PrimaryCTAURL, "顶栏主按钮链接", true); err != nil {
		return HomeConfig{}, err
	}
	if out.Header.SecondaryCTALabel, err = cleanSingleLine(out.Header.SecondaryCTALabel, MaxHomeLabelLen, "顶栏次按钮文案"); err != nil {
		return HomeConfig{}, err
	}
	if out.Header.SecondaryCTAURL, err = validateHomeURL(out.Header.SecondaryCTAURL, "顶栏次按钮链接", true); err != nil {
		return HomeConfig{}, err
	}

	// 3. Sections
	if out.Sections, err = cleanHomeSections(out.Sections); err != nil {
		return HomeConfig{}, err
	}

	// 4. Hero
	if out.Hero.Title, err = cleanMultiLine(out.Hero.Title, MaxHomeTitleLen, "主视觉标题"); err != nil {
		return HomeConfig{}, err
	}
	if out.Hero.Subtitle, err = cleanMultiLine(out.Hero.Subtitle, MaxHomeSubtitleLen, "主视觉副标题"); err != nil {
		return HomeConfig{}, err
	}
	if out.Hero.PrimaryLabel, err = cleanSingleLine(out.Hero.PrimaryLabel, MaxHomeLabelLen, "主视觉主按钮文案"); err != nil {
		return HomeConfig{}, err
	}
	if out.Hero.PrimaryURL, err = validateHomeURL(out.Hero.PrimaryURL, "主视觉主按钮链接", true); err != nil {
		return HomeConfig{}, err
	}
	if out.Hero.SecondaryLabel, err = cleanSingleLine(out.Hero.SecondaryLabel, MaxHomeLabelLen, "主视觉次按钮文案"); err != nil {
		return HomeConfig{}, err
	}
	if out.Hero.SecondaryURL, err = validateHomeURL(out.Hero.SecondaryURL, "主视觉次按钮链接", true); err != nil {
		return HomeConfig{}, err
	}
	if out.Hero.BgImageURL, err = validateHomeURL(out.Hero.BgImageURL, "主视觉背景图链接", false); err != nil {
		return HomeConfig{}, err
	}
	if out.Hero.BgOverlay < MinHomeBgOverlay || out.Hero.BgOverlay > MaxHomeBgOverlay {
		return HomeConfig{}, &UserError{fmt.Sprintf("主视觉背景遮罩透明度需在 %d 到 %d 之间", MinHomeBgOverlay, MaxHomeBgOverlay)}
	}

	// 5. Showcase
	if len(out.Showcase.Cards) > MaxHomeShowcaseCards {
		return HomeConfig{}, &UserError{fmt.Sprintf("焦点展示卡最多配置 %d 张", MaxHomeShowcaseCards)}
	}
	cards := make([]HomeShowcaseCard, 0, len(out.Showcase.Cards))
	for i, card := range out.Showcase.Cards {
		if card.ID, err = cleanItemID(card.ID, fmt.Sprintf("card-%d", i+1)); err != nil {
			return HomeConfig{}, err
		}
		if card.Title, err = cleanSingleLine(card.Title, MaxHomeTitleLen, "展示卡标题"); err != nil {
			return HomeConfig{}, err
		}
		if card.Subtitle, err = cleanMultiLine(card.Subtitle, MaxHomeSubtitleLen, "展示卡副标题"); err != nil {
			return HomeConfig{}, err
		}
		card.Theme = strings.ToLower(strings.TrimSpace(card.Theme))
		if card.Theme == "" {
			card.Theme = "dark"
		}
		if !validShowcaseTheme(card.Theme) {
			return HomeConfig{}, &UserError{"展示卡主题无效，可选值：dark、olive、mist、sand"}
		}
		if card.ImageURL, err = validateHomeURL(card.ImageURL, "展示卡配图链接", false); err != nil {
			return HomeConfig{}, err
		}
		if card.PrimaryLabel, err = cleanSingleLine(card.PrimaryLabel, MaxHomeLabelLen, "展示卡主按钮文案"); err != nil {
			return HomeConfig{}, err
		}
		if card.PrimaryURL, err = validateHomeURL(card.PrimaryURL, "展示卡主按钮链接", true); err != nil {
			return HomeConfig{}, err
		}
		if card.SecondaryLabel, err = cleanSingleLine(card.SecondaryLabel, MaxHomeLabelLen, "展示卡次按钮文案"); err != nil {
			return HomeConfig{}, err
		}
		if card.SecondaryURL, err = validateHomeURL(card.SecondaryURL, "展示卡次按钮链接", true); err != nil {
			return HomeConfig{}, err
		}
		cards = append(cards, card)
	}
	out.Showcase.Cards = cards

	// 6. News
	if out.News.Title, err = cleanSingleLine(out.News.Title, MaxHomeTitleLen, "最新动态标题"); err != nil {
		return HomeConfig{}, err
	}
	if out.News.ViewAllLabel, err = cleanSingleLine(out.News.ViewAllLabel, MaxHomeLabelLen, "查看全部文案"); err != nil {
		return HomeConfig{}, err
	}
	if out.News.ViewAllURL, err = validateHomeURL(out.News.ViewAllURL, "查看全部链接", true); err != nil {
		return HomeConfig{}, err
	}
	if out.News.Category, err = cleanSingleLine(out.News.Category, MaxCategoryLen, "文章分类筛选"); err != nil {
		return HomeConfig{}, err
	}
	if out.News.Limit == 0 {
		out.News.Limit = DefaultHomeNewsLimit
	}
	if out.News.Limit < MinHomeNewsLimit || out.News.Limit > MaxHomeNewsLimit {
		return HomeConfig{}, &UserError{fmt.Sprintf("最新动态展示篇数需在 %d 到 %d 之间", MinHomeNewsLimit, MaxHomeNewsLimit)}
	}

	// 7. Capabilities
	if out.Capabilities.Title, err = cleanSingleLine(out.Capabilities.Title, MaxHomeTitleLen, "能力板块标题"); err != nil {
		return HomeConfig{}, err
	}
	if out.Capabilities.Subtitle, err = cleanSingleLine(out.Capabilities.Subtitle, MaxHomeSubtitleLen, "能力板块副标题"); err != nil {
		return HomeConfig{}, err
	}
	if out.Capabilities.CTALabel, err = cleanSingleLine(out.Capabilities.CTALabel, MaxHomeLabelLen, "能力板块按钮文案"); err != nil {
		return HomeConfig{}, err
	}
	if out.Capabilities.CTAURL, err = validateHomeURL(out.Capabilities.CTAURL, "能力板块按钮链接", true); err != nil {
		return HomeConfig{}, err
	}
	if len(out.Capabilities.Items) > MaxHomeCapabilities {
		return HomeConfig{}, &UserError{fmt.Sprintf("能力卡片最多配置 %d 项", MaxHomeCapabilities)}
	}
	capItems := make([]HomeCapabilityItem, 0, len(out.Capabilities.Items))
	for i, item := range out.Capabilities.Items {
		if item.ID, err = cleanItemID(item.ID, fmt.Sprintf("cap-%d", i+1)); err != nil {
			return HomeConfig{}, err
		}
		item.Icon = strings.ToLower(strings.TrimSpace(item.Icon))
		if item.Icon == "" {
			item.Icon = "sparkle"
		}
		if !validCapabilityIcon(item.Icon) {
			return HomeConfig{}, &UserError{"能力卡片图标无效，可选值：brain、sparkle、clock、note、message、globe、code、mask、star"}
		}
		if item.Badge, err = cleanSingleLine(item.Badge, 24, "能力卡片角标"); err != nil {
			return HomeConfig{}, err
		}
		if item.Title, err = cleanSingleLine(item.Title, MaxHomeTitleLen, "能力卡片标题"); err != nil {
			return HomeConfig{}, err
		}
		if item.Description, err = cleanMultiLine(item.Description, MaxHomeSubtitleLen, "能力卡片描述"); err != nil {
			return HomeConfig{}, err
		}
		if item.LinksHeading, err = cleanSingleLine(item.LinksHeading, MaxHomeLabelLen, "能力子链接标题"); err != nil {
			return HomeConfig{}, err
		}
		if item.Links, err = cleanHomeLinks(item.Links, MaxHomeCapabilityLinks, "能力卡片直达链接"); err != nil {
			return HomeConfig{}, err
		}
		if item.LearnMoreLabel, err = cleanSingleLine(item.LearnMoreLabel, MaxHomeLabelLen, "能力了解更多文案"); err != nil {
			return HomeConfig{}, err
		}
		if item.LearnMoreURL, err = validateHomeURL(item.LearnMoreURL, "能力了解更多链接", true); err != nil {
			return HomeConfig{}, err
		}
		capItems = append(capItems, item)
	}
	out.Capabilities.Items = capItems

	// 8. Spotlight
	if out.Spotlight.Title, err = cleanSingleLine(out.Spotlight.Title, MaxHomeTitleLen, "交互舞台标题"); err != nil {
		return HomeConfig{}, err
	}
	if out.Spotlight.Subtitle, err = cleanSingleLine(out.Spotlight.Subtitle, MaxHomeSubtitleLen, "交互舞台副标题"); err != nil {
		return HomeConfig{}, err
	}
	if out.Spotlight.CTALabel, err = cleanSingleLine(out.Spotlight.CTALabel, MaxHomeLabelLen, "交互舞台按钮文案"); err != nil {
		return HomeConfig{}, err
	}
	if out.Spotlight.CTAURL, err = validateHomeURL(out.Spotlight.CTAURL, "交互舞台按钮链接", true); err != nil {
		return HomeConfig{}, err
	}
	if len(out.Spotlight.Items) > MaxHomeSpotlightItems {
		return HomeConfig{}, &UserError{fmt.Sprintf("交互舞台条目最多配置 %d 项", MaxHomeSpotlightItems)}
	}
	spotItems := make([]HomeSpotlightItem, 0, len(out.Spotlight.Items))
	for i, item := range out.Spotlight.Items {
		if item.ID, err = cleanItemID(item.ID, fmt.Sprintf("spot-%d", i+1)); err != nil {
			return HomeConfig{}, err
		}
		if item.Title, err = cleanSingleLine(item.Title, MaxHomeTitleLen, "舞台条目标题"); err != nil {
			return HomeConfig{}, err
		}
		if item.Description, err = cleanMultiLine(item.Description, MaxHomeSubtitleLen, "舞台条目说明"); err != nil {
			return HomeConfig{}, err
		}
		item.StageType = strings.ToLower(strings.TrimSpace(item.StageType))
		if item.StageType == "" {
			item.StageType = "editorial"
		}
		if !validStageType(item.StageType) {
			return HomeConfig{}, &UserError{"交互舞台类型无效，可选值：chat-demo、arena-preview、image、editorial"}
		}
		if item.ImageURL, err = validateHomeURL(item.ImageURL, "舞台配图链接", false); err != nil {
			return HomeConfig{}, err
		}
		if item.StageTitle, err = cleanSingleLine(item.StageTitle, MaxHomeTitleLen, "舞台主标题"); err != nil {
			return HomeConfig{}, err
		}
		if item.StageBody, err = cleanMultiLine(item.StageBody, MaxHomeBodyRunes, "舞台正文"); err != nil {
			return HomeConfig{}, err
		}
		if item.StageCTALabel, err = cleanSingleLine(item.StageCTALabel, MaxHomeLabelLen, "舞台按钮文案"); err != nil {
			return HomeConfig{}, err
		}
		if item.StageCTAURL, err = validateHomeURL(item.StageCTAURL, "舞台按钮链接", true); err != nil {
			return HomeConfig{}, err
		}
		spotItems = append(spotItems, item)
	}
	out.Spotlight.Items = spotItems

	// 9. CustomBlocks
	if len(out.CustomBlocks) > MaxHomeCustomBlocks {
		return HomeConfig{}, &UserError{fmt.Sprintf("自定义区块最多配置 %d 个", MaxHomeCustomBlocks)}
	}
	blocks := make([]HomeCustomBlock, 0, len(out.CustomBlocks))
	for i, blk := range out.CustomBlocks {
		if blk.ID, err = cleanItemID(blk.ID, fmt.Sprintf("custom-%d", i+1)); err != nil {
			return HomeConfig{}, err
		}
		if blk.Eyebrow, err = cleanSingleLine(blk.Eyebrow, MaxHomeLabelLen, "自定义区块眉标"); err != nil {
			return HomeConfig{}, err
		}
		if blk.Title, err = cleanSingleLine(blk.Title, MaxHomeTitleLen, "自定义区块标题"); err != nil {
			return HomeConfig{}, err
		}
		if blk.Subtitle, err = cleanMultiLine(blk.Subtitle, MaxHomeSubtitleLen, "自定义区块副标题"); err != nil {
			return HomeConfig{}, err
		}
		if blk.BodyMD, err = cleanMultiLine(blk.BodyMD, MaxHomeBodyRunes, "自定义区块正文"); err != nil {
			return HomeConfig{}, err
		}
		if blk.ImageURL, err = validateHomeURL(blk.ImageURL, "自定义区块配图链接", false); err != nil {
			return HomeConfig{}, err
		}
		blk.Theme = strings.ToLower(strings.TrimSpace(blk.Theme))
		if blk.Theme == "" {
			blk.Theme = "light"
		}
		if !validCustomBlockTheme(blk.Theme) {
			return HomeConfig{}, &UserError{"自定义区块主题无效，可选值：light、card、dark"}
		}
		if blk.CTALabel, err = cleanSingleLine(blk.CTALabel, MaxHomeLabelLen, "自定义区块主按钮文案"); err != nil {
			return HomeConfig{}, err
		}
		if blk.CTAURL, err = validateHomeURL(blk.CTAURL, "自定义区块主按钮链接", true); err != nil {
			return HomeConfig{}, err
		}
		if blk.SecondaryLabel, err = cleanSingleLine(blk.SecondaryLabel, MaxHomeLabelLen, "自定义区块次按钮文案"); err != nil {
			return HomeConfig{}, err
		}
		if blk.SecondaryURL, err = validateHomeURL(blk.SecondaryURL, "自定义区块次按钮链接", true); err != nil {
			return HomeConfig{}, err
		}
		blocks = append(blocks, blk)
	}
	out.CustomBlocks = blocks

	// 10. Footer
	if out.Footer.CTAHeading, err = cleanSingleLine(out.Footer.CTAHeading, MaxHomeTitleLen, "页脚行动条标题"); err != nil {
		return HomeConfig{}, err
	}
	if out.Footer.CTANote, err = cleanMultiLine(out.Footer.CTANote, MaxHomeSubtitleLen, "页脚行动条说明"); err != nil {
		return HomeConfig{}, err
	}
	if out.Footer.CTAPlaceholder, err = cleanSingleLine(out.Footer.CTAPlaceholder, MaxHomeLabelLen, "页脚输入框提示"); err != nil {
		return HomeConfig{}, err
	}
	if out.Footer.CTAButtonLabel, err = cleanSingleLine(out.Footer.CTAButtonLabel, MaxHomeLabelLen, "页脚行动按钮文案"); err != nil {
		return HomeConfig{}, err
	}
	if out.Footer.CTAButtonURL, err = validateHomeURL(out.Footer.CTAButtonURL, "页脚行动按钮链接", true); err != nil {
		return HomeConfig{}, err
	}
	if out.Footer.Mission, err = cleanMultiLine(out.Footer.Mission, MaxHomeSubtitleLen, "页脚使命短句"); err != nil {
		return HomeConfig{}, err
	}
	if len(out.Footer.Columns) > MaxHomeFooterColumns {
		return HomeConfig{}, &UserError{fmt.Sprintf("页脚链接分栏最多配置 %d 栏", MaxHomeFooterColumns)}
	}
	cols := make([]HomeFooterColumn, 0, len(out.Footer.Columns))
	for _, col := range out.Footer.Columns {
		if col.Title, err = cleanSingleLine(col.Title, MaxHomeLabelLen, "页脚分栏标题"); err != nil {
			return HomeConfig{}, err
		}
		if len(col.Links) > MaxHomeFooterColLinks {
			return HomeConfig{}, &UserError{fmt.Sprintf("单个页脚分栏最多配置 %d 条链接", MaxHomeFooterColLinks)}
		}
		links := make([]HomeFooterLink, 0, len(col.Links))
		for _, lk := range col.Links {
			if lk.Label, err = cleanSingleLine(lk.Label, MaxHomeLabelLen, "页脚链接名称"); err != nil {
				return HomeConfig{}, err
			}
			if lk.URL, err = validateHomeURL(lk.URL, "页脚链接地址", true); err != nil {
				return HomeConfig{}, err
			}
			if lk.Icon, err = cleanOptionalIcon(lk.Icon); err != nil {
				return HomeConfig{}, err
			}
			links = append(links, lk)
		}
		col.Links = links
		cols = append(cols, col)
	}
	out.Footer.Columns = cols
	if out.Footer.BottomNote, err = cleanMultiLine(out.Footer.BottomNote, MaxHomeSubtitleLen, "页脚底部说明"); err != nil {
		return HomeConfig{}, err
	}
	if out.Footer.BottomLinks, err = cleanHomeLinks(out.Footer.BottomLinks, MaxHomeFooterBottomLink, "页脚底部链接"); err != nil {
		return HomeConfig{}, err
	}

	return out, nil
}

// normalizeLoadedHomeConfig 为从数据库读取的老数据或不完整数据兜底补全，保证永不报错。
func normalizeLoadedHomeConfig(cfg *HomeConfig, def HomeConfig) {
	if c, err := validateHexColor(cfg.Theme.CanvasColor, ""); err == nil {
		cfg.Theme.CanvasColor = c
	} else {
		cfg.Theme.CanvasColor = def.Theme.CanvasColor
	}
	if c, err := validateHexColor(cfg.Theme.AccentColor, ""); err == nil {
		cfg.Theme.AccentColor = c
	} else {
		cfg.Theme.AccentColor = def.Theme.AccentColor
	}
	if c, err := validateHexColor(cfg.Theme.HeroGlowPrimary, ""); err == nil {
		cfg.Theme.HeroGlowPrimary = c
	} else {
		cfg.Theme.HeroGlowPrimary = def.Theme.HeroGlowPrimary
	}
	if c, err := validateHexColor(cfg.Theme.HeroGlowSecondary, ""); err == nil {
		cfg.Theme.HeroGlowSecondary = c
	} else {
		cfg.Theme.HeroGlowSecondary = def.Theme.HeroGlowSecondary
	}
	if cfg.Theme.HeroHeight != "full" && cfg.Theme.HeroHeight != "compact" {
		cfg.Theme.HeroHeight = def.Theme.HeroHeight
	}
	if cfg.Theme.FontStyle != "sans" && cfg.Theme.FontStyle != "serif" {
		cfg.Theme.FontStyle = def.Theme.FontStyle
	}
	if cfg.Theme.CardRadius < MinHomeCardRadius || cfg.Theme.CardRadius > MaxHomeCardRadius {
		cfg.Theme.CardRadius = def.Theme.CardRadius
	}

	cfg.Sections = normalizeSectionsPermissive(cfg.Sections)

	if cfg.Hero.BgOverlay < MinHomeBgOverlay {
		cfg.Hero.BgOverlay = MinHomeBgOverlay
	} else if cfg.Hero.BgOverlay > MaxHomeBgOverlay {
		cfg.Hero.BgOverlay = MaxHomeBgOverlay
	}
	cfg.Hero.PrimaryURL = sanitizeLoadedURL(cfg.Hero.PrimaryURL, true)
	cfg.Hero.SecondaryURL = sanitizeLoadedURL(cfg.Hero.SecondaryURL, true)
	cfg.Hero.BgImageURL = sanitizeLoadedURL(cfg.Hero.BgImageURL, false)

	cfg.Header.PrimaryCTAURL = sanitizeLoadedURL(cfg.Header.PrimaryCTAURL, true)
	cfg.Header.SecondaryCTAURL = sanitizeLoadedURL(cfg.Header.SecondaryCTAURL, true)
	if cfg.Header.NavLinks == nil {
		cfg.Header.NavLinks = []HomeLink{}
	}
	for i := range cfg.Header.NavLinks {
		cfg.Header.NavLinks[i].URL = sanitizeLoadedURL(cfg.Header.NavLinks[i].URL, true)
	}

	if cfg.Showcase.Cards == nil {
		cfg.Showcase.Cards = []HomeShowcaseCard{}
	}
	for i := range cfg.Showcase.Cards {
		c := &cfg.Showcase.Cards[i]
		if !validShowcaseTheme(c.Theme) {
			c.Theme = "dark"
		}
		c.ImageURL = sanitizeLoadedURL(c.ImageURL, false)
		c.PrimaryURL = sanitizeLoadedURL(c.PrimaryURL, true)
		c.SecondaryURL = sanitizeLoadedURL(c.SecondaryURL, true)
	}

	if cfg.News.Limit < MinHomeNewsLimit || cfg.News.Limit > MaxHomeNewsLimit {
		cfg.News.Limit = def.News.Limit
	}
	cfg.News.ViewAllURL = sanitizeLoadedURL(cfg.News.ViewAllURL, true)

	cfg.Capabilities.CTAURL = sanitizeLoadedURL(cfg.Capabilities.CTAURL, true)
	if cfg.Capabilities.Items == nil {
		cfg.Capabilities.Items = []HomeCapabilityItem{}
	}
	for i := range cfg.Capabilities.Items {
		item := &cfg.Capabilities.Items[i]
		if !validCapabilityIcon(item.Icon) {
			item.Icon = "sparkle"
		}
		if item.Links == nil {
			item.Links = []HomeLink{}
		}
		for j := range item.Links {
			item.Links[j].URL = sanitizeLoadedURL(item.Links[j].URL, true)
		}
		item.LearnMoreURL = sanitizeLoadedURL(item.LearnMoreURL, true)
	}

	cfg.Spotlight.CTAURL = sanitizeLoadedURL(cfg.Spotlight.CTAURL, true)
	if cfg.Spotlight.Items == nil {
		cfg.Spotlight.Items = []HomeSpotlightItem{}
	}
	for i := range cfg.Spotlight.Items {
		item := &cfg.Spotlight.Items[i]
		if !validStageType(item.StageType) {
			item.StageType = "editorial"
		}
		item.ImageURL = sanitizeLoadedURL(item.ImageURL, false)
		item.StageCTAURL = sanitizeLoadedURL(item.StageCTAURL, true)
	}

	if cfg.CustomBlocks == nil {
		cfg.CustomBlocks = []HomeCustomBlock{}
	}
	for i := range cfg.CustomBlocks {
		blk := &cfg.CustomBlocks[i]
		if !validCustomBlockTheme(blk.Theme) {
			blk.Theme = "light"
		}
		blk.ImageURL = sanitizeLoadedURL(blk.ImageURL, false)
		blk.CTAURL = sanitizeLoadedURL(blk.CTAURL, true)
		blk.SecondaryURL = sanitizeLoadedURL(blk.SecondaryURL, true)
	}

	cfg.Footer.CTAButtonURL = sanitizeLoadedURL(cfg.Footer.CTAButtonURL, true)
	if cfg.Footer.Columns == nil {
		cfg.Footer.Columns = []HomeFooterColumn{}
	}
	for i := range cfg.Footer.Columns {
		if cfg.Footer.Columns[i].Links == nil {
			cfg.Footer.Columns[i].Links = []HomeFooterLink{}
		}
		for j := range cfg.Footer.Columns[i].Links {
			cfg.Footer.Columns[i].Links[j].URL = sanitizeLoadedURL(cfg.Footer.Columns[i].Links[j].URL, true)
		}
	}
	if cfg.Footer.BottomLinks == nil {
		cfg.Footer.BottomLinks = []HomeLink{}
	}
	for i := range cfg.Footer.BottomLinks {
		cfg.Footer.BottomLinks[i].URL = sanitizeLoadedURL(cfg.Footer.BottomLinks[i].URL, true)
	}
}

func cleanHomeSections(in []HomeSection) ([]HomeSection, error) {
	defaults := make(map[string]HomeSection, len(defaultHomeSectionOrder))
	for _, s := range defaultHomeSectionOrder {
		defaults[s.ID] = s
	}

	seen := make(map[string]struct{}, len(defaultHomeSectionOrder))
	out := make([]HomeSection, 0, len(defaultHomeSectionOrder))
	for _, sec := range in {
		id := strings.ToLower(strings.TrimSpace(sec.ID))
		defSec, ok := defaults[id]
		if !ok {
			return nil, &UserError{"区块标识无效，可选值：hero、showcase、news、capabilities、spotlight、custom、footer"}
		}
		if _, dup := seen[id]; dup {
			continue
		}
		label, err := cleanSingleLine(sec.Label, MaxHomeLabelLen, "区块名称")
		if err != nil {
			return nil, err
		}
		if label == "" {
			label = defSec.Label
		}
		seen[id] = struct{}{}
		out = append(out, HomeSection{
			ID:      id,
			Label:   label,
			Visible: sec.Visible,
		})
	}
	for _, defSec := range defaultHomeSectionOrder {
		if _, ok := seen[defSec.ID]; !ok {
			out = append(out, defSec)
		}
	}
	return out, nil
}

func normalizeSectionsPermissive(in []HomeSection) []HomeSection {
	defaults := make(map[string]HomeSection, len(defaultHomeSectionOrder))
	for _, s := range defaultHomeSectionOrder {
		defaults[s.ID] = s
	}

	seen := make(map[string]struct{}, len(defaultHomeSectionOrder))
	out := make([]HomeSection, 0, len(defaultHomeSectionOrder))
	for _, sec := range in {
		id := strings.ToLower(strings.TrimSpace(sec.ID))
		defSec, ok := defaults[id]
		if !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		label := strings.TrimSpace(sec.Label)
		if label == "" {
			label = defSec.Label
		}
		seen[id] = struct{}{}
		out = append(out, HomeSection{
			ID:      id,
			Label:   label,
			Visible: sec.Visible,
		})
	}
	for _, defSec := range defaultHomeSectionOrder {
		if _, ok := seen[defSec.ID]; !ok {
			out = append(out, defSec)
		}
	}
	return out
}

func cleanHomeLinks(in []HomeLink, maxCount int, field string) ([]HomeLink, error) {
	if len(in) > maxCount {
		return nil, &UserError{fmt.Sprintf("%s最多配置 %d 条", field, maxCount)}
	}
	out := make([]HomeLink, 0, len(in))
	for _, lk := range in {
		label, err := cleanSingleLine(lk.Label, MaxHomeLabelLen, field+"名称")
		if err != nil {
			return nil, err
		}
		u, err := validateHomeURL(lk.URL, field+"地址", true)
		if err != nil {
			return nil, err
		}
		out = append(out, HomeLink{Label: label, URL: u})
	}
	return out, nil
}

func validateHexColor(raw, field string) (string, error) {
	c := strings.ToLower(strings.TrimSpace(raw))
	if len(c) != 4 && len(c) != 7 {
		return "", &UserError{fmt.Sprintf("%s格式无效，仅支持 3 位或 6 位十六进制色值（如 #faf9f5）", field)}
	}
	if c[0] != '#' {
		return "", &UserError{fmt.Sprintf("%s格式无效，仅支持 3 位或 6 位十六进制色值（如 #faf9f5）", field)}
	}
	for i := 1; i < len(c); i++ {
		ch := c[i]
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return "", &UserError{fmt.Sprintf("%s格式无效，仅支持 3 位或 6 位十六进制色值（如 #faf9f5）", field)}
		}
	}
	return c, nil
}

// validateHomeURL 校验首页配置中的所有链接，允许空串、#锚点（可选）、站内相对路径与 http/https 网址。
func validateHomeURL(raw, field string, allowAnchor bool) (string, error) {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "", nil
	}
	if len(u) > MaxHomeURLLen {
		return "", &UserError{fmt.Sprintf("%s最多 %d 个字符", field, MaxHomeURLLen)}
	}
	for _, r := range u {
		if r <= 0x20 || r == 0x7f {
			return "", &UserError{fmt.Sprintf("%s不能包含空白或控制字符", field)}
		}
	}
	if strings.HasPrefix(u, "#") {
		if !allowAnchor {
			return "", &UserError{fmt.Sprintf("%s仅支持站内路径（以 / 开头）或 http/https 网址", field)}
		}
		if len(u) < 2 || strings.ContainsAny(u, `\"'<>`) {
			return "", &UserError{fmt.Sprintf("%s锚点格式无效", field)}
		}
		return u, nil
	}
	if strings.HasPrefix(u, "/") {
		if strings.HasPrefix(u, "//") || strings.Contains(u, "\\") {
			return "", &UserError{fmt.Sprintf("%s站内路径无效，不能以 // 开头或包含反斜杠", field)}
		}
		return u, nil
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", &UserError{fmt.Sprintf("%s格式无效", field)}
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" {
		if allowAnchor {
			return "", &UserError{fmt.Sprintf("%s仅支持站内路径（以 / 开头）、#锚点或 http/https 网址", field)}
		}
		return "", &UserError{fmt.Sprintf("%s仅支持站内路径（以 / 开头）或 http/https 网址", field)}
	}
	return u, nil
}

func sanitizeLoadedURL(raw string, allowAnchor bool) string {
	cleaned, err := validateHomeURL(raw, "", allowAnchor)
	if err != nil {
		return ""
	}
	return cleaned
}

func cleanSingleLine(raw string, maxRunes int, field string) (string, error) {
	s := strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(s) > maxRunes {
		return "", &UserError{fmt.Sprintf("%s最多 %d 个字符", field, maxRunes)}
	}
	return s, nil
}

func cleanMultiLine(raw string, maxRunes int, field string) (string, error) {
	s := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	s = strings.ReplaceAll(s, "\r", "\n")
	if utf8.RuneCountInString(s) > maxRunes {
		return "", &UserError{fmt.Sprintf("%s最多 %d 个字符", field, maxRunes)}
	}
	return s, nil
}

func cleanItemID(raw, fallback string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback, nil
	}
	if utf8.RuneCountInString(s) > MaxHomeIDLen {
		return "", &UserError{fmt.Sprintf("条目标识最多 %d 个字符", MaxHomeIDLen)}
	}
	return s, nil
}

func cleanOptionalIcon(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", nil
	}
	if len(s) > MaxHomeIconLen {
		return "", &UserError{fmt.Sprintf("图标名称最多 %d 个字符", MaxHomeIconLen)}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return "", &UserError{"图标名称只能包含小写字母、数字和连字符"}
		}
	}
	return s, nil
}

func validShowcaseTheme(theme string) bool {
	switch theme {
	case "dark", "olive", "mist", "sand":
		return true
	default:
		return false
	}
}

func validCapabilityIcon(icon string) bool {
	switch icon {
	case "brain", "sparkle", "clock", "note", "message", "globe", "code", "mask", "star":
		return true
	default:
		return false
	}
}

func validStageType(stage string) bool {
	switch stage {
	case "chat-demo", "arena-preview", "image", "editorial":
		return true
	default:
		return false
	}
}

func validCustomBlockTheme(theme string) bool {
	switch theme {
	case "light", "card", "dark":
		return true
	default:
		return false
	}
}
