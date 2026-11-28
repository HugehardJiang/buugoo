<script setup lang="ts">
// 管理后台 → 官网首页：高自由度编排公开首页的布局、主题配色、焦点竖卡、动态、能力书架、交互舞台、自定义区与页脚。
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError } from '../api'
import { toast } from '../toast'
import Modal from './Modal.vue'

type HomeHeroHeight = 'full' | 'compact'
type HomeFontStyle = 'sans' | 'serif'
type HomeSectionId = 'hero' | 'showcase' | 'news' | 'capabilities' | 'spotlight' | 'custom' | 'footer'
type HomeShowcaseTheme = 'dark' | 'olive' | 'mist' | 'sand'
type HomeCapIcon = 'brain' | 'sparkle' | 'clock' | 'note' | 'message' | 'globe' | 'code' | 'mask' | 'star'
type HomeStageType = 'chat-demo' | 'arena-preview' | 'image' | 'editorial'
type HomeCustomTheme = 'light' | 'card' | 'dark'
type EditorTab = 'layout' | 'header-hero' | 'showcase' | 'news-caps' | 'spotlight-custom' | 'footer-json'

interface HomeLink {
  label: string
  url: string
}

interface HomeFooterLink {
  label: string
  url: string
  icon: string
}

interface HomeTheme {
  canvas_color: string
  accent_color: string
  hero_glow_primary: string
  hero_glow_secondary: string
  hero_height: HomeHeroHeight
  header_blur: boolean
  font_style: HomeFontStyle
  card_radius: number
}

interface HomeHeader {
  brand_name: string
  nav_links: HomeLink[]
  primary_cta_label: string
  primary_cta_url: string
  secondary_cta_label: string
  secondary_cta_url: string
}

interface HomeSectionItem {
  id: HomeSectionId
  label: string
  visible: boolean
}

interface HomeHero {
  title: string
  subtitle: string
  primary_label: string
  primary_url: string
  secondary_label: string
  secondary_url: string
  bg_image_url: string
  bg_overlay: number
}

interface HomeShowcaseCard {
  id: string
  title: string
  subtitle: string
  theme: HomeShowcaseTheme
  image_url: string
  primary_label: string
  primary_url: string
  secondary_label: string
  secondary_url: string
}

interface HomeShowcase {
  cards: HomeShowcaseCard[]
}

interface HomeNews {
  title: string
  view_all_label: string
  view_all_url: string
  category: string
  limit: number
}

interface HomeCapabilityItem {
  id: string
  icon: HomeCapIcon
  badge: string
  title: string
  description: string
  links_heading: string
  links: HomeLink[]
  learn_more_label: string
  learn_more_url: string
}

interface HomeCapabilities {
  title: string
  subtitle: string
  cta_label: string
  cta_url: string
  items: HomeCapabilityItem[]
}

interface HomeSpotlightItem {
  id: string
  title: string
  description: string
  stage_type: HomeStageType
  image_url: string
  stage_title: string
  stage_body: string
  stage_cta_label: string
  stage_cta_url: string
}

interface HomeSpotlight {
  title: string
  subtitle: string
  cta_label: string
  cta_url: string
  items: HomeSpotlightItem[]
}

interface HomeCustomBlock {
  id: string
  eyebrow: string
  title: string
  subtitle: string
  body_md: string
  image_url: string
  theme: HomeCustomTheme
  cta_label: string
  cta_url: string
  secondary_label: string
  secondary_url: string
}

interface HomeFooterColumn {
  title: string
  links: HomeFooterLink[]
}

interface HomeFooter {
  show_cta_bar: boolean
  cta_heading: string
  cta_note: string
  cta_placeholder: string
  cta_button_label: string
  cta_button_url: string
  mission: string
  columns: HomeFooterColumn[]
  bottom_note: string
  bottom_links: HomeLink[]
}

interface CmsHomeConfig {
  theme: HomeTheme
  header: HomeHeader
  sections: HomeSectionItem[]
  hero: HomeHero
  showcase: HomeShowcase
  news: HomeNews
  capabilities: HomeCapabilities
  spotlight: HomeSpotlight
  custom_blocks: HomeCustomBlock[]
  footer: HomeFooter
}

interface HomeConfigResp {
  config: CmsHomeConfig
  is_default?: boolean
}

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

const TABS: { key: EditorTab; label: string }[] = [
  { key: 'layout', label: '布局与主题' },
  { key: 'header-hero', label: '顶栏与主视觉' },
  { key: 'showcase', label: '焦点竖卡' },
  { key: 'news-caps', label: '动态与能力书架' },
  { key: 'spotlight-custom', label: '交互舞台与自定义区' },
  { key: 'footer-json', label: '页脚与 JSON' },
]

const BUUGOO_COLORS = {
  canvas_color: '#faf9f5',
  accent_color: '#5b7a4a',
  hero_glow_primary: '#8fa67f',
  hero_glow_secondary: '#bcd1ca',
} as const

const BUILTIN_SECTIONS: HomeSectionItem[] = [
  { id: 'hero', label: '首屏主视觉', visible: true },
  { id: 'showcase', label: '焦点竖卡展示', visible: true },
  { id: 'news', label: '最新动态', visible: true },
  { id: 'capabilities', label: '能力与生态书架', visible: true },
  { id: 'spotlight', label: '交互舞台', visible: true },
  { id: 'custom', label: '自定义扩展区', visible: true },
  { id: 'footer', label: '页脚', visible: true },
]

const SECTION_HINTS: Record<HomeSectionId, string> = {
  hero: '居中大标题、背景氛围光晕与首屏行动按钮',
  showcase: '2:3 竖版焦点展示卡横向轮播',
  news: '不对称双栏最新动态与精选文章',
  capabilities: '多列横向图标能力与生态书架',
  spotlight: '左侧手风琴目录驱动右侧大圆角交互舞台',
  custom: '管理员自由编排的 Markdown 扩展内容区',
  footer: '底部行动条、分组链接与隐私说明',
}

const SHOWCASE_THEMES: { value: HomeShowcaseTheme; label: string; swatch: string }[] = [
  { value: 'dark', label: '深碳黑', swatch: '#17181b' },
  { value: 'olive', label: '墨橄榄', swatch: '#1b2319' },
  { value: 'mist', label: '雾青', swatch: '#bcd1ca' },
  { value: 'sand', label: '暖砂', swatch: '#e3dacc' },
]

const CAP_ICONS: { value: HomeCapIcon; label: string }[] = [
  { value: 'brain', label: '记忆大脑（brain）' },
  { value: 'sparkle', label: '灵动星光（sparkle）' },
  { value: 'clock', label: '定时提醒（clock）' },
  { value: 'note', label: '备忘笔记（note）' },
  { value: 'message', label: '对话气泡（message）' },
  { value: 'globe', label: '开放生态（globe）' },
  { value: 'code', label: '代码工具（code）' },
  { value: 'mask', label: '角色性格（mask）' },
  { value: 'star', label: '精选星标（star）' },
]

const STAGE_TYPES: { value: HomeStageType; label: string }[] = [
  { value: 'chat-demo', label: '对话演示（chat-demo）' },
  { value: 'arena-preview', label: '盲测舞台（arena-preview）' },
  { value: 'image', label: '自定义配图（image）' },
  { value: 'editorial', label: '图文观点卡（editorial）' },
]

const CUSTOM_THEMES: { value: HomeCustomTheme; label: string }[] = [
  { value: 'light', label: '浅色画布（light）' },
  { value: 'card', label: '暖米卡片（card）' },
  { value: 'dark', label: '深色主题（dark）' },
]

const PRESET_NEWS_CATEGORIES = ['动态', '产品', '技术', '指南', '更新日志']
const HEX_RE = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/

const activeTab = ref<EditorTab>('layout')
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const resetting = ref(false)
const isDefault = ref(true)
const error = ref('')
const savedSnapshot = ref('')

const confirmResetOpen = ref(false)
const jsonModalOpen = ref(false)
const jsonImportText = ref('')
const jsonError = ref('')
const collapsed = reactive<Record<string, boolean>>({})

function createEmptyConfig(): CmsHomeConfig {
  return {
    theme: {
      canvas_color: BUUGOO_COLORS.canvas_color,
      accent_color: BUUGOO_COLORS.accent_color,
      hero_glow_primary: BUUGOO_COLORS.hero_glow_primary,
      hero_glow_secondary: BUUGOO_COLORS.hero_glow_secondary,
      hero_height: 'full',
      header_blur: true,
      font_style: 'sans',
      card_radius: 24,
    },
    header: {
      brand_name: '',
      nav_links: [],
      primary_cta_label: '',
      primary_cta_url: '',
      secondary_cta_label: '',
      secondary_cta_url: '',
    },
    sections: BUILTIN_SECTIONS.map((s) => ({ ...s })),
    hero: {
      title: '',
      subtitle: '',
      primary_label: '',
      primary_url: '',
      secondary_label: '',
      secondary_url: '',
      bg_image_url: '',
      bg_overlay: 30,
    },
    showcase: {
      cards: [],
    },
    news: {
      title: '最新动态',
      view_all_label: '查看全部文章',
      view_all_url: '/posts',
      category: '',
      limit: 5,
    },
    capabilities: {
      title: '',
      subtitle: '',
      cta_label: '',
      cta_url: '',
      items: [],
    },
    spotlight: {
      title: '',
      subtitle: '',
      cta_label: '',
      cta_url: '',
      items: [],
    },
    custom_blocks: [],
    footer: {
      show_cta_bar: false,
      cta_heading: '',
      cta_note: '',
      cta_placeholder: '',
      cta_button_label: '',
      cta_button_url: '',
      mission: '',
      columns: [],
      bottom_note: '',
      bottom_links: [],
    },
  }
}

const form = ref<CmsHomeConfig>(createEmptyConfig())

function makeId(prefix: string): string {
  const rand = Math.random().toString(36).slice(2, 7)
  return `${prefix}-${Date.now().toString(36)}-${rand}`
}

function asRecord(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {}
}

function asString(v: unknown, fallback = ''): string {
  return typeof v === 'string' ? v : fallback
}

function asBool(v: unknown, fallback: boolean): boolean {
  return typeof v === 'boolean' ? v : fallback
}

function clampInt(raw: unknown, min: number, max: number, fallback: number): number {
  const s = String(raw ?? '').trim()
  if (!s) return fallback
  const n = Number(s)
  if (!Number.isFinite(n)) return fallback
  return Math.min(max, Math.max(min, Math.round(n)))
}

function asEnum<T extends string>(v: unknown, allowed: readonly T[], fallback: T): T {
  return typeof v === 'string' && (allowed as readonly string[]).includes(v) ? (v as T) : fallback
}

function normalizeLinks(raw: unknown): HomeLink[] {
  if (!Array.isArray(raw)) return []
  return raw.map((item) => {
    const r = asRecord(item)
    return {
      label: asString(r.label),
      url: asString(r.url),
    }
  })
}

function normalizeFooterLinks(raw: unknown): HomeFooterLink[] {
  if (!Array.isArray(raw)) return []
  return raw.map((item) => {
    const r = asRecord(item)
    return {
      label: asString(r.label),
      url: asString(r.url),
      icon: asString(r.icon),
    }
  })
}

function normalizeSections(raw: unknown): HomeSectionItem[] {
  const out: HomeSectionItem[] = []
  const seen = new Set<HomeSectionId>()
  const builtinMap = new Map(BUILTIN_SECTIONS.map((s) => [s.id, s]))

  if (Array.isArray(raw)) {
    for (const item of raw) {
      const r = asRecord(item)
      const id = asString(r.id) as HomeSectionId
      const def = builtinMap.get(id)
      if (!def || seen.has(id)) continue
      seen.add(id)
      out.push({
        id,
        label: asString(r.label).trim() || def.label,
        visible: asBool(r.visible, def.visible),
      })
    }
  }
  for (const def of BUILTIN_SECTIONS) {
    if (!seen.has(def.id)) {
      out.push({ ...def })
    }
  }
  return out
}

function normalizeConfig(raw: unknown): CmsHomeConfig {
  const root = asRecord(raw)
  const theme = asRecord(root.theme)
  const header = asRecord(root.header)
  const hero = asRecord(root.hero)
  const showcase = asRecord(root.showcase)
  const news = asRecord(root.news)
  const capabilities = asRecord(root.capabilities)
  const spotlight = asRecord(root.spotlight)
  const footer = asRecord(root.footer)

  const cards = Array.isArray(showcase.cards)
    ? showcase.cards.map((item, idx) => {
        const r = asRecord(item)
        return {
          id: asString(r.id).trim() || `card-${idx + 1}`,
          title: asString(r.title),
          subtitle: asString(r.subtitle),
          theme: asEnum<HomeShowcaseTheme>(r.theme, ['dark', 'olive', 'mist', 'sand'], 'dark'),
          image_url: asString(r.image_url),
          primary_label: asString(r.primary_label),
          primary_url: asString(r.primary_url),
          secondary_label: asString(r.secondary_label),
          secondary_url: asString(r.secondary_url),
        }
      })
    : []

  const capItems = Array.isArray(capabilities.items)
    ? capabilities.items.map((item, idx) => {
        const r = asRecord(item)
        return {
          id: asString(r.id).trim() || `cap-${idx + 1}`,
          icon: asEnum<HomeCapIcon>(
            r.icon,
            ['brain', 'sparkle', 'clock', 'note', 'message', 'globe', 'code', 'mask', 'star'],
            'sparkle',
          ),
          badge: asString(r.badge),
          title: asString(r.title),
          description: asString(r.description),
          links_heading: asString(r.links_heading),
          links: normalizeLinks(r.links),
          learn_more_label: asString(r.learn_more_label),
          learn_more_url: asString(r.learn_more_url),
        }
      })
    : []

  const spotItems = Array.isArray(spotlight.items)
    ? spotlight.items.map((item, idx) => {
        const r = asRecord(item)
        return {
          id: asString(r.id).trim() || `spot-${idx + 1}`,
          title: asString(r.title),
          description: asString(r.description),
          stage_type: asEnum<HomeStageType>(r.stage_type, ['chat-demo', 'arena-preview', 'image', 'editorial'], 'editorial'),
          image_url: asString(r.image_url),
          stage_title: asString(r.stage_title),
          stage_body: asString(r.stage_body),
          stage_cta_label: asString(r.stage_cta_label),
          stage_cta_url: asString(r.stage_cta_url),
        }
      })
    : []

  const customBlocks = Array.isArray(root.custom_blocks)
    ? root.custom_blocks.map((item, idx) => {
        const r = asRecord(item)
        return {
          id: asString(r.id).trim() || `custom-${idx + 1}`,
          eyebrow: asString(r.eyebrow),
          title: asString(r.title),
          subtitle: asString(r.subtitle),
          body_md: asString(r.body_md),
          image_url: asString(r.image_url),
          theme: asEnum<HomeCustomTheme>(r.theme, ['light', 'card', 'dark'], 'card'),
          cta_label: asString(r.cta_label),
          cta_url: asString(r.cta_url),
          secondary_label: asString(r.secondary_label),
          secondary_url: asString(r.secondary_url),
        }
      })
    : []

  const footerColumns = Array.isArray(footer.columns)
    ? footer.columns.map((item) => {
        const r = asRecord(item)
        return {
          title: asString(r.title),
          links: normalizeFooterLinks(r.links),
        }
      })
    : []

  return {
    theme: {
      canvas_color: asString(theme.canvas_color, BUUGOO_COLORS.canvas_color).trim() || BUUGOO_COLORS.canvas_color,
      accent_color: asString(theme.accent_color, BUUGOO_COLORS.accent_color).trim() || BUUGOO_COLORS.accent_color,
      hero_glow_primary: asString(theme.hero_glow_primary, BUUGOO_COLORS.hero_glow_primary).trim() || BUUGOO_COLORS.hero_glow_primary,
      hero_glow_secondary: asString(theme.hero_glow_secondary, BUUGOO_COLORS.hero_glow_secondary).trim() || BUUGOO_COLORS.hero_glow_secondary,
      hero_height: asEnum<HomeHeroHeight>(theme.hero_height, ['full', 'compact'], 'full'),
      header_blur: asBool(theme.header_blur, true),
      font_style: asEnum<HomeFontStyle>(theme.font_style, ['sans', 'serif'], 'sans'),
      card_radius: clampInt(theme.card_radius, 16, 32, 24),
    },
    header: {
      brand_name: asString(header.brand_name),
      nav_links: normalizeLinks(header.nav_links),
      primary_cta_label: asString(header.primary_cta_label),
      primary_cta_url: asString(header.primary_cta_url),
      secondary_cta_label: asString(header.secondary_cta_label),
      secondary_cta_url: asString(header.secondary_cta_url),
    },
    sections: normalizeSections(root.sections),
    hero: {
      title: asString(hero.title),
      subtitle: asString(hero.subtitle),
      primary_label: asString(hero.primary_label),
      primary_url: asString(hero.primary_url),
      secondary_label: asString(hero.secondary_label),
      secondary_url: asString(hero.secondary_url),
      bg_image_url: asString(hero.bg_image_url),
      bg_overlay: clampInt(hero.bg_overlay, 0, 80, 30),
    },
    showcase: { cards },
    news: {
      title: asString(news.title),
      view_all_label: asString(news.view_all_label),
      view_all_url: asString(news.view_all_url),
      category: asString(news.category),
      limit: clampInt(news.limit, 3, 8, 5),
    },
    capabilities: {
      title: asString(capabilities.title),
      subtitle: asString(capabilities.subtitle),
      cta_label: asString(capabilities.cta_label),
      cta_url: asString(capabilities.cta_url),
      items: capItems,
    },
    spotlight: {
      title: asString(spotlight.title),
      subtitle: asString(spotlight.subtitle),
      cta_label: asString(spotlight.cta_label),
      cta_url: asString(spotlight.cta_url),
      items: spotItems,
    },
    custom_blocks: customBlocks,
    footer: {
      show_cta_bar: asBool(footer.show_cta_bar, false),
      cta_heading: asString(footer.cta_heading),
      cta_note: asString(footer.cta_note),
      cta_placeholder: asString(footer.cta_placeholder),
      cta_button_label: asString(footer.cta_button_label),
      cta_button_url: asString(footer.cta_button_url),
      mission: asString(footer.mission),
      columns: footerColumns,
      bottom_note: asString(footer.bottom_note),
      bottom_links: normalizeLinks(footer.bottom_links),
    },
  }
}

function buildPayload(source: CmsHomeConfig = form.value): CmsHomeConfig {
  return {
    theme: {
      canvas_color: source.theme.canvas_color.trim(),
      accent_color: source.theme.accent_color.trim(),
      hero_glow_primary: source.theme.hero_glow_primary.trim(),
      hero_glow_secondary: source.theme.hero_glow_secondary.trim(),
      hero_height: source.theme.hero_height,
      header_blur: !!source.theme.header_blur,
      font_style: source.theme.font_style,
      card_radius: clampInt(source.theme.card_radius, 16, 32, 24),
    },
    header: {
      brand_name: source.header.brand_name.trim(),
      nav_links: source.header.nav_links.map((l) => ({ label: l.label.trim(), url: l.url.trim() })),
      primary_cta_label: source.header.primary_cta_label.trim(),
      primary_cta_url: source.header.primary_cta_url.trim(),
      secondary_cta_label: source.header.secondary_cta_label.trim(),
      secondary_cta_url: source.header.secondary_cta_url.trim(),
    },
    sections: normalizeSections(source.sections),
    hero: {
      title: source.hero.title,
      subtitle: source.hero.subtitle,
      primary_label: source.hero.primary_label.trim(),
      primary_url: source.hero.primary_url.trim(),
      secondary_label: source.hero.secondary_label.trim(),
      secondary_url: source.hero.secondary_url.trim(),
      bg_image_url: source.hero.bg_image_url.trim(),
      bg_overlay: clampInt(source.hero.bg_overlay, 0, 80, 30),
    },
    showcase: {
      cards: source.showcase.cards.map((c, idx) => ({
        id: c.id.trim() || `card-${idx + 1}`,
        title: c.title.trim(),
        subtitle: c.subtitle.trim(),
        theme: c.theme,
        image_url: c.image_url.trim(),
        primary_label: c.primary_label.trim(),
        primary_url: c.primary_url.trim(),
        secondary_label: c.secondary_label.trim(),
        secondary_url: c.secondary_url.trim(),
      })),
    },
    news: {
      title: source.news.title.trim(),
      view_all_label: source.news.view_all_label.trim(),
      view_all_url: source.news.view_all_url.trim(),
      category: source.news.category.trim(),
      limit: clampInt(source.news.limit, 3, 8, 5),
    },
    capabilities: {
      title: source.capabilities.title.trim(),
      subtitle: source.capabilities.subtitle.trim(),
      cta_label: source.capabilities.cta_label.trim(),
      cta_url: source.capabilities.cta_url.trim(),
      items: source.capabilities.items.map((item, idx) => ({
        id: item.id.trim() || `cap-${idx + 1}`,
        icon: item.icon,
        badge: item.badge.trim(),
        title: item.title.trim(),
        description: item.description.trim(),
        links_heading: item.links_heading.trim(),
        links: item.links.map((l) => ({ label: l.label.trim(), url: l.url.trim() })),
        learn_more_label: item.learn_more_label.trim(),
        learn_more_url: item.learn_more_url.trim(),
      })),
    },
    spotlight: {
      title: source.spotlight.title.trim(),
      subtitle: source.spotlight.subtitle.trim(),
      cta_label: source.spotlight.cta_label.trim(),
      cta_url: source.spotlight.cta_url.trim(),
      items: source.spotlight.items.map((item, idx) => ({
        id: item.id.trim() || `spot-${idx + 1}`,
        title: item.title.trim(),
        description: item.description.trim(),
        stage_type: item.stage_type,
        image_url: item.image_url.trim(),
        stage_title: item.stage_title.trim(),
        stage_body: item.stage_body,
        stage_cta_label: item.stage_cta_label.trim(),
        stage_cta_url: item.stage_cta_url.trim(),
      })),
    },
    custom_blocks: source.custom_blocks.map((b, idx) => ({
      id: b.id.trim() || `custom-${idx + 1}`,
      eyebrow: b.eyebrow.trim(),
      title: b.title.trim(),
      subtitle: b.subtitle.trim(),
      body_md: b.body_md,
      image_url: b.image_url.trim(),
      theme: b.theme,
      cta_label: b.cta_label.trim(),
      cta_url: b.cta_url.trim(),
      secondary_label: b.secondary_label.trim(),
      secondary_url: b.secondary_url.trim(),
    })),
    footer: {
      show_cta_bar: !!source.footer.show_cta_bar,
      cta_heading: source.footer.cta_heading.trim(),
      cta_note: source.footer.cta_note.trim(),
      cta_placeholder: source.footer.cta_placeholder.trim(),
      cta_button_label: source.footer.cta_button_label.trim(),
      cta_button_url: source.footer.cta_button_url.trim(),
      mission: source.footer.mission,
      columns: source.footer.columns.map((col) => ({
        title: col.title.trim(),
        links: col.links.map((l) => ({
          label: l.label.trim(),
          url: l.url.trim(),
          icon: l.icon.trim(),
        })),
      })),
      bottom_note: source.footer.bottom_note.trim(),
      bottom_links: source.footer.bottom_links.map((l) => ({ label: l.label.trim(), url: l.url.trim() })),
    },
  }
}

function isHexColor(v: string): boolean {
  return HEX_RE.test(v.trim())
}

function toPickerHex(v: string, fallback: string): string {
  const s = v.trim()
  if (/^#[0-9a-fA-F]{6}$/.test(s)) return s.toLowerCase()
  if (/^#[0-9a-fA-F]{3}$/.test(s)) {
    return `#${s[1]}${s[1]}${s[2]}${s[2]}${s[3]}${s[3]}`.toLowerCase()
  }
  return fallback
}

function isSafeUrl(raw: string): boolean {
  const u = raw.trim()
  if (!u) return true
  if (u.startsWith('#')) return true
  if (u.startsWith('/') && !u.startsWith('//')) return true
  return /^https?:\/\//i.test(u)
}

function validateConfig(cfg: CmsHomeConfig): string {
  const colorChecks: [string, string][] = [
    ['画布底色', cfg.theme.canvas_color],
    ['主题点缀色', cfg.theme.accent_color],
    ['Hero 主光晕', cfg.theme.hero_glow_primary],
    ['Hero 辅光晕', cfg.theme.hero_glow_secondary],
  ]
  for (const [label, val] of colorChecks) {
    if (!isHexColor(val)) {
      return `${label}格式不正确，请使用 3 位或 6 位十六进制色值（例如 #5b7a4a）`
    }
  }

  const urlChecks: [string, string][] = [
    ['顶栏主按钮链接', cfg.header.primary_cta_url],
    ['顶栏次按钮链接', cfg.header.secondary_cta_url],
    ['主视觉主按钮链接', cfg.hero.primary_url],
    ['主视觉次按钮链接', cfg.hero.secondary_url],
    ['主视觉背景图链接', cfg.hero.bg_image_url],
    ['动态区查看全部链接', cfg.news.view_all_url],
    ['能力书架右上角按钮链接', cfg.capabilities.cta_url],
    ['交互舞台右上角按钮链接', cfg.spotlight.cta_url],
    ['页脚行动条按钮链接', cfg.footer.cta_button_url],
  ]
  cfg.header.nav_links.forEach((l, i) => urlChecks.push([`顶栏第 ${i + 1} 条导航链接`, l.url]))
  cfg.showcase.cards.forEach((c, i) => {
    urlChecks.push([`焦点竖卡第 ${i + 1} 张封面图链接`, c.image_url])
    urlChecks.push([`焦点竖卡第 ${i + 1} 张主按钮链接`, c.primary_url])
    urlChecks.push([`焦点竖卡第 ${i + 1} 张次按钮链接`, c.secondary_url])
  })
  cfg.capabilities.items.forEach((item, i) => {
    urlChecks.push([`能力卡片第 ${i + 1} 项了解更多链接`, item.learn_more_url])
    item.links.forEach((l, j) => urlChecks.push([`能力卡片第 ${i + 1} 项第 ${j + 1} 条直达链接`, l.url]))
  })
  cfg.spotlight.items.forEach((item, i) => {
    urlChecks.push([`交互舞台第 ${i + 1} 项配图链接`, item.image_url])
    urlChecks.push([`交互舞台第 ${i + 1} 项按钮链接`, item.stage_cta_url])
  })
  cfg.custom_blocks.forEach((b, i) => {
    urlChecks.push([`自定义区块第 ${i + 1} 项配图链接`, b.image_url])
    urlChecks.push([`自定义区块第 ${i + 1} 项主按钮链接`, b.cta_url])
    urlChecks.push([`自定义区块第 ${i + 1} 项次按钮链接`, b.secondary_url])
  })
  cfg.footer.columns.forEach((col, i) => {
    col.links.forEach((l, j) => urlChecks.push([`页脚第 ${i + 1} 栏第 ${j + 1} 条链接`, l.url]))
  })
  cfg.footer.bottom_links.forEach((l, i) => urlChecks.push([`页脚底部第 ${i + 1} 条链接`, l.url]))

  for (const [label, url] of urlChecks) {
    if (!isSafeUrl(url)) {
      return `${label}格式不受支持，只允许 https://、http://、/ 开头的站内路径或 # 锚点`
    }
  }
  return ''
}

const isDirty = computed(() => loaded.value && JSON.stringify(buildPayload()) !== savedSnapshot.value)
const exportJsonText = computed(() => JSON.stringify(buildPayload(), null, 2))
const visibleSectionCount = computed(() => form.value.sections.filter((s) => s.visible).length)

function applyConfig(cfg: CmsHomeConfig, defaultFlag?: boolean) {
  const normalized = normalizeConfig(cfg)
  form.value = normalized
  savedSnapshot.value = JSON.stringify(buildPayload(normalized))
  if (typeof defaultFlag === 'boolean') isDefault.value = defaultFlag
  loaded.value = true
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const resp = await props.call<HomeConfigResp>('GET', '/cms/home')
    applyConfig(resp.config, resp.is_default ?? false)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载官网首页配置失败'
  } finally {
    loading.value = false
  }
}

async function save() {
  if (saving.value || !loaded.value) return
  error.value = ''
  const payload = buildPayload()
  const invalid = validateConfig(payload)
  if (invalid) {
    error.value = invalid
    toast(invalid)
    return
  }
  saving.value = true
  try {
    const resp = await props.call<HomeConfigResp>('PUT', '/cms/home', payload)
    applyConfig(resp.config ?? payload, false)
    toast('官网首页配置已保存并立即生效')
  } catch (e) {
    const msg = e instanceof ApiError ? e.message : '保存失败'
    error.value = msg
    toast(msg)
  } finally {
    saving.value = false
  }
}

async function confirmReset() {
  if (resetting.value) return
  resetting.value = true
  error.value = ''
  try {
    const resp = await props.call<HomeConfigResp>('DELETE', '/cms/home')
    confirmResetOpen.value = false
    applyConfig(resp.config, true)
    toast('已恢复默认官网首页配置')
  } catch (e) {
    const msg = e instanceof ApiError ? e.message : '恢复默认失败'
    error.value = msg
    toast(msg)
  } finally {
    resetting.value = false
  }
}

function resetBrandColors() {
  form.value.theme.canvas_color = BUUGOO_COLORS.canvas_color
  form.value.theme.accent_color = BUUGOO_COLORS.accent_color
  form.value.theme.hero_glow_primary = BUUGOO_COLORS.hero_glow_primary
  form.value.theme.hero_glow_secondary = BUUGOO_COLORS.hero_glow_secondary
  toast('已填充布谷原色，记得点击保存修改')
}

function moveItem<T>(list: T[], index: number, delta: -1 | 1) {
  const target = index + delta
  if (target < 0 || target >= list.length) return
  const [picked] = list.splice(index, 1)
  list.splice(target, 0, picked)
}

function removeItem<T>(list: T[], index: number) {
  list.splice(index, 1)
}

function toggleCollapsed(key: string) {
  collapsed[key] = !collapsed[key]
}

function addNavLink() {
  form.value.header.nav_links.push({ label: '', url: '' })
}

function addShowcaseCard() {
  const id = makeId('card')
  form.value.showcase.cards.push({
    id,
    title: '',
    subtitle: '',
    theme: 'dark',
    image_url: '',
    primary_label: '立即体验',
    primary_url: '/login',
    secondary_label: '',
    secondary_url: '',
  })
  collapsed[`card:${id}`] = false
}

function addCapabilityItem() {
  const id = makeId('cap')
  form.value.capabilities.items.push({
    id,
    icon: 'sparkle',
    badge: '',
    title: '',
    description: '',
    links_heading: '快速入口',
    links: [{ label: '', url: '' }],
    learn_more_label: '了解更多',
    learn_more_url: '/posts',
  })
  collapsed[`cap:${id}`] = false
}

function addSpotlightItem() {
  const id = makeId('spot')
  form.value.spotlight.items.push({
    id,
    title: '',
    description: '',
    stage_type: 'editorial',
    image_url: '',
    stage_title: '',
    stage_body: '',
    stage_cta_label: '',
    stage_cta_url: '',
  })
  collapsed[`spot:${id}`] = false
}

function addCustomBlock() {
  const id = makeId('custom')
  form.value.custom_blocks.push({
    id,
    eyebrow: '',
    title: '',
    subtitle: '',
    body_md: '',
    image_url: '',
    theme: 'card',
    cta_label: '',
    cta_url: '',
    secondary_label: '',
    secondary_url: '',
  })
  collapsed[`custom:${id}`] = false
}

function addFooterColumn() {
  form.value.footer.columns.push({
    title: '新分组',
    links: [{ label: '', url: '', icon: '' }],
  })
}

function addFooterBottomLink() {
  form.value.footer.bottom_links.push({ label: '', url: '' })
}

function openJsonModal() {
  jsonError.value = ''
  jsonImportText.value = ''
  jsonModalOpen.value = true
}

async function copyExportJson() {
  try {
    await navigator.clipboard.writeText(exportJsonText.value)
    toast('已复制当前首页配置 JSON')
  } catch {
    toast('复制失败，请手动选中文本框内容复制')
  }
}

function downloadExportJson() {
  const blob = new Blob([exportJsonText.value], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'buugoo-home-config.json'
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
  toast('已开始下载配置文件')
}

function parseImportJson(): CmsHomeConfig | null {
  jsonError.value = ''
  const rawText = jsonImportText.value.trim()
  if (!rawText) {
    jsonError.value = '请先粘贴要导入的 JSON 内容'
    return null
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(rawText)
  } catch {
    jsonError.value = 'JSON 语法解析失败，请检查括号、引号与逗号格式'
    return null
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    jsonError.value = '根节点必须是 JSON 对象'
    return null
  }
  const normalized = normalizeConfig(parsed)
  const payload = buildPayload(normalized)
  const invalid = validateConfig(payload)
  if (invalid) {
    jsonError.value = invalid
    return null
  }
  return payload
}

function importJsonOnly() {
  const parsed = parseImportJson()
  if (!parsed) return
  form.value = parsed
  jsonModalOpen.value = false
  toast('已导入 JSON 到表单，请检查后点击保存修改')
}

async function importJsonAndSave() {
  const parsed = parseImportJson()
  if (!parsed) return
  form.value = parsed
  jsonModalOpen.value = false
  await save()
}

onMounted(load)
</script>

<template>
  <section class="panel panel--inset home-cms">
    <div class="head">
      <div class="head__title">
        <h2>官网首页</h2>
        <span class="chip-meta status-chip">{{ isDefault ? '默认配置' : '已自定义' }}</span>
        <span v-if="isDirty" class="chip-note status-chip">有未保存修改</span>
      </div>
      <div class="head__btns">
        <a class="btn btn-sm btn-outline" href="/welcome" target="_blank" rel="noopener noreferrer">打开首页预览</a>
        <button type="button" class="btn btn-sm" :disabled="!loaded" @click="openJsonModal">导入 / 导出 JSON</button>
        <button type="button" class="btn btn-sm btn-danger-lite" :disabled="!loaded || resetting" @click="confirmResetOpen = true">恢复默认</button>
        <button type="button" class="btn btn-sm btn-primary" :disabled="!loaded || saving" @click="save">
          {{ saving ? '正在保存' : '保存修改' }}
        </button>
      </div>
    </div>

    <p class="muted intro">
      可视化编排公开首页（<code>/welcome</code>）的区块顺序、主题色彩、顶栏主视觉、焦点竖卡、能力书架、交互舞台与页脚。保存后立即生效，无需重启服务。
    </p>

    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>

    <div v-if="loading && !loaded" class="skeleton skel" aria-hidden="true" />

    <template v-else-if="loaded">
      <div class="subtabs" role="tablist" aria-label="官网首页配置分页">
        <button
          v-for="t in TABS"
          :key="t.key"
          type="button"
          role="tab"
          class="subtabs__item"
          :class="{ 'is-active': activeTab === t.key }"
          :aria-selected="activeTab === t.key"
          @click="activeTab = t.key"
        >
          {{ t.label }}
        </button>
      </div>

      <!-- 1. 布局与主题 -->
      <div v-show="activeTab === 'layout'" class="tab-pane">
        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>首页区块显隐与排序</h3>
              <p class="caption">已启用 {{ visibleSectionCount }} / {{ form.sections.length }} 个区块，按从上到下顺序渲染。</p>
            </div>
          </div>
          <div class="section-list">
            <div v-for="(sec, idx) in form.sections" :key="sec.id" class="section-row">
              <label class="section-row__check">
                <input v-model="sec.visible" type="checkbox" />
                <span class="section-row__name">{{ sec.label }}</span>
                <code class="section-row__id">{{ sec.id }}</code>
              </label>
              <span class="caption section-row__hint">{{ SECTION_HINTS[sec.id] }}</span>
              <div class="item-ops">
                <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === 0" @click="moveItem(form.sections, idx, -1)">上移</button>
                <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === form.sections.length - 1" @click="moveItem(form.sections, idx, 1)">下移</button>
              </div>
            </div>
          </div>
        </div>

        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>主题调色板</h3>
              <p class="caption">支持调色器或直接输入 3/6 位十六进制色值，调整首页画布、点缀与主视觉氛围光晕。</p>
            </div>
            <button type="button" class="btn btn-sm btn-outline" @click="resetBrandColors">重置为布谷原色</button>
          </div>
          <div class="form-row color-grid">
            <div class="field">
              <label class="field__label" for="home-color-canvas">画布底色</label>
              <div class="color-row">
                <input
                  type="color"
                  class="color-picker"
                  aria-label="选择画布底色"
                  :value="toPickerHex(form.theme.canvas_color, BUUGOO_COLORS.canvas_color)"
                  @input="form.theme.canvas_color = ($event.target as HTMLInputElement).value"
                />
                <input id="home-color-canvas" v-model="form.theme.canvas_color" class="text-input mono" maxlength="7" placeholder="#faf9f5" />
              </div>
            </div>
            <div class="field">
              <label class="field__label" for="home-color-accent">主题点缀色</label>
              <div class="color-row">
                <input
                  type="color"
                  class="color-picker"
                  aria-label="选择主题点缀色"
                  :value="toPickerHex(form.theme.accent_color, BUUGOO_COLORS.accent_color)"
                  @input="form.theme.accent_color = ($event.target as HTMLInputElement).value"
                />
                <input id="home-color-accent" v-model="form.theme.accent_color" class="text-input mono" maxlength="7" placeholder="#5b7a4a" />
              </div>
            </div>
            <div class="field">
              <label class="field__label" for="home-color-glow1">Hero 主光晕</label>
              <div class="color-row">
                <input
                  type="color"
                  class="color-picker"
                  aria-label="选择 Hero 主光晕颜色"
                  :value="toPickerHex(form.theme.hero_glow_primary, BUUGOO_COLORS.hero_glow_primary)"
                  @input="form.theme.hero_glow_primary = ($event.target as HTMLInputElement).value"
                />
                <input id="home-color-glow1" v-model="form.theme.hero_glow_primary" class="text-input mono" maxlength="7" placeholder="#8fa67f" />
              </div>
            </div>
            <div class="field">
              <label class="field__label" for="home-color-glow2">Hero 辅光晕</label>
              <div class="color-row">
                <input
                  type="color"
                  class="color-picker"
                  aria-label="选择 Hero 辅光晕颜色"
                  :value="toPickerHex(form.theme.hero_glow_secondary, BUUGOO_COLORS.hero_glow_secondary)"
                  @input="form.theme.hero_glow_secondary = ($event.target as HTMLInputElement).value"
                />
                <input id="home-color-glow2" v-model="form.theme.hero_glow_secondary" class="text-input mono" maxlength="7" placeholder="#bcd1ca" />
              </div>
            </div>
          </div>
        </div>

        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>视觉开关与比例</h3>
              <p class="caption">控制首屏高度、顶栏渐进模糊、标题字体气质与全站卡片圆角。</p>
            </div>
          </div>
          <div class="form-row">
            <div class="field">
              <span class="field__label">Hero 高度</span>
              <div class="type-switch" role="group" aria-label="Hero 高度">
                <button type="button" :class="{ 'is-active': form.theme.hero_height === 'full' }" @click="form.theme.hero_height = 'full'">满屏（full）</button>
                <button type="button" :class="{ 'is-active': form.theme.hero_height === 'compact' }" @click="form.theme.hero_height = 'compact'">紧凑（compact）</button>
              </div>
            </div>
            <div class="field">
              <span class="field__label">标题字体风格</span>
              <div class="type-switch" role="group" aria-label="标题字体风格">
                <button type="button" :class="{ 'is-active': form.theme.font_style === 'sans' }" @click="form.theme.font_style = 'sans'">现代无衬线（sans）</button>
                <button type="button" :class="{ 'is-active': form.theme.font_style === 'serif' }" @click="form.theme.font_style = 'serif'">优雅衬线（serif）</button>
              </div>
            </div>
          </div>

          <div class="form-row">
            <div class="field">
              <label class="field__label" for="home-card-radius">卡片圆角（16–32 px）</label>
              <div class="range-row">
                <input
                  v-model.number="form.theme.card_radius"
                  class="range-slider"
                  type="range"
                  min="16"
                  max="32"
                  step="1"
                  aria-label="卡片圆角滑块"
                />
                <input
                  id="home-card-radius"
                  v-model="form.theme.card_radius"
                  class="text-input range-num"
                  type="number"
                  min="16"
                  max="32"
                  step="1"
                />
                <span class="caption">px</span>
              </div>
            </div>
            <div class="field">
              <span class="field__label">顶栏视觉效果</span>
              <label class="check-row check-box-inline">
                <input v-model="form.theme.header_blur" type="checkbox" />
                <span>启用顶栏 5 层渐进模糊效果（页面滚动后柔和虚化底部内容）</span>
              </label>
            </div>
          </div>
        </div>
      </div>

      <!-- 2. 顶栏与主视觉 -->
      <div v-show="activeTab === 'header-hero'" class="tab-pane">
        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>顶部导航栏</h3>
              <p class="caption">配置左上角品牌名称、中间导航链接与右侧双胶囊按钮。</p>
            </div>
            <button type="button" class="btn btn-sm" @click="addNavLink">新增导航链接</button>
          </div>

          <div class="field">
            <label class="field__label" for="home-brand-name">品牌名覆盖（选填）</label>
            <input id="home-brand-name" v-model="form.header.brand_name" class="text-input" maxlength="64" placeholder="留空则自动使用系统配置的站点名称" />
          </div>

          <div class="field">
            <span class="field__label">导航链接列表</span>
            <p v-if="!form.header.nav_links.length" class="caption">暂无导航链接，可点击右上角新增。</p>
            <div v-else class="link-list">
              <div v-for="(link, idx) in form.header.nav_links" :key="idx" class="link-row">
                <input v-model="link.label" class="text-input" placeholder="显示文案，例如：文章" aria-label="导航文案" />
                <input v-model="link.url" class="text-input mono" placeholder="链接地址，例如：/posts 或 #capabilities" aria-label="导航链接" />
                <div class="item-ops">
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === 0" @click="moveItem(form.header.nav_links, idx, -1)">上移</button>
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === form.header.nav_links.length - 1" @click="moveItem(form.header.nav_links, idx, 1)">下移</button>
                  <button type="button" class="btn btn-sm btn-danger-lite" @click="removeItem(form.header.nav_links, idx)">删除</button>
                </div>
              </div>
            </div>
          </div>

          <div class="form-row">
            <div class="field">
              <label class="field__label" for="home-header-p-label">右栏主胶囊按钮文案</label>
              <input id="home-header-p-label" v-model="form.header.primary_cta_label" class="text-input" placeholder="留空自动跟随注册模式" />
            </div>
            <div class="field">
              <label class="field__label" for="home-header-p-url">右栏主胶囊按钮链接</label>
              <input id="home-header-p-url" v-model="form.header.primary_cta_url" class="text-input mono" placeholder="留空自动跳转登录或注册页" />
            </div>
            <div class="field">
              <label class="field__label" for="home-header-s-label">右栏次胶囊按钮文案</label>
              <input id="home-header-s-label" v-model="form.header.secondary_cta_label" class="text-input" placeholder="留空自动跟随注册模式" />
            </div>
            <div class="field">
              <label class="field__label" for="home-header-s-url">右栏次胶囊按钮链接</label>
              <input id="home-header-s-url" v-model="form.header.secondary_cta_url" class="text-input mono" placeholder="留空自动处理" />
            </div>
          </div>
        </div>

        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>首屏主视觉（Hero）</h3>
              <p class="caption">主标题支持手动换行以精确控制分行节奏；自定义主标题与副标题也会同步写入首屏 SEO 元信息。</p>
            </div>
          </div>

          <div class="field">
            <label class="field__label" for="home-hero-title">主标题（支持换行）</label>
            <textarea id="home-hero-title" v-model="form.hero.title" class="textarea short-textarea" placeholder="例如：布谷，一个真的&#10;记得你的 AI 朋友" />
          </div>

          <div class="field">
            <label class="field__label" for="home-hero-subtitle">副标题（选填）</label>
            <textarea id="home-hero-subtitle" v-model="form.hero.subtitle" class="textarea short-textarea" placeholder="简洁的一两句介绍，留空时首屏保持极简大字视觉" />
          </div>

          <div class="form-row">
            <div class="field">
              <label class="field__label" for="home-hero-p-label">主胶囊按钮文案</label>
              <input id="home-hero-p-label" v-model="form.hero.primary_label" class="text-input" placeholder="留空自动跟随注册模式" />
            </div>
            <div class="field">
              <label class="field__label" for="home-hero-p-url">主胶囊按钮链接</label>
              <input id="home-hero-p-url" v-model="form.hero.primary_url" class="text-input mono" placeholder="例如：/login" />
            </div>
            <div class="field">
              <label class="field__label" for="home-hero-s-label">次胶囊按钮文案</label>
              <input id="home-hero-s-label" v-model="form.hero.secondary_label" class="text-input" placeholder="例如：探索核心能力" />
            </div>
            <div class="field">
              <label class="field__label" for="home-hero-s-url">次胶囊按钮链接</label>
              <input id="home-hero-s-url" v-model="form.hero.secondary_url" class="text-input mono" placeholder="例如：#capabilities" />
            </div>
          </div>

          <div class="form-row">
            <div class="field">
              <label class="field__label" for="home-hero-bg">可选背景图 URL</label>
              <input id="home-hero-bg" v-model="form.hero.bg_image_url" class="text-input mono" placeholder="支持 https:// 或 / 开头的站内路径，留空使用柔和光晕" />
            </div>
            <div class="field">
              <label class="field__label" for="home-hero-overlay">背景暗化遮罩透明度（0–80%）</label>
              <div class="range-row">
                <input
                  v-model.number="form.hero.bg_overlay"
                  class="range-slider"
                  type="range"
                  min="0"
                  max="80"
                  step="1"
                  aria-label="遮罩透明度滑块"
                />
                <input
                  id="home-hero-overlay"
                  v-model="form.hero.bg_overlay"
                  class="text-input range-num"
                  type="number"
                  min="0"
                  max="80"
                  step="1"
                />
                <span class="caption">%</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 3. 焦点竖卡 -->
      <div v-show="activeTab === 'showcase'" class="tab-pane">
        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>焦点竖卡（2:3 横向轮播）</h3>
              <p class="caption">共 {{ form.showcase.cards.length }} 张竖卡，建议保持 3 至 6 张，每张卡左上放标题副标题，左下放白底主按钮与玻璃次按钮。</p>
            </div>
            <button type="button" class="btn btn-sm" @click="addShowcaseCard">新增竖卡</button>
          </div>

          <p v-if="!form.showcase.cards.length" class="caption empty-tip">还没有焦点竖卡，点击右上角「新增竖卡」添加第一张。</p>

          <div v-else class="card-stack card-stack--grid">
            <article v-for="(card, idx) in form.showcase.cards" :key="card.id" class="editor-card">
              <header class="editor-card__head">
                <div class="editor-card__title">
                  <span class="order-tag">{{ idx + 1 }}</span>
                  <strong>{{ card.title || '未命名焦点竖卡' }}</strong>
                  <span class="badge-pill">{{ SHOWCASE_THEMES.find((t) => t.value === card.theme)?.label || card.theme }}</span>
                </div>
                <div class="item-ops">
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === 0" @click="moveItem(form.showcase.cards, idx, -1)">上移</button>
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === form.showcase.cards.length - 1" @click="moveItem(form.showcase.cards, idx, 1)">下移</button>
                  <button type="button" class="btn btn-sm btn-ghost" @click="toggleCollapsed(`card:${card.id}`)">
                    {{ collapsed[`card:${card.id}`] ? '展开' : '收起' }}
                  </button>
                  <button type="button" class="btn btn-sm btn-danger-lite" @click="removeItem(form.showcase.cards, idx)">删除</button>
                </div>
              </header>

              <div v-show="!collapsed[`card:${card.id}`]" class="editor-card__body">
                <div class="form-row">
                  <div class="field">
                    <label class="field__label">卡片标题</label>
                    <input v-model="card.title" class="text-input" placeholder="例如：长期记忆引擎" />
                  </div>
                  <div class="field">
                    <label class="field__label">色板主题</label>
                    <div class="swatch-group" role="radiogroup" aria-label="竖卡色板主题">
                      <button
                        v-for="t in SHOWCASE_THEMES"
                        :key="t.value"
                        type="button"
                        role="radio"
                        class="swatch-btn"
                        :class="{ 'is-active': card.theme === t.value }"
                        :aria-checked="card.theme === t.value"
                        @click="card.theme = t.value"
                      >
                        <span class="swatch-dot" :style="{ background: t.swatch }" aria-hidden="true" />
                        <span>{{ t.label }}</span>
                      </button>
                    </div>
                  </div>
                </div>

                <div class="form-row">
                  <div class="field">
                    <label class="field__label">副标题</label>
                    <input v-model="card.subtitle" class="text-input" placeholder="简短描述这张卡的亮点" />
                  </div>
                  <div class="field">
                    <label class="field__label">可选封面图 URL</label>
                    <input v-model="card.image_url" class="text-input mono" placeholder="留空则使用精致色板纹理背景" />
                  </div>
                </div>

                <div class="form-row">
                  <div class="field">
                    <label class="field__label">白底主按钮文案</label>
                    <input v-model="card.primary_label" class="text-input" placeholder="例如：了解原理" />
                  </div>
                  <div class="field">
                    <label class="field__label">白底主按钮链接</label>
                    <input v-model="card.primary_url" class="text-input mono" placeholder="例如：/login 或 https://arena.buugoo.com" />
                  </div>
                  <div class="field">
                    <label class="field__label">玻璃次按钮文案（选填）</label>
                    <input v-model="card.secondary_label" class="text-input" placeholder="留空不显示次按钮" />
                  </div>
                  <div class="field">
                    <label class="field__label">玻璃次按钮链接（选填）</label>
                    <input v-model="card.secondary_url" class="text-input mono" placeholder="例如：/posts" />
                  </div>
                </div>
              </div>
            </article>
          </div>
        </div>
      </div>

      <!-- 4. 动态与能力书架 -->
      <div v-show="activeTab === 'news-caps'" class="tab-pane">
        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>最新动态板块</h3>
              <p class="caption">自动拉取「文章管理」中已发布的文章，左侧展示主推文章，右侧展示极简排版列表。</p>
            </div>
          </div>

          <div class="form-row">
            <div class="field">
              <label class="field__label" for="home-news-title">分区标题</label>
              <input id="home-news-title" v-model="form.news.title" class="text-input" placeholder="例如：最新动态" />
            </div>
            <div class="field">
              <label class="field__label" for="home-news-limit">展示篇数（3–8 篇）</label>
              <input id="home-news-limit" v-model="form.news.limit" class="text-input" type="number" min="3" max="8" step="1" />
            </div>
            <div class="field">
              <label class="field__label" for="home-news-all-label">查看全部按钮文案</label>
              <input id="home-news-all-label" v-model="form.news.view_all_label" class="text-input" placeholder="例如：查看全部文章" />
            </div>
            <div class="field">
              <label class="field__label" for="home-news-all-url">查看全部按钮链接</label>
              <input id="home-news-all-url" v-model="form.news.view_all_url" class="text-input mono" placeholder="/posts" />
            </div>
          </div>

          <div class="field">
            <label class="field__label" for="home-news-category">按分类筛选（留空显示全部已发布文章）</label>
            <input id="home-news-category" v-model="form.news.category" class="text-input" placeholder="留空表示不限分类" />
            <div class="preset-row">
              <button type="button" class="preset-pill" :class="{ 'is-active': !form.news.category }" @click="form.news.category = ''">全部分类</button>
              <button
                v-for="cat in PRESET_NEWS_CATEGORIES"
                :key="cat"
                type="button"
                class="preset-pill"
                :class="{ 'is-active': form.news.category === cat }"
                @click="form.news.category = cat"
              >
                {{ cat }}
              </button>
            </div>
          </div>
        </div>

        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>能力与生态书架</h3>
              <p class="caption">共 {{ form.capabilities.items.length }} 张能力卡片，桌面端横向展示 4 列并支持翻页。</p>
            </div>
            <button type="button" class="btn btn-sm" @click="addCapabilityItem">新增能力卡片</button>
          </div>

          <div class="form-row">
            <div class="field">
              <label class="field__label" for="home-cap-title">分区主标题</label>
              <input id="home-cap-title" v-model="form.capabilities.title" class="text-input" placeholder="例如：围绕真实陪伴打造" />
            </div>
            <div class="field">
              <label class="field__label" for="home-cap-subtitle">分区副标题</label>
              <input id="home-cap-subtitle" v-model="form.capabilities.subtitle" class="text-input" placeholder="例如：从长期记忆到开放评测生态" />
            </div>
            <div class="field">
              <label class="field__label" for="home-cap-cta-label">右上角按钮文案</label>
              <input id="home-cap-cta-label" v-model="form.capabilities.cta_label" class="text-input" placeholder="例如：开始体验" />
            </div>
            <div class="field">
              <label class="field__label" for="home-cap-cta-url">右上角按钮链接</label>
              <input id="home-cap-cta-url" v-model="form.capabilities.cta_url" class="text-input mono" placeholder="/login" />
            </div>
          </div>

          <p v-if="!form.capabilities.items.length" class="caption empty-tip">还没有能力卡片，点击右上角「新增能力卡片」添加。</p>

          <div v-else class="card-stack card-stack--grid">
            <article v-for="(item, idx) in form.capabilities.items" :key="item.id" class="editor-card">
              <header class="editor-card__head">
                <div class="editor-card__title">
                  <span class="order-tag">{{ idx + 1 }}</span>
                  <strong>{{ item.title || '未命名能力卡片' }}</strong>
                  <span v-if="item.badge" class="chip-note chip-compact">{{ item.badge }}</span>
                </div>
                <div class="item-ops">
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === 0" @click="moveItem(form.capabilities.items, idx, -1)">上移</button>
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === form.capabilities.items.length - 1" @click="moveItem(form.capabilities.items, idx, 1)">下移</button>
                  <button type="button" class="btn btn-sm btn-ghost" @click="toggleCollapsed(`cap:${item.id}`)">
                    {{ collapsed[`cap:${item.id}`] ? '展开' : '收起' }}
                  </button>
                  <button type="button" class="btn btn-sm btn-danger-lite" @click="removeItem(form.capabilities.items, idx)">删除</button>
                </div>
              </header>

              <div v-show="!collapsed[`cap:${item.id}`]" class="editor-card__body">
                <div class="form-row-3">
                  <div class="field">
                    <label class="field__label">图标</label>
                    <select v-model="item.icon" class="text-input">
                      <option v-for="ic in CAP_ICONS" :key="ic.value" :value="ic.value">{{ ic.label }}</option>
                    </select>
                  </div>
                  <div class="field">
                    <label class="field__label">角标（选填）</label>
                    <input v-model="item.badge" class="text-input" maxlength="16" placeholder="例如：新" />
                  </div>
                  <div class="field">
                    <label class="field__label">能力标题</label>
                    <input v-model="item.title" class="text-input" placeholder="例如：长期记忆" />
                  </div>
                </div>

                <div class="field">
                  <label class="field__label">简介描述</label>
                  <textarea v-model="item.description" class="textarea short-textarea" placeholder="两行左右的清晰能力说明" />
                </div>

                <div class="field">
                  <div class="sub-head">
                    <label class="field__label">子链接组</label>
                    <button type="button" class="btn btn-sm btn-ghost" @click="item.links.push({ label: '', url: '' })">新增直达链接</button>
                  </div>
                  <input v-model="item.links_heading" class="text-input mb-8" placeholder="子链接组标题，例如：立即体验 / 评测维度" />
                  <div v-if="item.links.length" class="link-list">
                    <div v-for="(link, lIdx) in item.links" :key="lIdx" class="link-row">
                      <input v-model="link.label" class="text-input" placeholder="链接文案" aria-label="直达链接文案" />
                      <input v-model="link.url" class="text-input mono" placeholder="链接地址" aria-label="直达链接地址" />
                      <div class="item-ops">
                        <button type="button" class="btn btn-sm btn-ghost" :disabled="lIdx === 0" @click="moveItem(item.links, lIdx, -1)">上移</button>
                        <button type="button" class="btn btn-sm btn-ghost" :disabled="lIdx === item.links.length - 1" @click="moveItem(item.links, lIdx, 1)">下移</button>
                        <button type="button" class="btn btn-sm btn-danger-lite" @click="removeItem(item.links, lIdx)">删除</button>
                      </div>
                    </div>
                  </div>
                </div>

                <div class="form-row">
                  <div class="field">
                    <label class="field__label">底部了解更多文案（选填）</label>
                    <input v-model="item.learn_more_label" class="text-input" placeholder="例如：了解更多" />
                  </div>
                  <div class="field">
                    <label class="field__label">底部了解更多链接（选填）</label>
                    <input v-model="item.learn_more_url" class="text-input mono" placeholder="例如：/posts" />
                  </div>
                </div>
              </div>
            </article>
          </div>
        </div>
      </div>

      <!-- 5. 交互舞台与自定义区 -->
      <div v-show="activeTab === 'spotlight-custom'" class="tab-pane">
        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>交互舞台（手风琴 + 右侧演示区）</h3>
              <p class="caption">共 {{ form.spotlight.items.length }} 项，点击左侧手风琴条目时，右侧舞台会平滑切换到对应演示内容。</p>
            </div>
            <button type="button" class="btn btn-sm" @click="addSpotlightItem">新增手风琴条目</button>
          </div>

          <div class="form-row">
            <div class="field">
              <label class="field__label" for="home-spot-title">分区主标题</label>
              <input id="home-spot-title" v-model="form.spotlight.title" class="text-input" placeholder="例如：像老朋友一样自然相处" />
            </div>
            <div class="field">
              <label class="field__label" for="home-spot-subtitle">分区副标题</label>
              <input id="home-spot-subtitle" v-model="form.spotlight.subtitle" class="text-input" placeholder="例如：每一次对话都在悄悄积累默契" />
            </div>
            <div class="field">
              <label class="field__label" for="home-spot-cta-label">右上角按钮文案</label>
              <input id="home-spot-cta-label" v-model="form.spotlight.cta_label" class="text-input" placeholder="例如：阅读更多故事" />
            </div>
            <div class="field">
              <label class="field__label" for="home-spot-cta-url">右上角按钮链接</label>
              <input id="home-spot-cta-url" v-model="form.spotlight.cta_url" class="text-input mono" placeholder="/posts" />
            </div>
          </div>

          <p v-if="!form.spotlight.items.length" class="caption empty-tip">还没有手风琴条目，点击右上角「新增手风琴条目」添加。</p>

          <div v-else class="card-stack">
            <article v-for="(item, idx) in form.spotlight.items" :key="item.id" class="editor-card">
              <header class="editor-card__head">
                <div class="editor-card__title">
                  <span class="order-tag">{{ idx + 1 }}</span>
                  <strong>{{ item.title || '未命名手风琴条目' }}</strong>
                  <span class="badge-pill">{{ item.stage_type }}</span>
                </div>
                <div class="item-ops">
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === 0" @click="moveItem(form.spotlight.items, idx, -1)">上移</button>
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === form.spotlight.items.length - 1" @click="moveItem(form.spotlight.items, idx, 1)">下移</button>
                  <button type="button" class="btn btn-sm btn-ghost" @click="toggleCollapsed(`spot:${item.id}`)">
                    {{ collapsed[`spot:${item.id}`] ? '展开' : '收起' }}
                  </button>
                  <button type="button" class="btn btn-sm btn-danger-lite" @click="removeItem(form.spotlight.items, idx)">删除</button>
                </div>
              </header>

              <div v-show="!collapsed[`spot:${item.id}`]" class="editor-card__body">
                <div class="form-row">
                  <div class="field">
                    <label class="field__label">手风琴标题</label>
                    <input v-model="item.title" class="text-input" placeholder="例如：上周说过的话，今天还记得" />
                  </div>
                  <div class="field">
                    <label class="field__label">右侧舞台类型</label>
                    <select v-model="item.stage_type" class="text-input">
                      <option v-for="st in STAGE_TYPES" :key="st.value" :value="st.value">{{ st.label }}</option>
                    </select>
                  </div>
                </div>

                <div class="field">
                  <label class="field__label">手风琴展开说明</label>
                  <textarea v-model="item.description" class="textarea short-textarea" placeholder="点击该条目后在左侧展开的说明文字" />
                </div>

                <div class="form-row">
                  <div class="field">
                    <label class="field__label">舞台配图 URL（选填）</label>
                    <input v-model="item.image_url" class="text-input mono" placeholder="舞台类型为 image 时展示大图" />
                  </div>
                  <div class="field">
                    <label class="field__label">舞台标题（选填）</label>
                    <input v-model="item.stage_title" class="text-input" placeholder="展示在右侧舞台卡片中的主标题" />
                  </div>
                </div>

                <div class="field">
                  <label class="field__label">舞台正文（选填）</label>
                  <textarea v-model="item.stage_body" class="textarea short-textarea" placeholder="右侧舞台卡片中的补充内容或要点" />
                </div>

                <div class="form-row">
                  <div class="field">
                    <label class="field__label">舞台按钮文案（选填）</label>
                    <input v-model="item.stage_cta_label" class="text-input" placeholder="例如：前往盲测竞技场" />
                  </div>
                  <div class="field">
                    <label class="field__label">舞台按钮链接（选填）</label>
                    <input v-model="item.stage_cta_url" class="text-input mono" placeholder="例如：https://arena.buugoo.com" />
                  </div>
                </div>
              </div>
            </article>
          </div>
        </div>

        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>自定义扩展区块</h3>
              <p class="caption">共 {{ form.custom_blocks.length }} 个自定义区块，支持 Markdown 正文、配图与三种主题风格，在布局顺序中的「自定义扩展区」位置渲染。</p>
            </div>
            <button type="button" class="btn btn-sm" @click="addCustomBlock">新增自定义区块</button>
          </div>

          <p v-if="!form.custom_blocks.length" class="caption empty-tip">当前没有自定义扩展区块，如需发布活动说明、品牌故事或合作入口可点击新增。</p>

          <div v-else class="card-stack">
            <article v-for="(block, idx) in form.custom_blocks" :key="block.id" class="editor-card">
              <header class="editor-card__head">
                <div class="editor-card__title">
                  <span class="order-tag">{{ idx + 1 }}</span>
                  <strong>{{ block.title || '未命名自定义区块' }}</strong>
                  <span class="badge-pill">{{ CUSTOM_THEMES.find((t) => t.value === block.theme)?.label || block.theme }}</span>
                </div>
                <div class="item-ops">
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === 0" @click="moveItem(form.custom_blocks, idx, -1)">上移</button>
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === form.custom_blocks.length - 1" @click="moveItem(form.custom_blocks, idx, 1)">下移</button>
                  <button type="button" class="btn btn-sm btn-ghost" @click="toggleCollapsed(`custom:${block.id}`)">
                    {{ collapsed[`custom:${block.id}`] ? '展开' : '收起' }}
                  </button>
                  <button type="button" class="btn btn-sm btn-danger-lite" @click="removeItem(form.custom_blocks, idx)">删除</button>
                </div>
              </header>

              <div v-show="!collapsed[`custom:${block.id}`]" class="editor-card__body">
                <div class="form-row-3">
                  <div class="field">
                    <label class="field__label">眉标（选填）</label>
                    <input v-model="block.eyebrow" class="text-input" placeholder="例如：开放生态" />
                  </div>
                  <div class="field">
                    <label class="field__label">区块标题</label>
                    <input v-model="block.title" class="text-input" placeholder="区块主标题" />
                  </div>
                  <div class="field">
                    <label class="field__label">区块主题</label>
                    <select v-model="block.theme" class="text-input">
                      <option v-for="ct in CUSTOM_THEMES" :key="ct.value" :value="ct.value">{{ ct.label }}</option>
                    </select>
                  </div>
                </div>

                <div class="form-row">
                  <div class="field">
                    <label class="field__label">副标题（选填）</label>
                    <input v-model="block.subtitle" class="text-input" placeholder="区块副标题" />
                  </div>
                  <div class="field">
                    <label class="field__label">配图 URL（选填）</label>
                    <input v-model="block.image_url" class="text-input mono" placeholder="支持 https:// 或 / 开头的站内路径" />
                  </div>
                </div>

                <div class="field">
                  <label class="field__label">Markdown 正文</label>
                  <textarea v-model="block.body_md" class="textarea md-area" placeholder="支持段落、列表、加粗与链接等 Markdown 语法" />
                </div>

                <div class="form-row">
                  <div class="field">
                    <label class="field__label">主按钮文案（选填）</label>
                    <input v-model="block.cta_label" class="text-input" placeholder="例如：立即查看" />
                  </div>
                  <div class="field">
                    <label class="field__label">主按钮链接（选填）</label>
                    <input v-model="block.cta_url" class="text-input mono" placeholder="/posts" />
                  </div>
                  <div class="field">
                    <label class="field__label">次按钮文案（选填）</label>
                    <input v-model="block.secondary_label" class="text-input" placeholder="留空不显示" />
                  </div>
                  <div class="field">
                    <label class="field__label">次按钮链接（选填）</label>
                    <input v-model="block.secondary_url" class="text-input mono" placeholder="例如：https://arena.buugoo.com" />
                  </div>
                </div>
              </div>
            </article>
          </div>
        </div>
      </div>

      <!-- 6. 页脚与 JSON -->
      <div v-show="activeTab === 'footer-json'" class="tab-pane">
        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>页脚顶部行动条</h3>
              <p class="caption">展示在页脚上方的细长行动引导条，可用于引导开始对话或订阅动态。</p>
            </div>
          </div>

          <label class="check-row">
            <input v-model="form.footer.show_cta_bar" type="checkbox" />
            <span>在页脚顶部显示行动条</span>
          </label>

          <div v-if="form.footer.show_cta_bar" class="cta-bar-fields">
            <div class="form-row">
              <div class="field">
                <label class="field__label" for="home-f-cta-head">行动条标题</label>
                <input id="home-f-cta-head" v-model="form.footer.cta_heading" class="text-input" placeholder="例如：认识你的布谷朋友" />
              </div>
              <div class="field">
                <label class="field__label" for="home-f-cta-note">行动条说明</label>
                <input id="home-f-cta-note" v-model="form.footer.cta_note" class="text-input" placeholder="简短一句补充文案" />
              </div>
              <div class="field">
                <label class="field__label" for="home-f-cta-ph">占位提示文字（选填）</label>
                <input id="home-f-cta-ph" v-model="form.footer.cta_placeholder" class="text-input" placeholder="例如：从今天的一句问候开始" />
              </div>
              <div class="field">
                <label class="field__label" for="home-f-cta-btn">按钮文案</label>
                <input id="home-f-cta-btn" v-model="form.footer.cta_button_label" class="text-input" placeholder="例如：立即开始" />
              </div>
            </div>
            <div class="field">
              <label class="field__label" for="home-f-cta-url">按钮链接</label>
              <input id="home-f-cta-url" v-model="form.footer.cta_button_url" class="text-input mono" placeholder="/login" />
            </div>
          </div>
        </div>

        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>左侧使命宣言与链接栏目</h3>
              <p class="caption">左侧为大字使命短句（支持换行），右侧建议配置 3 组链接栏。</p>
            </div>
            <button type="button" class="btn btn-sm" @click="addFooterColumn">新增链接栏</button>
          </div>

          <div class="field">
            <label class="field__label" for="home-footer-mission">左侧使命短句（支持换行）</label>
            <textarea id="home-footer-mission" v-model="form.footer.mission" class="textarea short-textarea" placeholder="例如：记住日常的微光&#10;陪你走过长久的时光" />
          </div>

          <datalist id="home-footer-icon-options">
            <option v-for="ic in CAP_ICONS" :key="ic.value" :value="ic.value" />
          </datalist>

          <p v-if="!form.footer.columns.length" class="caption empty-tip">还没有页脚链接栏，点击右上角「新增链接栏」添加。</p>

          <div v-else class="card-stack">
            <article v-for="(col, cIdx) in form.footer.columns" :key="cIdx" class="editor-card">
              <header class="editor-card__head">
                <div class="editor-card__title">
                  <span class="order-tag">{{ cIdx + 1 }}</span>
                  <strong>{{ col.title || '未命名链接栏' }}</strong>
                  <span class="caption">（{{ col.links.length }} 条链接）</span>
                </div>
                <div class="item-ops">
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="cIdx === 0" @click="moveItem(form.footer.columns, cIdx, -1)">上移</button>
                  <button type="button" class="btn btn-sm btn-ghost" :disabled="cIdx === form.footer.columns.length - 1" @click="moveItem(form.footer.columns, cIdx, 1)">下移</button>
                  <button type="button" class="btn btn-sm btn-danger-lite" @click="removeItem(form.footer.columns, cIdx)">删除栏目</button>
                </div>
              </header>

              <div class="editor-card__body">
                <div class="field">
                  <label class="field__label">栏目标题</label>
                  <input v-model="col.title" class="text-input" placeholder="例如：产品与能力" />
                </div>

                <div class="field">
                  <div class="sub-head">
                    <span class="field__label">栏目链接</span>
                    <button type="button" class="btn btn-sm btn-ghost" @click="col.links.push({ label: '', url: '', icon: '' })">新增链接</button>
                  </div>
                  <div v-if="col.links.length" class="link-list">
                    <div v-for="(link, lIdx) in col.links" :key="lIdx" class="link-row link-row--footer">
                      <input v-model="link.label" class="text-input" placeholder="链接文案" aria-label="页脚链接文案" />
                      <input v-model="link.url" class="text-input mono" placeholder="链接地址" aria-label="页脚链接地址" />
                      <input
                        v-model="link.icon"
                        class="text-input mono"
                        list="home-footer-icon-options"
                        placeholder="图标（选填）"
                        aria-label="页脚链接图标"
                      />
                      <div class="item-ops">
                        <button type="button" class="btn btn-sm btn-ghost" :disabled="lIdx === 0" @click="moveItem(col.links, lIdx, -1)">上移</button>
                        <button type="button" class="btn btn-sm btn-ghost" :disabled="lIdx === col.links.length - 1" @click="moveItem(col.links, lIdx, 1)">下移</button>
                        <button type="button" class="btn btn-sm btn-danger-lite" @click="removeItem(col.links, lIdx)">删除</button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </article>
          </div>
        </div>

        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>底部隐私说明与链接</h3>
              <p class="caption">显示在页脚最底一行，通常包含版权/隐私说明、RSS 订阅或文章目录链接。</p>
            </div>
            <button type="button" class="btn btn-sm" @click="addFooterBottomLink">新增底部链接</button>
          </div>

          <div class="field">
            <label class="field__label" for="home-footer-note">底部隐私与版权说明</label>
            <input id="home-footer-note" v-model="form.footer.bottom_note" class="text-input" placeholder="例如：你的对话与记忆只保存在你自己的服务器中" />
          </div>

          <div v-if="form.footer.bottom_links.length" class="link-list">
            <div v-for="(link, idx) in form.footer.bottom_links" :key="idx" class="link-row">
              <input v-model="link.label" class="text-input" placeholder="链接文案，例如：RSS 订阅" aria-label="底部链接文案" />
              <input v-model="link.url" class="text-input mono" placeholder="链接地址，例如：/rss.xml" aria-label="底部链接地址" />
              <div class="item-ops">
                <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === 0" @click="moveItem(form.footer.bottom_links, idx, -1)">上移</button>
                <button type="button" class="btn btn-sm btn-ghost" :disabled="idx === form.footer.bottom_links.length - 1" @click="moveItem(form.footer.bottom_links, idx, 1)">下移</button>
                <button type="button" class="btn btn-sm btn-danger-lite" @click="removeItem(form.footer.bottom_links, idx)">删除</button>
              </div>
            </div>
          </div>
        </div>

        <div class="group-block">
          <div class="block-head">
            <div>
              <h3>配置 JSON 备份与迁移</h3>
              <p class="caption">可导出当前全部编排为 JSON 文件备份，或粘贴别的站点的配置 JSON 一键导入。</p>
            </div>
            <div class="item-ops">
              <button type="button" class="btn btn-sm" @click="copyExportJson">复制 JSON</button>
              <button type="button" class="btn btn-sm" @click="downloadExportJson">下载 JSON</button>
              <button type="button" class="btn btn-sm btn-outline" @click="openJsonModal">打开导入 / 导出弹窗</button>
            </div>
          </div>
        </div>
      </div>

      <div v-if="isDirty" class="save-bar">
        <span>当前官网首页配置有未保存的改动</span>
        <div class="item-ops">
          <button type="button" class="btn btn-sm btn-ghost" :disabled="saving" @click="load">放弃未保存修改</button>
          <button type="button" class="btn btn-sm btn-primary" :disabled="saving" @click="save">
            {{ saving ? '正在保存' : '保存修改' }}
          </button>
        </div>
      </div>
    </template>

    <!-- JSON 导入导出弹窗 -->
    <Modal
      :open="jsonModalOpen"
      title="导入 / 导出首页配置 JSON"
      subtitle="可一键复制或下载当前配置备份；也可在下方粘贴合法的配置 JSON 校验后导入表单或直接保存。"
      wide
      @close="jsonModalOpen = false"
    >
      <div class="json-grid">
        <div class="field">
          <div class="sub-head">
            <label class="field__label" for="home-json-export">当前配置 JSON（只读）</label>
            <div class="item-ops">
              <button type="button" class="btn btn-sm" @click="copyExportJson">复制</button>
              <button type="button" class="btn btn-sm btn-outline" @click="downloadExportJson">下载文件</button>
            </div>
          </div>
          <textarea id="home-json-export" class="textarea json-area" :value="exportJsonText" readonly spellcheck="false" />
        </div>

        <div class="field">
          <div class="sub-head">
            <label class="field__label" for="home-json-import">粘贴要导入的 JSON</label>
            <button type="button" class="btn btn-sm btn-ghost" @click="jsonImportText = exportJsonText">填入当前配置</button>
          </div>
          <textarea
            id="home-json-import"
            v-model="jsonImportText"
            class="textarea json-area"
            spellcheck="false"
            placeholder="在此粘贴导出的首页配置 JSON 对象"
          />
        </div>
      </div>

      <p v-if="jsonError" class="form-flash form-flash--error">{{ jsonError }}</p>

      <div class="modal-actions">
        <button type="button" class="btn" @click="jsonModalOpen = false">关闭</button>
        <button type="button" class="btn btn-outline" @click="importJsonOnly">校验并填入表单</button>
        <button type="button" class="btn btn-primary" :disabled="saving" @click="importJsonAndSave">
          {{ saving ? '正在保存' : '导入并立即保存' }}
        </button>
      </div>
    </Modal>

    <!-- 恢复默认二次确认弹窗 -->
    <Modal
      :open="confirmResetOpen"
      title="恢复默认官网首页配置？"
      subtitle="已保存的自定义首页布局、颜色与文案会被清空，立即恢复为布谷内置默认首页。"
      @close="confirmResetOpen = false"
    >
      <div class="modal-actions">
        <button type="button" class="btn" :disabled="resetting" @click="confirmResetOpen = false">取消</button>
        <button type="button" class="btn btn-danger" :disabled="resetting" @click="confirmReset">
          {{ resetting ? '正在恢复' : '确认恢复默认' }}
        </button>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
.home-cms {
  border-radius: 24px;
  border: 0;
  background: var(--md-surface-container-low);
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px 16px;
  flex-wrap: wrap;
}
.head__title {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.head h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 500;
  letter-spacing: -.02em;
}
.status-chip {
  height: 24px;
  padding: 0 10px;
  font-size: 12px;
}
.head__btns {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.intro {
  margin: 8px 0 18px;
  font-size: 14px;
  line-height: 1.65;
}
.skel {
  height: 180px;
  border-radius: 16px;
}

.subtabs {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 4px;
  padding: 4px;
  margin-bottom: 22px;
  border: 0;
  border-radius: 9999px;
  background: var(--md-surface-container);
  overflow-x: auto;
  scrollbar-width: none;
  -webkit-overflow-scrolling: touch;
}
.subtabs::-webkit-scrollbar {
  display: none;
}
.subtabs__item {
  flex: 1 0 auto;
  min-height: 36px;
  padding: 0 16px;
  border: 0;
  border-radius: 9999px;
  color: var(--color-text-secondary);
  font-size: 13.5px;
  font-weight: 500;
  white-space: nowrap;
  transition: background-color var(--motion-fast) var(--md-ease), color var(--motion-fast) var(--md-ease);
}
.subtabs__item:hover {
  background: var(--md-state-hover);
  color: var(--color-ink);
}
.subtabs__item.is-active {
  background: var(--md-primary-container);
  color: var(--md-on-primary-container);
  font-weight: 600;
  box-shadow: none;
}

.tab-pane {
  display: grid;
  gap: 24px;
}
.group-block {
  padding-top: 22px;
  border-top: 1px solid rgba(20, 20, 19, .08);
}
.group-block:first-child {
  padding-top: 0;
  border-top: 0;
}
.block-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}
.block-head h3 {
  margin: 0 0 4px;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: -.015em;
  color: var(--color-ink);
}
.block-head p {
  margin: 0;
}

.section-list {
  display: grid;
  gap: 8px;
}
.section-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px 16px;
  padding: 12px 16px;
  border: 0;
  border-radius: 16px;
  background: var(--md-surface-container-lowest);
  transition: box-shadow var(--motion-fast) var(--md-ease);
}
.section-row:hover {
  box-shadow: var(--md-elev-1);
}
.section-row__check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}
.section-row__name {
  font-size: 14px;
  font-weight: 500;
  color: var(--color-ink);
}
.section-row__id {
  padding: 2px 8px;
  border-radius: 9999px;
  background: var(--md-surface-container);
  color: var(--color-text-secondary);
}
.section-row__hint {
  min-width: 0;
}

.color-grid {
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
}
.color-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.color-picker {
  width: 42px;
  height: 42px;
  padding: 3px;
  flex: 0 0 42px;
  border: 0;
  border-radius: 12px;
  background: var(--md-surface-container-lowest);
  box-shadow: var(--md-elev-1);
  cursor: pointer;
}
.mono {
  font-family: var(--font-mono);
  font-size: 13px;
}

.range-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.range-slider {
  flex: 1 1 auto;
  min-width: 0;
  accent-color: var(--color-accent);
}
.range-num {
  width: 78px;
  flex: 0 0 78px;
  min-height: 38px;
  padding: 6px 8px;
}
.check-box-inline {
  margin: 8px 0 0;
}

.short-textarea {
  min-height: 76px;
}
.md-area {
  min-height: 150px;
  font-family: var(--font-mono);
  font-size: 13px;
  line-height: 1.65;
}
.mb-8 {
  margin-bottom: 8px;
}
.empty-tip {
  margin: 4px 0 0;
}

.link-list {
  display: grid;
  gap: 8px;
}
.link-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.35fr) auto;
  gap: 8px;
  align-items: center;
}
.link-row--footer {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.25fr) 136px auto;
}
.item-ops {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}

.card-stack {
  display: grid;
  gap: 16px;
}
.card-stack--grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
}
.card-stack--grid .link-row {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr);
  padding: 10px;
  border: 0;
  border-radius: 14px;
  background: var(--md-surface-container-low);
}
.card-stack--grid .link-row .item-ops {
  grid-column: 1 / -1;
  justify-content: flex-end;
}
.editor-card {
  border: 0;
  border-radius: 20px;
  background: var(--md-surface-container-lowest);
  box-shadow: var(--md-elev-1);
  overflow: hidden;
  transition: box-shadow var(--motion-fast) var(--md-ease);
}
.editor-card:hover {
  box-shadow: var(--md-elev-2);
}
.editor-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 13px 16px;
  border-bottom: 0;
  background: var(--md-surface-container);
  flex-wrap: wrap;
}
.editor-card__title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex-wrap: wrap;
}
.editor-card__title strong {
  font-size: 14.5px;
  font-weight: 600;
  color: var(--color-ink);
  overflow-wrap: anywhere;
}
.order-tag {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--color-ink);
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  flex: 0 0 24px;
}
.editor-card__body {
  padding: 18px 16px 6px;
}
.chip-compact {
  height: 22px;
  padding: 0 8px;
  font-size: 12px;
}

.swatch-group {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.swatch-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 34px;
  padding: 0 12px;
  border: 0;
  border-radius: 9999px;
  background: var(--md-surface-container);
  color: var(--color-text-secondary);
  font-size: 13px;
  transition: background-color var(--motion-fast) var(--md-ease), color var(--motion-fast) var(--md-ease);
}
.swatch-btn:hover {
  background: var(--md-surface-container-high);
  color: var(--color-ink);
}
.swatch-btn.is-active {
  background: var(--md-primary-container);
  color: var(--md-on-primary-container);
  font-weight: 600;
  box-shadow: none;
}
.swatch-dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 1px solid rgba(20, 20, 19, .15);
  flex: 0 0 12px;
}

.form-row-3 {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  align-items: start;
}
.sub-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}
.preset-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 2px;
}
.preset-pill {
  min-height: 28px;
  padding: 0 12px;
  border: 0;
  border-radius: 9999px;
  background: var(--md-surface-container);
  color: var(--color-text-secondary);
  font-size: 12px;
  transition: background-color var(--motion-fast) var(--md-ease), color var(--motion-fast) var(--md-ease);
}
.preset-pill:hover {
  background: var(--md-surface-container-high);
  color: var(--color-ink);
}
.preset-pill.is-active {
  background: var(--md-primary-container);
  color: var(--md-on-primary-container);
  font-weight: 600;
}

.cta-bar-fields {
  margin-top: 12px;
}

.save-bar {
  position: sticky;
  bottom: 18px;
  z-index: 15;
  width: fit-content;
  max-width: 100%;
  margin: 24px auto 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 10px 14px 10px 20px;
  border: 0;
  border-radius: 9999px;
  background: rgba(255, 255, 255, .9);
  -webkit-backdrop-filter: blur(16px);
  backdrop-filter: blur(16px);
  box-shadow: var(--md-elev-3);
  font-size: 13.5px;
  font-weight: 500;
  color: var(--color-ink);
  flex-wrap: wrap;
}

.json-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.json-area {
  min-height: 360px;
  border-radius: 14px;
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre;
  overflow-x: auto;
}

@media (max-width: 1180px) {
  .card-stack--grid {
    grid-template-columns: 1fr;
  }
  .card-stack--grid .link-row {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.35fr) auto;
    padding: 0;
    border: 0;
    background: transparent;
  }
  .card-stack--grid .link-row .item-ops {
    grid-column: auto;
  }
  .color-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 767px) {
  .home-cms {
    border-radius: 20px;
  }
  .subtabs {
    padding: 4px;
    border-radius: 9999px;
  }
  .subtabs__item {
    flex: 0 0 auto;
    padding: 0 13px;
    font-size: 12.5px;
  }
  .color-grid {
    grid-template-columns: 1fr;
  }
  .section-row {
    grid-template-columns: minmax(0, 1fr);
    gap: 6px;
  }
  .link-row,
  .link-row--footer,
  .card-stack--grid .link-row {
    grid-template-columns: minmax(0, 1fr);
    padding: 10px;
    border: 0;
    border-radius: 14px;
    background: var(--md-surface-container-low);
  }
  .form-row-3,
  .json-grid {
    grid-template-columns: 1fr;
  }
  .json-area {
    min-height: 200px;
  }
  .save-bar {
    width: 100%;
    border-radius: 24px;
    padding: 10px 14px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .subtabs__item,
  .section-row,
  .editor-card,
  .swatch-btn,
  .preset-pill {
    transition: none;
  }
}
</style>
