<script setup lang="ts">
// 网站首页：视觉与排版 1:1 对齐 DeepMind 官网设计语言（保留布谷暖米白/墨黑/橄榄绿默认色板），
// 完整接入 GET /api/v1/cms/home 高自由度官网编排配置与公开 CMS 文章列表。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '../Icon.vue'
import {
  api,
  type CmsHomeCapabilityIcon,
  type CmsHomeConfig,
  type CmsHomeSection,
  type CmsHomeSectionId,
  type CmsPostSummary,
} from '../api'
import { renderMarkdown } from '../markdown'
import { setPageSeo, toAbsoluteUrl } from '../seo'
import { needsLogin, session } from '../session'

const VALID_SECTION_IDS: readonly CmsHomeSectionId[] = [
  'hero',
  'showcase',
  'news',
  'capabilities',
  'spotlight',
  'custom',
  'footer',
]

const VALID_ICONS = new Set<string>([
  'brain',
  'sparkle',
  'clock',
  'note',
  'message',
  'globe',
  'code',
  'mask',
  'star',
  'book',
  'search',
  'link',
  'file',
  'grid',
  'settings',
])

const VALID_COVER_THEMES = ['obsidian', 'aurora', 'cobalt', 'sage', 'sand'] as const

type ValidIconName =
  | CmsHomeCapabilityIcon
  | 'book'
  | 'search'
  | 'link'
  | 'file'
  | 'grid'
  | 'settings'

const metaName = computed(() => session.meta?.name?.trim() || '布谷')
const metaTitle = computed(() => session.meta?.home?.title?.trim() || '')
const metaTagline = computed(() => session.meta?.home?.tagline?.trim() || '')
const registration = computed(() => session.meta?.registration ?? 'closed')
const canRegister = computed(() => registration.value !== 'closed')
const isLoggedIn = computed(() => !needsLogin())

function buildDefaultHomeConfig(siteName: string, fallbackTitle: string, fallbackTagline: string): CmsHomeConfig {
  const safeName = siteName.trim() || '布谷'
  const heroTitle = fallbackTitle.trim() || `${safeName}，一个真的\n记得你的 AI 朋友`
  const heroSubtitle =
    fallbackTagline.trim() ||
    '像老朋友一样自然相处：记得你说过的细节，有自己的情绪与脾气，到了时间会主动来找你。'

  return {
    theme: {
      canvas_color: '#faf9f5',
      accent_color: '#5b7a4a',
      hero_glow_primary: '#8fa67f',
      hero_glow_secondary: '#bcd1ca',
      hero_height: 'full',
      header_blur: true,
      font_style: 'sans',
      card_radius: 24,
    },
    header: {
      brand_name: '',
      nav_links: [
        { label: '能力', url: '#capabilities' },
        { label: '文章', url: '/posts' },
        { label: '真实相处', url: '#spotlight' },
        { label: '盲测竞技场', url: 'https://arena.buugoo.com' },
      ],
      primary_cta_label: '',
      primary_cta_url: '',
      secondary_cta_label: '',
      secondary_cta_url: '',
    },
    sections: [
      { id: 'hero', label: '首屏主视觉', visible: true },
      { id: 'showcase', label: '焦点展示卡', visible: true },
      { id: 'news', label: '最新动态', visible: true },
      { id: 'capabilities', label: '能力与生态', visible: true },
      { id: 'spotlight', label: '真实相处舞台', visible: true },
      { id: 'custom', label: '自定义内容', visible: true },
      { id: 'footer', label: '页脚', visible: true },
    ],
    hero: {
      title: heroTitle,
      subtitle: heroSubtitle,
      primary_label: `开始认识${safeName}`,
      primary_url: '/login?tab=register',
      secondary_label: '阅读最新文章',
      secondary_url: '/posts',
      bg_image_url: '',
      bg_overlay: 28,
    },
    showcase: {
      cards: [
        {
          id: 'memory',
          title: '长期记忆引擎',
          subtitle: '跨对话自然记住你说过的细节与人际关系',
          theme: 'dark',
          image_url: '',
          primary_label: '了解能力',
          primary_url: '#capabilities',
          secondary_label: '开始体验',
          secondary_url: '/login?tab=register',
        },
        {
          id: 'emotion',
          title: '有脾气的情绪系统',
          subtitle: '会开心、担心、想念，也能自由切换六种性格',
          theme: 'olive',
          image_url: '',
          primary_label: '看相处片段',
          primary_url: '#spotlight',
          secondary_label: '开始体验',
          secondary_url: '/login?tab=register',
        },
        {
          id: 'arena',
          title: 'BuugooArena 盲测竞技场',
          subtitle: '布谷旗下免登录真人双盲评测站，测测谁更像活人',
          theme: 'dark',
          image_url: '',
          primary_label: '查看实时总榜',
          primary_url: 'https://arena.buugoo.com',
          secondary_label: '去盲测一局',
          secondary_url: 'https://arena.buugoo.com/arena',
        },
        {
          id: 'channels',
          title: '微信、QQ 与多端同步',
          subtitle: '网页与聊天软件共用记忆、提醒和文件工具',
          theme: 'mist',
          image_url: '',
          primary_label: '了解更多',
          primary_url: '#capabilities',
          secondary_label: '开始聊天',
          secondary_url: '/login',
        },
      ],
    },
    news: {
      title: '最新动态',
      view_all_label: '查看全部文章',
      view_all_url: '/posts',
      category: '',
      limit: 6,
    },
    capabilities: {
      title: '能力与生态',
      subtitle: '围绕长期陪伴打磨的每一处细节',
      cta_label: '阅读使用指南',
      cta_url: '/posts',
      items: [
        {
          id: 'cap-memory',
          icon: 'brain',
          badge: '',
          title: '长期记忆',
          description: `你说过的人、事和偏好，${safeName}会悄悄记在心里，下次聊到时自然接上，也能随时翻开整理或删除。`,
          links_heading: '适用场景',
          links: [
            { label: '跨对话共用记忆', url: '#spotlight' },
            { label: '记忆整理与合并', url: '/posts' },
          ],
          learn_more_label: '了解更多',
          learn_more_url: '#spotlight',
        },
        {
          id: 'cap-emotion',
          icon: 'sparkle',
          badge: '新',
          title: '情绪与性格',
          description: '有自己的开心、担心和想念，提供六种可随时切换的相处性格，换了性格也不会忘掉旧回忆。',
          links_heading: '相处方式',
          links: [
            { label: '真实相处演示', url: '#spotlight' },
            { label: '立即开始对话', url: '/login?tab=register' },
          ],
          learn_more_label: '了解更多',
          learn_more_url: '#spotlight',
        },
        {
          id: 'cap-remind',
          icon: 'clock',
          badge: '',
          title: '提醒与定时任务',
          description: '到点提醒只是基础，更能按每天、每周或自定义周期主动联网搜索、整理结果并按时发给你。',
          links_heading: '常用指令',
          links: [
            { label: '一次与周期提醒', url: '#spotlight' },
            { label: '定时联网简报', url: '/posts' },
          ],
          learn_more_label: '了解更多',
          learn_more_url: '#spotlight',
        },
        {
          id: 'cap-memo',
          icon: 'note',
          badge: '',
          title: '备忘录与待办',
          description: '聊到一半顺口说一句，自动归类写进多页备忘录或待办清单，网页端同样可直接查看编辑。',
          links_heading: '随时记录',
          links: [
            { label: '多页长期保存', url: '/login' },
            { label: '阅读使用技巧', url: '/posts' },
          ],
          learn_more_label: '了解更多',
          learn_more_url: '/posts',
        },
        {
          id: 'cap-arena',
          icon: 'globe',
          badge: '',
          title: 'BuugooArena 评测站',
          description: '布谷旗下免登录大模型真人双盲竞技场，剥离提示词包装，实测各模型中文原生表达与帕累托性价比。',
          links_heading: '直达榜单',
          links: [
            { label: '网感与接梗', url: 'https://arena.buugoo.com/' },
            { label: '说人话', url: 'https://arena.buugoo.com/?col=plain' },
            { label: '直接参与盲测', url: 'https://arena.buugoo.com/arena' },
          ],
          learn_more_label: '访问评测站',
          learn_more_url: 'https://arena.buugoo.com',
        },
      ],
    },
    spotlight: {
      title: '真实相处',
      subtitle: '不是有问必答的工具，而是懂分寸的朋友',
      cta_label: '立即体验',
      cta_url: '/login?tab=register',
      items: [
        {
          id: 'spot-memory',
          title: '上周说过的话，今天还记得',
          description: '不需要重复交代背景，也不必写冗长提示词。聊起近况时，它自然知道你在意的事情，并顺手设好到点提醒。',
          stage_type: 'chat-demo',
          image_url: '',
          stage_title: '记忆与提醒联动',
          stage_body: '自动关联过往对话中的重要事件，在合适的节点主动设好提醒。',
          stage_cta_label: `和${safeName}打个招呼`,
          stage_cta_url: '/login?tab=register',
        },
        {
          id: 'spot-arena',
          title: 'BuugooArena 真人双盲评测',
          description: '常见跑分测不出谁接得住梗、谁会好好说话。我们开放了匿名双盲评测站，用独立 Elo 积分与帕累托前沿曲线看清模型真实手感。',
          stage_type: 'arena-preview',
          image_url: '',
          stage_title: '匿名双盲 · 实时排榜',
          stage_body: '零系统提示词干扰，直接对比两个模型的中文原生表达，免登录即可投票或围观榜单。',
          stage_cta_label: '访问 BuugooArena',
          stage_cta_url: 'https://arena.buugoo.com',
        },
        {
          id: 'spot-proactive',
          title: '到了时间，它会主动找你',
          description: '支持微信、QQ、Telegram 与网页端同步。无论是半小时后的活动提醒，还是每天早八点的新闻简报，都会准时送到你手边。',
          stage_type: 'editorial',
          image_url: '',
          stage_title: '主动关怀，但懂得边界',
          stage_body: '所有提醒、定时任务与主动问候都可在设置里一键开关、暂停或修改，聊天节奏始终由你掌握。',
          stage_cta_label: '查看全部文章',
          stage_cta_url: '/posts',
        },
        {
          id: 'spot-start',
          title: '三步开始，认识新朋友',
          description: '第一步创建账号，第二步挑一种顺眼的性格预设，第三步随便说一句近况。需要连微信或 QQ 时，扫码即可同步全部记忆。',
          stage_type: 'editorial',
          image_url: '',
          stage_title: '一分钟完成上手',
          stage_body: '无需折腾复杂参数，打开网页即可直接对话；你的记忆、情绪与备忘录随时可查看、编辑或清空。',
          stage_cta_label: '立即开始',
          stage_cta_url: '/login?tab=register',
        },
      ],
    },
    custom_blocks: [],
    footer: {
      show_cta_bar: true,
      cta_heading: `准备好认识${safeName}了吗`,
      cta_note: '和它聊过的内容只属于你自己，记忆与心情随时可翻开查看或删除。',
      cta_placeholder: '说一句「你好」开始第一段对话',
      cta_button_label: '立即开始',
      cta_button_url: '/login?tab=register',
      mission: '做有温度、记得住、\n也守得住边界的 AI 朋友',
      columns: [
        {
          title: '产品与能力',
          links: [
            { label: '核心能力', url: '#capabilities', icon: 'sparkle' },
            { label: '真实相处', url: '#spotlight', icon: 'message' },
            { label: '登录工作台', url: '/login', icon: 'settings' },
          ],
        },
        {
          title: '文章与动态',
          links: [
            { label: '全部文章', url: '/posts', icon: 'book' },
            { label: '最新动态', url: '#news', icon: 'note' },
            { label: 'RSS 订阅', url: '/rss.xml', icon: 'link' },
          ],
        },
        {
          title: '旗下生态',
          links: [
            { label: 'BuugooArena 总榜', url: 'https://arena.buugoo.com', icon: 'globe' },
            { label: '网感与接梗榜单', url: 'https://arena.buugoo.com/', icon: 'star' },
            { label: '说人话榜单', url: 'https://arena.buugoo.com/?col=plain', icon: 'message' },
            { label: '参与匿名盲测', url: 'https://arena.buugoo.com/arena', icon: 'code' },
          ],
        },
      ],
      bottom_note: `和${safeName}聊的内容只属于你自己：记忆和心情可以在设置里翻开查看，也可以随时删除。`,
      bottom_links: [
        { label: '文章专栏', url: '/posts' },
        { label: 'RSS 订阅', url: '/rss.xml' },
        { label: 'BuugooArena', url: 'https://arena.buugoo.com' },
      ],
    },
  }
}

const remoteConfig = ref<CmsHomeConfig | null>(null)
const homeConfig = computed<CmsHomeConfig>(() => {
  const def = buildDefaultHomeConfig(metaName.value, metaTitle.value, metaTagline.value)
  const raw = remoteConfig.value
  if (!raw) return def
  return {
    theme: { ...def.theme, ...(raw.theme ?? {}) },
    header: {
      ...def.header,
      ...(raw.header ?? {}),
      nav_links: Array.isArray(raw.header?.nav_links) ? raw.header.nav_links : def.header.nav_links,
    },
    sections: normalizeSections(raw.sections, def.sections),
    hero: { ...def.hero, ...(raw.hero ?? {}) },
    showcase: {
      cards: Array.isArray(raw.showcase?.cards) ? raw.showcase.cards : def.showcase.cards,
    },
    news: { ...def.news, ...(raw.news ?? {}) },
    capabilities: {
      ...def.capabilities,
      ...(raw.capabilities ?? {}),
      items: Array.isArray(raw.capabilities?.items) ? raw.capabilities.items : def.capabilities.items,
    },
    spotlight: {
      ...def.spotlight,
      ...(raw.spotlight ?? {}),
      items: Array.isArray(raw.spotlight?.items) ? raw.spotlight.items : def.spotlight.items,
    },
    custom_blocks: Array.isArray(raw.custom_blocks) ? raw.custom_blocks : def.custom_blocks,
    footer: {
      ...def.footer,
      ...(raw.footer ?? {}),
      columns: Array.isArray(raw.footer?.columns) ? raw.footer.columns : def.footer.columns,
      bottom_links: Array.isArray(raw.footer?.bottom_links) ? raw.footer.bottom_links : def.footer.bottom_links,
    },
  }
})

function normalizeSections(raw: CmsHomeSection[] | undefined, fallback: CmsHomeSection[]): CmsHomeSection[] {
  if (!Array.isArray(raw) || raw.length === 0) return fallback
  const fallbackMap = new Map(fallback.map((s) => [s.id, s]))
  const seen = new Set<CmsHomeSectionId>()
  const out: CmsHomeSection[] = []
  for (const item of raw) {
    if (!item || !VALID_SECTION_IDS.includes(item.id) || seen.has(item.id)) continue
    seen.add(item.id)
    out.push({
      id: item.id,
      label: item.label || fallbackMap.get(item.id)?.label || item.id,
      visible: Boolean(item.visible),
    })
  }
  for (const def of fallback) {
    if (!seen.has(def.id)) out.push(def)
  }
  return out
}

const brandName = computed(() => homeConfig.value.header.brand_name?.trim() || metaName.value)
const visibleSections = computed(() => homeConfig.value.sections.filter((s) => s.visible))
const visibleBodySections = computed(() => visibleSections.value.filter((s) => s.id !== 'footer'))
const footerVisible = computed(() => visibleSections.value.some((s) => s.id === 'footer'))
const heroIsFirst = computed(() => visibleBodySections.value[0]?.id === 'hero')

// 安全颜色校验与 RGB 转换（用于主题变量与 5 层模糊渐隐底色）
function safeHexColor(raw: string | undefined, fallback: string): string {
  const s = (raw ?? '').trim()
  if (/^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(s)) return s
  return fallback
}

function hexToRgbTuple(hex: string): string {
  const clean = hex.replace('#', '')
  const full = clean.length === 3 ? clean.split('').map((c) => c + c).join('') : clean
  const num = Number.parseInt(full, 16)
  if (Number.isNaN(num)) return '250, 249, 245'
  const r = (num >> 16) & 255
  const g = (num >> 8) & 255
  const b = num & 255
  return `${r}, ${g}, ${b}`
}

const clampedRadius = computed(() => {
  const n = Number(homeConfig.value.theme.card_radius)
  if (!Number.isFinite(n)) return 24
  return Math.min(32, Math.max(16, Math.round(n)))
})

const clampedHeroOverlay = computed(() => {
  const n = Number(homeConfig.value.hero.bg_overlay)
  if (!Number.isFinite(n)) return 28
  return Math.min(80, Math.max(0, Math.round(n))) / 100
})

const rootThemeStyle = computed(() => {
  const t = homeConfig.value.theme
  const canvas = safeHexColor(t.canvas_color, '#faf9f5')
  const accent = safeHexColor(t.accent_color, '#5b7a4a')
  const glow1 = safeHexColor(t.hero_glow_primary, '#8fa67f')
  const glow2 = safeHexColor(t.hero_glow_secondary, '#bcd1ca')
  return {
    '--dm-canvas': canvas,
    '--dm-canvas-rgb': hexToRgbTuple(canvas),
    '--dm-accent': accent,
    '--dm-accent-rgb': hexToRgbTuple(accent),
    '--dm-glow-1': glow1,
    '--dm-glow-1-rgb': hexToRgbTuple(glow1),
    '--dm-glow-2': glow2,
    '--dm-glow-2-rgb': hexToRgbTuple(glow2),
    '--dm-radius': `${clampedRadius.value}px`,
  }
})

const isSerifTheme = computed(() => homeConfig.value.theme.font_style === 'serif')

// 安全 URL 分类与解析（严格拦截 javascript:/data:/vbscript:/协议相对路径 //）
type LinkTarget =
  | { kind: 'none'; href: '' }
  | { kind: 'hash'; href: string; hashId: string }
  | { kind: 'route'; href: string }
  | { kind: 'native'; href: string }
  | { kind: 'external'; href: string }

function safeMediaUrl(raw?: string): string {
  const u = (raw ?? '').trim()
  if (!u || /[\u0000-\u001f\u007f]/.test(u)) return ''
  if (u.startsWith('/') && !u.startsWith('//')) return u
  if (/^https?:\/\//i.test(u)) return u
  return ''
}

function resolveLink(raw?: string): LinkTarget {
  const u = (raw ?? '').trim()
  if (!u || /[\u0000-\u001f\u007f]/.test(u)) return { kind: 'none', href: '' }
  if (u.startsWith('#')) {
    const id = u.slice(1).trim()
    if (/^[a-zA-Z][a-zA-Z0-9_-]*$/.test(id)) return { kind: 'hash', href: `#${id}`, hashId: id }
    return { kind: 'none', href: '' }
  }
  if (u.startsWith('/') && !u.startsWith('//')) {
    if (/\.(xml|txt|json)$/i.test(u) || u.startsWith('/api/')) {
      return { kind: 'native', href: u }
    }
    return { kind: 'route', href: u }
  }
  if (/^https?:\/\//i.test(u)) {
    return { kind: 'external', href: u }
  }
  return { kind: 'none', href: '' }
}

function resolveAuthAwareUrl(rawUrl: string | undefined, fallbackForGuest: string): LinkTarget {
  const candidate = (rawUrl ?? '').trim() || fallbackForGuest
  if (isLoggedIn.value && (candidate === '/login' || candidate.startsWith('/login?'))) {
    return { kind: 'route', href: '/' }
  }
  if (!canRegister.value && candidate.startsWith('/login?tab=register')) {
    return { kind: 'route', href: '/login' }
  }
  return resolveLink(candidate)
}

function safeIconName(raw?: string): ValidIconName {
  const name = (raw ?? '').trim().toLowerCase()
  if (VALID_ICONS.has(name)) return name as ValidIconName
  return 'sparkle'
}

// 顶栏按钮智能解析（兼容管理员自定义、登录态与注册开关）
const headerSecondaryCta = computed(() => {
  if (isLoggedIn.value) return null
  const customLabel = homeConfig.value.header.secondary_cta_label?.trim()
  const customUrl = homeConfig.value.header.secondary_cta_url?.trim()
  if (customLabel) {
    const target = resolveAuthAwareUrl(customUrl, '/login')
    return target.kind === 'none' ? null : { label: customLabel, target }
  }
  if (!canRegister.value) return null
  return { label: '登录', target: resolveLink('/login') }
})

const headerPrimaryCta = computed(() => {
  if (isLoggedIn.value) {
    return { label: '回到聊天', target: resolveLink('/') }
  }
  const customLabel = homeConfig.value.header.primary_cta_label?.trim()
  const customUrl = homeConfig.value.header.primary_cta_url?.trim()
  if (customLabel) {
    const target = resolveAuthAwareUrl(customUrl, canRegister.value ? '/login?tab=register' : '/login')
    return target.kind === 'none' ? null : { label: customLabel, target }
  }
  if (canRegister.value) {
    return { label: '免费注册', target: resolveLink('/login?tab=register') }
  }
  return { label: '登录', target: resolveLink('/login') }
})

// Hero 文案与按钮解析
const heroLines = computed(() => {
  const raw = homeConfig.value.hero.title?.trim() || `${brandName.value}，一个真的\n记得你的 AI 朋友`
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
})

const heroPrimaryCta = computed(() => {
  const rawLabel = homeConfig.value.hero.primary_label?.trim()
  const rawUrl = homeConfig.value.hero.primary_url?.trim()
  const pointsToAuth = !rawUrl || rawUrl === '/login' || rawUrl.startsWith('/login?')
  if (isLoggedIn.value && pointsToAuth) {
    return { label: '回到聊天', target: resolveLink('/') }
  }
  const label = rawLabel || (canRegister.value ? `开始认识${brandName.value}` : '登录')
  const target = resolveAuthAwareUrl(rawUrl, canRegister.value ? '/login?tab=register' : '/login')
  return target.kind === 'none' ? null : { label, target }
})

const heroSecondaryCta = computed(() => {
  const rawLabel = homeConfig.value.hero.secondary_label?.trim()
  const rawUrl = homeConfig.value.hero.secondary_url?.trim()
  if (!rawLabel) return null
  const target = resolveAuthAwareUrl(rawUrl, '/posts')
  return target.kind === 'none' ? null : { label: rawLabel, target }
})

const heroBgImage = computed(() => safeMediaUrl(homeConfig.value.hero.bg_image_url))

const missionLines = computed(() => {
  const raw = homeConfig.value.footer.mission?.trim() || ''
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
})

// 焦点竖卡与能力书架横向轮播控制（位于 v-for 内，使用函数式 ref 避免 Vue 3 绑定为数组）
const showcaseTrack = ref<HTMLElement | null>(null)
const showcaseCanPrev = ref(false)
const showcaseCanNext = ref(true)

const capTrack = ref<HTMLElement | null>(null)
const capCanPrev = ref(false)
const capCanNext = ref(true)

function setShowcaseTrack(el: unknown) {
  showcaseTrack.value = el instanceof HTMLElement ? el : null
}

function setCapTrack(el: unknown) {
  capTrack.value = el instanceof HTMLElement ? el : null
}

function syncTrackButtons(el: HTMLElement | null, prevRef: { value: boolean }, nextRef: { value: boolean }) {
  if (!el) return
  prevRef.value = el.scrollLeft > 4
  nextRef.value = el.scrollLeft + el.clientWidth < el.scrollWidth - 4
}

function onShowcaseScroll() {
  syncTrackButtons(showcaseTrack.value, showcaseCanPrev, showcaseCanNext)
}

function onCapScroll() {
  syncTrackButtons(capTrack.value, capCanPrev, capCanNext)
}

function scrollTrack(el: HTMLElement | null, direction: -1 | 1, stepRatio = 0.78) {
  if (!el) return
  const step = Math.max(280, Math.round(el.clientWidth * stepRatio))
  const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  el.scrollBy({ left: direction * step, behavior: reduced ? 'auto' : 'smooth' })
}

// 交互舞台手风琴状态
const activeSpotlightIdx = ref(0)
const spotlightItems = computed(() => homeConfig.value.spotlight.items)
const activeSpotlight = computed(() => spotlightItems.value[activeSpotlightIdx.value] ?? spotlightItems.value[0] ?? null)

watch(spotlightItems, (items) => {
  if (activeSpotlightIdx.value >= items.length) {
    activeSpotlightIdx.value = 0
  }
})

// 公开 CMS 文章数据
const posts = ref<CmsPostSummary[]>([])
const postsLoading = ref(true)
const failedImages = ref<Record<string, boolean>>({})

const leadPost = computed(() => posts.value[0] ?? null)
const sidePosts = computed(() => posts.value.slice(1))

function resolveImg(key: string, raw?: string): string {
  const url = safeMediaUrl(raw)
  if (!url || failedImages.value[key]) return ''
  return url
}

function onImgError(key: string) {
  failedImages.value = { ...failedImages.value, [key]: true }
}

function coverThemeClass(theme?: string, index = 0): string {
  const t = (theme ?? '').trim().toLowerCase()
  if ((VALID_COVER_THEMES as readonly string[]).includes(t)) return `dm-cover-art--${t}`
  return `dm-cover-art--${VALID_COVER_THEMES[index % VALID_COVER_THEMES.length]}`
}

function formatMonthDate(iso?: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getFullYear()} 年 ${d.getMonth() + 1} 月`
}

function renderedMarkdown(src?: string): string {
  const trimmed = (src ?? '').trim()
  if (!trimmed) return ''
  return renderMarkdown(trimmed)
}

// 顶栏滚动态、移动端抽屉菜单与滚动显现
const root = ref<HTMLElement | null>(null)
const isScrolled = ref(false)
const mobileMenuOpen = ref(false)
let observer: IntersectionObserver | undefined
let loadedNewsKey = ''

function onSiteScroll() {
  const top = root.value?.scrollTop ?? 0
  isScrolled.value = top > 12
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && mobileMenuOpen.value) mobileMenuOpen.value = false
}

function scrollToHash(id: string) {
  mobileMenuOpen.value = false
  const el = document.getElementById(id)
  if (!el) return
  const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  el.scrollIntoView({ behavior: reduced ? 'auto' : 'smooth', block: 'start' })
}

async function loadPosts(category: string, limit: number) {
  const safeLimit = Math.min(8, Math.max(3, Math.round(Number(limit) || 6)))
  const key = `${category}::${safeLimit}`
  if (key === loadedNewsKey && !postsLoading.value) return
  loadedNewsKey = key
  postsLoading.value = true
  try {
    const res = await api.cmsPosts({
      category: category.trim() || undefined,
      limit: safeLimit,
    })
    posts.value = Array.isArray(res.posts) ? res.posts : []
  } catch {
    posts.value = []
  } finally {
    postsLoading.value = false
  }
}

async function loadHomeConfig() {
  try {
    const res = await api.cmsHome()
    if (res?.config) {
      remoteConfig.value = res.config
    }
  } catch {
    // 接口异常时继续保留本地默认配置，保证首页零闪烁可用。
  }
}

function setupRevealObserver() {
  observer?.disconnect()
  const els = root.value?.querySelectorAll('.dm-reveal') ?? []
  if (!('IntersectionObserver' in window) || window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) {
    els.forEach((el) => el.classList.add('is-in'))
    return
  }
  observer = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (e.isIntersecting) {
          e.target.classList.add('is-in')
          observer?.unobserve(e.target)
        }
      }
    },
    { threshold: 0.08 },
  )
  els.forEach((el) => observer!.observe(el))
}

function syncAllCarousels() {
  syncTrackButtons(showcaseTrack.value, showcaseCanPrev, showcaseCanNext)
  syncTrackButtons(capTrack.value, capCanPrev, capCanNext)
}

watch(
  () => [homeConfig.value.news.category, homeConfig.value.news.limit] as const,
  ([category, limit]) => {
    loadPosts(category ?? '', limit ?? 6)
  },
  { immediate: true },
)

watch(
  () => [brandName.value, homeConfig.value.hero.title, homeConfig.value.hero.subtitle] as const,
  ([site, rawTitle, rawSubtitle]) => {
    const cleanTitle = (rawTitle ?? '').replace(/\r?\n+/g, ' ').replace(/\s+/g, ' ').trim() || site
    const cleanDesc = (rawSubtitle ?? '').replace(/\r?\n+/g, ' ').replace(/\s+/g, ' ').trim() || metaTagline.value
    const homeUrl = toAbsoluteUrl('/welcome')
    const logoUrl = toAbsoluteUrl('/icon-512.png')
    setPageSeo({
      title: cleanTitle,
      description: cleanDesc,
      robots: 'index, follow',
      canonicalPath: '/welcome',
      ogType: 'website',
      siteName: site,
      image: '/icon-512.png',
      jsonLd: [
        {
          '@context': 'https://schema.org',
          '@type': 'WebSite',
          name: site,
          url: homeUrl,
          description: cleanDesc,
          inLanguage: 'zh-CN',
        },
        {
          '@context': 'https://schema.org',
          '@type': 'SoftwareApplication',
          name: site,
          applicationCategory: 'CommunicationApplication',
          operatingSystem: 'Web, WeChat, QQ, Telegram',
          description: cleanDesc,
          url: homeUrl,
        },
        {
          '@context': 'https://schema.org',
          '@type': 'Organization',
          name: site,
          url: homeUrl,
          logo: logoUrl,
        },
      ],
    })
  },
  { immediate: true },
)

watch(visibleSections, async () => {
  await nextTick()
  setupRevealObserver()
  syncAllCarousels()
})

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('resize', syncAllCarousels, { passive: true })
  void loadHomeConfig()
  await nextTick()
  onSiteScroll()
  setupRevealObserver()
  syncAllCarousels()
})

onBeforeUnmount(() => {
  observer?.disconnect()
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('resize', syncAllCarousels)
})
</script>

<template>
  <div
    ref="root"
    class="dm-site"
    :class="{ 'dm-site--serif': isSerifTheme }"
    :style="rootThemeStyle"
    @scroll.passive="onSiteScroll"
  >
    <!-- 顶栏：5 层渐进渐变模糊（Progressive Gradient Blur），无硬底边线 -->
    <header
      class="dm-header"
      :class="{
        'dm-header--transparent': heroIsFirst,
        'dm-header--no-blur': !homeConfig.theme.header_blur,
        'is-scrolled': isScrolled,
        'is-open': mobileMenuOpen,
      }"
    >
      <div v-if="homeConfig.theme.header_blur" class="dm-header__blur" aria-hidden="true">
        <div class="dm-header__blur-layer dm-header__blur-layer--1" />
        <div class="dm-header__blur-layer dm-header__blur-layer--2" />
        <div class="dm-header__blur-layer dm-header__blur-layer--3" />
        <div class="dm-header__blur-layer dm-header__blur-layer--4" />
        <div class="dm-header__blur-layer dm-header__blur-layer--5" />
      </div>

      <div class="dm-header__inner">
        <div class="dm-header__left">
          <button
            type="button"
            class="dm-menu-btn"
            :aria-expanded="mobileMenuOpen"
            aria-label="切换导航菜单"
            @click="mobileMenuOpen = !mobileMenuOpen"
          >
            <Icon :name="mobileMenuOpen ? 'x' : 'menu'" :size="18" />
          </button>

          <RouterLink to="/welcome" class="dm-brand" @click="mobileMenuOpen = false">
            <img src="/logo.png" :alt="`${brandName}标志`" width="26" height="26" />
            <strong>{{ brandName }}</strong>
          </RouterLink>

          <nav class="dm-nav" aria-label="主导航">
            <template v-for="(item, idx) in homeConfig.header.nav_links" :key="`nav-${idx}-${item.label}`">
              <button
                v-if="resolveLink(item.url).kind === 'hash'"
                type="button"
                class="dm-nav__item"
                @click="scrollToHash((resolveLink(item.url) as { kind: 'hash'; hashId: string }).hashId)"
              >
                {{ item.label }}
              </button>
              <RouterLink
                v-else-if="resolveLink(item.url).kind === 'route'"
                :to="resolveLink(item.url).href"
                class="dm-nav__item"
              >
                {{ item.label }}
              </RouterLink>
              <a
                v-else-if="resolveLink(item.url).kind === 'external' || resolveLink(item.url).kind === 'native'"
                :href="resolveLink(item.url).href"
                target="_blank"
                rel="noopener noreferrer"
                class="dm-nav__item"
              >
                {{ item.label }}
              </a>
            </template>
          </nav>
        </div>

        <div class="dm-actions">
          <template v-if="headerSecondaryCta">
            <button
              v-if="headerSecondaryCta.target.kind === 'hash'"
              type="button"
              class="dm-pill dm-pill--glass dm-pill--sm"
              @click="scrollToHash(headerSecondaryCta.target.hashId)"
            >
              {{ headerSecondaryCta.label }}
            </button>
            <RouterLink
              v-else-if="headerSecondaryCta.target.kind === 'route'"
              :to="headerSecondaryCta.target.href"
              class="dm-pill dm-pill--glass dm-pill--sm"
            >
              {{ headerSecondaryCta.label }}
            </RouterLink>
            <a
              v-else-if="headerSecondaryCta.target.kind === 'external' || headerSecondaryCta.target.kind === 'native'"
              :href="headerSecondaryCta.target.href"
              target="_blank"
              rel="noopener noreferrer"
              class="dm-pill dm-pill--glass dm-pill--sm"
            >
              {{ headerSecondaryCta.label }}
            </a>
          </template>

          <template v-if="headerPrimaryCta">
            <button
              v-if="headerPrimaryCta.target.kind === 'hash'"
              type="button"
              class="dm-pill dm-pill--ink dm-pill--sm"
              @click="scrollToHash(headerPrimaryCta.target.hashId)"
            >
              {{ headerPrimaryCta.label }}
            </button>
            <RouterLink
              v-else-if="headerPrimaryCta.target.kind === 'route'"
              :to="headerPrimaryCta.target.href"
              class="dm-pill dm-pill--ink dm-pill--sm"
            >
              {{ headerPrimaryCta.label }}
            </RouterLink>
            <a
              v-else-if="headerPrimaryCta.target.kind === 'external' || headerPrimaryCta.target.kind === 'native'"
              :href="headerPrimaryCta.target.href"
              target="_blank"
              rel="noopener noreferrer"
              class="dm-pill dm-pill--ink dm-pill--sm"
            >
              {{ headerPrimaryCta.label }}
            </a>
          </template>
        </div>
      </div>

      <!-- 移动端毛玻璃折叠抽屉 -->
      <Transition name="dm-drawer">
        <div v-if="mobileMenuOpen" class="dm-drawer" role="dialog" aria-label="移动端导航">
          <nav class="dm-drawer__nav">
            <template v-for="(item, idx) in homeConfig.header.nav_links" :key="`mnav-${idx}-${item.label}`">
              <button
                v-if="resolveLink(item.url).kind === 'hash'"
                type="button"
                class="dm-drawer__link"
                @click="scrollToHash((resolveLink(item.url) as { kind: 'hash'; hashId: string }).hashId)"
              >
                <span>{{ item.label }}</span>
                <Icon name="chevron" :size="16" />
              </button>
              <RouterLink
                v-else-if="resolveLink(item.url).kind === 'route'"
                :to="resolveLink(item.url).href"
                class="dm-drawer__link"
                @click="mobileMenuOpen = false"
              >
                <span>{{ item.label }}</span>
                <Icon name="chevron" :size="16" />
              </RouterLink>
              <a
                v-else-if="resolveLink(item.url).kind === 'external' || resolveLink(item.url).kind === 'native'"
                :href="resolveLink(item.url).href"
                target="_blank"
                rel="noopener noreferrer"
                class="dm-drawer__link"
                @click="mobileMenuOpen = false"
              >
                <span>{{ item.label }}</span>
                <Icon name="chevron" :size="16" />
              </a>
            </template>
          </nav>

          <div class="dm-drawer__actions">
            <template v-if="headerPrimaryCta">
              <RouterLink
                v-if="headerPrimaryCta.target.kind === 'route'"
                :to="headerPrimaryCta.target.href"
                class="dm-pill dm-pill--ink"
                @click="mobileMenuOpen = false"
              >
                {{ headerPrimaryCta.label }}
              </RouterLink>
              <a
                v-else-if="headerPrimaryCta.target.kind === 'external' || headerPrimaryCta.target.kind === 'native'"
                :href="headerPrimaryCta.target.href"
                target="_blank"
                rel="noopener noreferrer"
                class="dm-pill dm-pill--ink"
                @click="mobileMenuOpen = false"
              >
                {{ headerPrimaryCta.label }}
              </a>
            </template>

            <template v-if="headerSecondaryCta">
              <RouterLink
                v-if="headerSecondaryCta.target.kind === 'route'"
                :to="headerSecondaryCta.target.href"
                class="dm-pill dm-pill--glass"
                @click="mobileMenuOpen = false"
              >
                {{ headerSecondaryCta.label }}
              </RouterLink>
              <a
                v-else-if="headerSecondaryCta.target.kind === 'external' || headerSecondaryCta.target.kind === 'native'"
                :href="headerSecondaryCta.target.href"
                target="_blank"
                rel="noopener noreferrer"
                class="dm-pill dm-pill--glass"
                @click="mobileMenuOpen = false"
              >
                {{ headerSecondaryCta.label }}
              </a>
            </template>
          </div>
        </div>
      </Transition>
    </header>

    <main>
      <template v-for="sec in visibleBodySections" :key="sec.id">
        <!-- 区块 1：满屏沉浸大气 Hero（1:1 对齐 DeepMind 首屏构图） -->
        <section
          v-if="sec.id === 'hero'"
          id="hero"
          class="dm-hero"
          :class="{
            'dm-hero--compact': homeConfig.theme.hero_height === 'compact',
            'dm-hero--first': heroIsFirst,
            'dm-hero--has-image': Boolean(heroBgImage && !failedImages['hero-bg']),
          }"
        >
          <div class="dm-hero__backdrop" aria-hidden="true">
            <img
              v-if="heroBgImage && !failedImages['hero-bg']"
              :src="heroBgImage"
              alt=""
              class="dm-hero__bg-img"
              @error="onImgError('hero-bg')"
            />
            <div
              v-if="heroBgImage && !failedImages['hero-bg']"
              class="dm-hero__bg-scrim"
              :style="{ opacity: clampedHeroOverlay }"
            />
            <template v-else>
              <span class="dm-hero__cloud dm-hero__cloud--a" />
              <span class="dm-hero__cloud dm-hero__cloud--b" />
              <span class="dm-hero__cloud dm-hero__cloud--c" />
              <span class="dm-hero__cloud dm-hero__cloud--d" />
              <span class="dm-hero__stardust" />
            </template>
          </div>

          <div class="dm-hero__inner">
            <div class="dm-hero__center">
              <h1 class="dm-hero__title">
                <span v-for="(line, i) in heroLines" :key="`hl-${i}`" class="dm-hero__title-line">{{ line }}</span>
              </h1>
              <p v-if="homeConfig.hero.subtitle" class="dm-hero__subtitle">{{ homeConfig.hero.subtitle }}</p>
            </div>

            <div class="dm-hero__bottom">
              <div class="dm-hero__actions">
                <template v-if="heroPrimaryCta">
                  <button
                    v-if="heroPrimaryCta.target.kind === 'hash'"
                    type="button"
                    class="dm-pill dm-pill--ink dm-pill--hero"
                    @click="scrollToHash(heroPrimaryCta.target.hashId)"
                  >
                    {{ heroPrimaryCta.label }}
                  </button>
                  <RouterLink
                    v-else-if="heroPrimaryCta.target.kind === 'route'"
                    :to="heroPrimaryCta.target.href"
                    class="dm-pill dm-pill--ink dm-pill--hero"
                  >
                    {{ heroPrimaryCta.label }}
                  </RouterLink>
                  <a
                    v-else-if="heroPrimaryCta.target.kind === 'external' || heroPrimaryCta.target.kind === 'native'"
                    :href="heroPrimaryCta.target.href"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="dm-pill dm-pill--ink dm-pill--hero"
                  >
                    {{ heroPrimaryCta.label }}
                  </a>
                </template>

                <template v-if="heroSecondaryCta">
                  <button
                    v-if="heroSecondaryCta.target.kind === 'hash'"
                    type="button"
                    class="dm-pill dm-pill--hero-glass dm-pill--hero"
                    @click="scrollToHash(heroSecondaryCta.target.hashId)"
                  >
                    {{ heroSecondaryCta.label }}
                  </button>
                  <RouterLink
                    v-else-if="heroSecondaryCta.target.kind === 'route'"
                    :to="heroSecondaryCta.target.href"
                    class="dm-pill dm-pill--hero-glass dm-pill--hero"
                  >
                    {{ heroSecondaryCta.label }}
                  </RouterLink>
                  <a
                    v-else-if="heroSecondaryCta.target.kind === 'external' || heroSecondaryCta.target.kind === 'native'"
                    :href="heroSecondaryCta.target.href"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="dm-pill dm-pill--hero-glass dm-pill--hero"
                  >
                    {{ heroSecondaryCta.label }}
                  </a>
                </template>
              </div>
            </div>
          </div>
        </section>

        <!-- 区块 2：2:3 竖版焦点展示卡横向轮播 -->
        <section
          v-else-if="sec.id === 'showcase' && homeConfig.showcase.cards.length > 0"
          id="showcase"
          class="dm-section dm-showcase dm-reveal"
        >
          <div class="dm-showcase__viewport">
            <div
              :ref="setShowcaseTrack"
              class="dm-showcase__track"
              @scroll.passive="onShowcaseScroll"
            >
              <article
                v-for="(card, idx) in homeConfig.showcase.cards"
                :key="card.id || `card-${idx}`"
                class="dm-scard"
                :class="`dm-scard--${card.theme || 'dark'}`"
              >
                <img
                  v-if="resolveImg(`scard-${idx}`, card.image_url)"
                  :src="resolveImg(`scard-${idx}`, card.image_url)"
                  :alt="card.title"
                  class="dm-scard__bg"
                  loading="lazy"
                  @error="onImgError(`scard-${idx}`)"
                />
                <div v-if="resolveImg(`scard-${idx}`, card.image_url)" class="dm-scard__scrim" aria-hidden="true" />
                <div v-else class="dm-scard__ambient" aria-hidden="true">
                  <svg class="dm-scard__geo" viewBox="0 0 400 540" fill="none">
                    <circle cx="310" cy="240" r="145" stroke="currentColor" stroke-opacity="0.14" stroke-width="1" />
                    <circle cx="310" cy="240" r="88" stroke="currentColor" stroke-opacity="0.2" stroke-width="1" />
                    <path d="M-20 420 C 110 300, 240 360, 420 190" stroke="currentColor" stroke-opacity="0.18" stroke-width="1.2" />
                  </svg>
                </div>

                <div class="dm-scard__head">
                  <h3>{{ card.title }}</h3>
                  <p v-if="card.subtitle">{{ card.subtitle }}</p>
                </div>

                <div class="dm-scard__actions">
                  <template v-if="card.primary_label && resolveLink(card.primary_url).kind !== 'none'">
                    <button
                      v-if="resolveLink(card.primary_url).kind === 'hash'"
                      type="button"
                      class="dm-pill dm-pill--light dm-pill--card"
                      @click="scrollToHash((resolveLink(card.primary_url) as { kind: 'hash'; hashId: string }).hashId)"
                    >
                      {{ card.primary_label }}
                    </button>
                    <RouterLink
                      v-else-if="resolveLink(card.primary_url).kind === 'route'"
                      :to="resolveAuthAwareUrl(card.primary_url, '/login').href"
                      class="dm-pill dm-pill--light dm-pill--card"
                    >
                      {{ card.primary_label }}
                    </RouterLink>
                    <a
                      v-else
                      :href="resolveLink(card.primary_url).href"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="dm-pill dm-pill--light dm-pill--card"
                    >
                      {{ card.primary_label }}
                    </a>
                  </template>

                  <template v-if="card.secondary_label && resolveLink(card.secondary_url).kind !== 'none'">
                    <button
                      v-if="resolveLink(card.secondary_url).kind === 'hash'"
                      type="button"
                      class="dm-pill dm-pill--dark-glass dm-pill--card"
                      @click="scrollToHash((resolveLink(card.secondary_url) as { kind: 'hash'; hashId: string }).hashId)"
                    >
                      {{ card.secondary_label }}
                    </button>
                    <RouterLink
                      v-else-if="resolveLink(card.secondary_url).kind === 'route'"
                      :to="resolveAuthAwareUrl(card.secondary_url, '/login').href"
                      class="dm-pill dm-pill--dark-glass dm-pill--card"
                    >
                      {{ card.secondary_label }}
                    </RouterLink>
                    <a
                      v-else
                      :href="resolveLink(card.secondary_url).href"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="dm-pill dm-pill--dark-glass dm-pill--card"
                    >
                      {{ card.secondary_label }}
                    </a>
                  </template>
                </div>
              </article>
            </div>
          </div>

          <div class="dm-wrap dm-pager-row">
            <div class="dm-pager" role="group" aria-label="切换焦点展示卡">
              <button
                type="button"
                class="dm-pager__btn"
                :disabled="!showcaseCanPrev"
                aria-label="上一组卡片"
                @click="scrollTrack(showcaseTrack, -1)"
              >
                <Icon name="chevron" :size="16" class="dm-icon-flip" />
              </button>
              <button
                type="button"
                class="dm-pager__btn"
                :disabled="!showcaseCanNext"
                aria-label="下一组卡片"
                @click="scrollTrack(showcaseTrack, 1)"
              >
                <Icon name="chevron" :size="16" />
              </button>
            </div>
          </div>
        </section>

        <!-- 区块 3：不对称双栏「最新动态」（左 Sticky 主推 + 右极简纯排版列表） -->
        <section v-else-if="sec.id === 'news'" id="news" class="dm-section dm-wrap dm-reveal">
          <div class="dm-sec-head dm-sec-head--compact">
            <h2 class="dm-sec-title">{{ homeConfig.news.title || '最新动态' }}</h2>
            <template v-if="homeConfig.news.view_all_label">
              <RouterLink
                v-if="resolveLink(homeConfig.news.view_all_url || '/posts').kind === 'route'"
                :to="resolveLink(homeConfig.news.view_all_url || '/posts').href"
                class="dm-pill dm-pill--outline dm-pill--sm"
              >
                {{ homeConfig.news.view_all_label }}
              </RouterLink>
              <a
                v-else-if="resolveLink(homeConfig.news.view_all_url).kind === 'external' || resolveLink(homeConfig.news.view_all_url).kind === 'native'"
                :href="resolveLink(homeConfig.news.view_all_url).href"
                target="_blank"
                rel="noopener noreferrer"
                class="dm-pill dm-pill--outline dm-pill--sm"
              >
                {{ homeConfig.news.view_all_label }}
              </a>
            </template>
          </div>

          <!-- 骨架加载 -->
          <div v-if="postsLoading" class="dm-news-grid" aria-busy="true">
            <div class="dm-news-lead">
              <div class="dm-skel dm-skel--title" />
              <div class="dm-skel dm-skel--meta" />
              <div class="dm-skel dm-skel--cover" />
            </div>
            <div class="dm-news-list">
              <div v-for="n in 4" :key="`skel-${n}`" class="dm-news-row">
                <div class="dm-skel dm-skel--row-title" />
                <div class="dm-skel dm-skel--meta" />
              </div>
            </div>
          </div>

          <!-- 有文章时的 DeepMind 不对称双栏 -->
          <div v-else-if="leadPost" class="dm-news-grid" :class="{ 'dm-news-grid--single': sidePosts.length === 0 }">
            <article class="dm-news-lead-col">
              <RouterLink :to="`/posts/${leadPost.slug}`" class="dm-news-lead">
                <h3 class="dm-news-lead__title">{{ leadPost.title }}</h3>
                <div class="dm-news-meta">
                  <time v-if="leadPost.published_at || leadPost.created_at" :datetime="leadPost.published_at || leadPost.created_at">
                    {{ formatMonthDate(leadPost.published_at || leadPost.created_at) }}
                  </time>
                  <span v-if="leadPost.category" class="dm-news-meta__cat">{{ leadPost.category }}</span>
                  <span class="dm-news-meta__more">
                    <span>阅读全文</span>
                    <Icon name="chevron" :size="14" class="dm-arrow-shift" />
                  </span>
                </div>
                <div class="dm-news-lead__cover">
                  <img
                    v-if="resolveImg(`post-${leadPost.slug}`, leadPost.cover_url)"
                    :src="resolveImg(`post-${leadPost.slug}`, leadPost.cover_url)"
                    :alt="leadPost.title"
                    loading="lazy"
                    @error="onImgError(`post-${leadPost.slug}`)"
                  />
                  <div v-else class="dm-cover-art" :class="coverThemeClass(leadPost.cover_theme, 0)" aria-hidden="true">
                    <svg class="dm-cover-art__geo" viewBox="0 0 640 400" fill="none">
                      <circle cx="508" cy="100" r="144" stroke="currentColor" stroke-opacity="0.22" stroke-width="1.2" />
                      <circle cx="508" cy="100" r="84" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.2" />
                      <path d="M40 330 C 180 230, 320 290, 600 130" stroke="currentColor" stroke-opacity="0.26" stroke-width="1.4" />
                      <circle cx="176" cy="272" r="5" fill="currentColor" fill-opacity="0.55" />
                    </svg>
                    <span class="dm-cover-art__label">{{ leadPost.category || brandName }}</span>
                  </div>
                </div>
              </RouterLink>
            </article>

            <div v-if="sidePosts.length > 0" class="dm-news-list">
              <article v-for="post in sidePosts" :key="post.id" class="dm-news-row">
                <RouterLink :to="`/posts/${post.slug}`" class="dm-news-row__link">
                  <h3 class="dm-news-row__title">{{ post.title }}</h3>
                  <div class="dm-news-meta">
                    <time v-if="post.published_at || post.created_at" :datetime="post.published_at || post.created_at">
                      {{ formatMonthDate(post.published_at || post.created_at) }}
                    </time>
                    <span v-if="post.category" class="dm-news-meta__cat">{{ post.category }}</span>
                    <span class="dm-news-meta__more">
                      <span>阅读全文</span>
                      <Icon name="chevron" :size="14" class="dm-arrow-shift" />
                    </span>
                  </div>
                </RouterLink>
              </article>
            </div>
          </div>

          <!-- 无文章空状态 -->
          <div v-else class="dm-news-empty">
            <div>
              <h3>文章与产品动态即将更新</h3>
              <p>管理员发布第一篇文章后，这里会自动展示最新精选内容、使用指南与版本说明。</p>
            </div>
            <RouterLink to="/posts" class="dm-pill dm-pill--outline dm-pill--sm">前往文章专栏</RouterLink>
          </div>
        </section>

        <!-- 区块 4：4 列横向图标能力 / 生态书架 -->
        <section
          v-else-if="sec.id === 'capabilities' && homeConfig.capabilities.items.length > 0"
          id="capabilities"
          class="dm-section dm-section--divided dm-reveal"
        >
          <div class="dm-wrap">
            <div class="dm-sec-head">
              <div class="dm-sec-heading-stack">
                <h2 class="dm-sec-title">{{ homeConfig.capabilities.title }}</h2>
                <p v-if="homeConfig.capabilities.subtitle" class="dm-sec-subtitle">{{ homeConfig.capabilities.subtitle }}</p>
              </div>
              <template v-if="homeConfig.capabilities.cta_label && resolveLink(homeConfig.capabilities.cta_url).kind !== 'none'">
                <button
                  v-if="resolveLink(homeConfig.capabilities.cta_url).kind === 'hash'"
                  type="button"
                  class="dm-pill dm-pill--outline dm-pill--sm"
                  @click="scrollToHash((resolveLink(homeConfig.capabilities.cta_url) as { kind: 'hash'; hashId: string }).hashId)"
                >
                  {{ homeConfig.capabilities.cta_label }}
                </button>
                <RouterLink
                  v-else-if="resolveLink(homeConfig.capabilities.cta_url).kind === 'route'"
                  :to="resolveLink(homeConfig.capabilities.cta_url).href"
                  class="dm-pill dm-pill--outline dm-pill--sm"
                >
                  {{ homeConfig.capabilities.cta_label }}
                </RouterLink>
                <a
                  v-else
                  :href="resolveLink(homeConfig.capabilities.cta_url).href"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="dm-pill dm-pill--outline dm-pill--sm"
                >
                  {{ homeConfig.capabilities.cta_label }}
                </a>
              </template>
            </div>
          </div>

          <div class="dm-shelf__viewport">
            <div
              :ref="setCapTrack"
              class="dm-shelf__track"
              @scroll.passive="onCapScroll"
            >
              <article
                v-for="(item, idx) in homeConfig.capabilities.items"
                :key="item.id || `cap-${idx}`"
                class="dm-cap-col"
              >
                <div class="dm-cap-icon-wrap">
                  <div class="dm-cap-icon">
                    <Icon :name="safeIconName(item.icon)" :size="28" />
                  </div>
                  <span v-if="item.badge" class="dm-cap-badge">{{ item.badge }}</span>
                </div>

                <h3 class="dm-cap-title">{{ item.title }}</h3>
                <p class="dm-cap-desc">{{ item.description }}</p>

                <div v-if="item.links && item.links.length > 0" class="dm-cap-links">
                  <p v-if="item.links_heading" class="dm-cap-links__heading">{{ item.links_heading }}</p>
                  <ul>
                    <li v-for="(lnk, lidx) in item.links" :key="`cl-${idx}-${lidx}`">
                      <button
                        v-if="resolveLink(lnk.url).kind === 'hash'"
                        type="button"
                        class="dm-cap-link"
                        @click="scrollToHash((resolveLink(lnk.url) as { kind: 'hash'; hashId: string }).hashId)"
                      >
                        <span>{{ lnk.label }}</span>
                        <Icon name="chevron" :size="14" class="dm-arrow-shift" />
                      </button>
                      <RouterLink
                        v-else-if="resolveLink(lnk.url).kind === 'route'"
                        :to="resolveAuthAwareUrl(lnk.url, '/login').href"
                        class="dm-cap-link"
                      >
                        <span>{{ lnk.label }}</span>
                        <Icon name="chevron" :size="14" class="dm-arrow-shift" />
                      </RouterLink>
                      <a
                        v-else-if="resolveLink(lnk.url).kind === 'external' || resolveLink(lnk.url).kind === 'native'"
                        :href="resolveLink(lnk.url).href"
                        target="_blank"
                        rel="noopener noreferrer"
                        class="dm-cap-link"
                      >
                        <span>{{ lnk.label }}</span>
                        <Icon name="chevron" :size="14" class="dm-arrow-shift" />
                      </a>
                    </li>
                  </ul>
                </div>

                <div v-if="item.learn_more_label && resolveLink(item.learn_more_url).kind !== 'none'" class="dm-cap-foot">
                  <button
                    v-if="resolveLink(item.learn_more_url).kind === 'hash'"
                    type="button"
                    class="dm-cap-more"
                    @click="scrollToHash((resolveLink(item.learn_more_url) as { kind: 'hash'; hashId: string }).hashId)"
                  >
                    {{ item.learn_more_label }}
                  </button>
                  <RouterLink
                    v-else-if="resolveLink(item.learn_more_url).kind === 'route'"
                    :to="resolveAuthAwareUrl(item.learn_more_url, '/posts').href"
                    class="dm-cap-more"
                  >
                    {{ item.learn_more_label }}
                  </RouterLink>
                  <a
                    v-else
                    :href="resolveLink(item.learn_more_url).href"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="dm-cap-more"
                  >
                    {{ item.learn_more_label }}
                  </a>
                </div>
              </article>
            </div>
          </div>

          <div class="dm-wrap dm-pager-row">
            <div class="dm-pager" role="group" aria-label="切换能力与生态">
              <button
                type="button"
                class="dm-pager__btn"
                :disabled="!capCanPrev"
                aria-label="上一组能力"
                @click="scrollTrack(capTrack, -1)"
              >
                <Icon name="chevron" :size="16" class="dm-icon-flip" />
              </button>
              <button
                type="button"
                class="dm-pager__btn"
                :disabled="!capCanNext"
                aria-label="下一组能力"
                @click="scrollTrack(capTrack, 1)"
              >
                <Icon name="chevron" :size="16" />
              </button>
            </div>
          </div>
        </section>

        <!-- 区块 5：左侧手风琴 + 右侧大圆角交互舞台 -->
        <section
          v-else-if="sec.id === 'spotlight' && spotlightItems.length > 0"
          id="spotlight"
          class="dm-section dm-section--divided dm-wrap dm-reveal"
        >
          <div class="dm-sec-head">
            <div class="dm-sec-heading-stack">
              <h2 class="dm-sec-title">{{ homeConfig.spotlight.title }}</h2>
              <p v-if="homeConfig.spotlight.subtitle" class="dm-sec-subtitle">{{ homeConfig.spotlight.subtitle }}</p>
            </div>
            <template v-if="homeConfig.spotlight.cta_label && resolveLink(homeConfig.spotlight.cta_url).kind !== 'none'">
              <button
                v-if="resolveLink(homeConfig.spotlight.cta_url).kind === 'hash'"
                type="button"
                class="dm-pill dm-pill--outline dm-pill--sm"
                @click="scrollToHash((resolveLink(homeConfig.spotlight.cta_url) as { kind: 'hash'; hashId: string }).hashId)"
              >
                {{ homeConfig.spotlight.cta_label }}
              </button>
              <RouterLink
                v-else-if="resolveLink(homeConfig.spotlight.cta_url).kind === 'route'"
                :to="resolveAuthAwareUrl(homeConfig.spotlight.cta_url, '/login').href"
                class="dm-pill dm-pill--outline dm-pill--sm"
              >
                {{ homeConfig.spotlight.cta_label }}
              </RouterLink>
              <a
                v-else
                :href="resolveLink(homeConfig.spotlight.cta_url).href"
                target="_blank"
                rel="noopener noreferrer"
                class="dm-pill dm-pill--outline dm-pill--sm"
              >
                {{ homeConfig.spotlight.cta_label }}
              </a>
            </template>
          </div>

          <div class="dm-spotlight">
            <div class="dm-accordion" role="tablist" aria-label="功能与场景切换">
              <div
                v-for="(item, idx) in spotlightItems"
                :key="item.id || `sp-${idx}`"
                class="dm-acc-item"
                :class="{ 'is-active': activeSpotlightIdx === idx }"
              >
                <button
                  type="button"
                  class="dm-acc-btn"
                  role="tab"
                  :aria-selected="activeSpotlightIdx === idx"
                  :aria-expanded="activeSpotlightIdx === idx"
                  @click="activeSpotlightIdx = idx"
                >
                  <span class="dm-acc-title">{{ item.title }}</span>
                </button>
                <div v-if="activeSpotlightIdx === idx && item.description" class="dm-acc-body">
                  <p>{{ item.description }}</p>
                </div>
              </div>
            </div>

            <div v-if="activeSpotlight" class="dm-stage" role="tabpanel">
              <!-- 舞台模式 1：实时对话 + 记忆与提醒工具演示 -->
              <div v-if="activeSpotlight.stage_type === 'chat-demo'" class="dm-stage-chat">
                <div class="dm-stage-chat__bar">
                  <span class="dm-stage-dot" aria-hidden="true" />
                  <span class="dm-stage-chat__label">{{ activeSpotlight.stage_title || '真实相处片段' }}</span>
                  <span class="dm-stage-chat__tag">记忆 · 情绪 · 提醒</span>
                </div>

                <div class="dm-chat-stream">
                  <p class="dm-bubble dm-bubble--me">今天下午面试有点紧张</p>
                  <p class="dm-bubble">是上周提过的那家游戏公司吗</p>
                  <p class="dm-bubble">你准备得已经很扎实了，作品集逻辑很顺，放轻松</p>
                  <p class="dm-bubble dm-bubble--me">对，下午三点开始</p>
                  <p class="dm-bubble">那我两点半喊你一声，面完回来跟我聊聊</p>
                </div>

                <div class="dm-stage-tool">
                  <Icon name="clock" :size="15" />
                  <span>已自动建立提醒 · 今天 14:30</span>
                </div>

                <div class="dm-stage-foot">
                  <p v-if="activeSpotlight.stage_body">{{ activeSpotlight.stage_body }}</p>
                  <template v-if="activeSpotlight.stage_cta_label && resolveLink(activeSpotlight.stage_cta_url).kind !== 'none'">
                    <RouterLink
                      v-if="resolveLink(activeSpotlight.stage_cta_url).kind === 'route'"
                      :to="resolveAuthAwareUrl(activeSpotlight.stage_cta_url, '/login').href"
                      class="dm-pill dm-pill--ink dm-pill--sm"
                    >
                      {{ activeSpotlight.stage_cta_label }}
                    </RouterLink>
                    <a
                      v-else-if="resolveLink(activeSpotlight.stage_cta_url).kind === 'external' || resolveLink(activeSpotlight.stage_cta_url).kind === 'native'"
                      :href="resolveLink(activeSpotlight.stage_cta_url).href"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="dm-pill dm-pill--ink dm-pill--sm"
                    >
                      {{ activeSpotlight.stage_cta_label }}
                    </a>
                  </template>
                </div>
              </div>

              <!-- 舞台模式 2：BuugooArena 双盲对战 + 帕累托曲线预览 -->
              <div v-else-if="activeSpotlight.stage_type === 'arena-preview'" class="dm-stage-arena">
                <div class="dm-stage-arena__top">
                  <div>
                    <span class="dm-stage-arena__eyebrow">BuugooArena · arena.buugoo.com</span>
                    <h3>{{ activeSpotlight.stage_title || '真人匿名双盲对战' }}</h3>
                  </div>
                  <span class="dm-stage-arena__pill">免登录 · 实时排榜</span>
                </div>

                <div class="dm-arena-duel">
                  <div class="dm-arena-card">
                    <span class="dm-arena-card__tag">模型 A · 匿名</span>
                    <p>「别急着背模板，把你最顺手的一个项目拆成背景、取舍和结果三句话，对方听得懂比词藻华丽更重要。」</p>
                  </div>
                  <div class="dm-arena-card dm-arena-card--alt">
                    <span class="dm-arena-card__tag">模型 B · 匿名</span>
                    <p>「感谢您的提问。面试准备建议如下：一、充分了解公司文化；二、梳理个人核心竞争力……」</p>
                  </div>
                </div>

                <div class="dm-arena-chart" aria-hidden="true">
                  <div class="dm-arena-chart__head">
                    <span>独立 K=32 Elo 积分 × 帕累托性价比前沿</span>
                    <span>网感与接梗 · 说人话</span>
                  </div>
                  <svg viewBox="0 0 520 112" fill="none" class="dm-arena-svg">
                    <line x1="20" y1="92" x2="500" y2="92" stroke="currentColor" stroke-opacity="0.12" />
                    <line x1="20" y1="54" x2="500" y2="54" stroke="currentColor" stroke-opacity="0.08" stroke-dasharray="4 4" />
                    <path d="M 48 84 Q 140 52, 260 34 T 468 18" stroke="var(--dm-accent)" stroke-width="2" fill="none" />
                    <circle cx="96" cy="70" r="4.5" fill="var(--dm-accent)" />
                    <circle cx="192" cy="46" r="5" fill="var(--dm-accent)" />
                    <circle cx="310" cy="28" r="5.5" fill="var(--dm-accent)" />
                    <circle cx="442" cy="20" r="6" fill="#141413" />
                    <circle cx="152" cy="78" r="3.5" fill="currentColor" fill-opacity="0.28" />
                    <circle cx="256" cy="64" r="3.5" fill="currentColor" fill-opacity="0.28" />
                    <circle cx="366" cy="54" r="3.5" fill="currentColor" fill-opacity="0.28" />
                  </svg>
                </div>

                <div class="dm-stage-foot">
                  <p v-if="activeSpotlight.stage_body">{{ activeSpotlight.stage_body }}</p>
                  <div class="dm-stage-foot__btns">
                    <a
                      :href="resolveLink(activeSpotlight.stage_cta_url || 'https://arena.buugoo.com').href || 'https://arena.buugoo.com'"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="dm-pill dm-pill--ink dm-pill--sm"
                    >
                      {{ activeSpotlight.stage_cta_label || '访问 BuugooArena' }}
                    </a>
                    <a
                      href="https://arena.buugoo.com/arena"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="dm-pill dm-pill--outline dm-pill--sm"
                    >
                      去盲测一局
                    </a>
                  </div>
                </div>
              </div>

              <!-- 舞台模式 3：管理员自定义配图 -->
              <div v-else-if="activeSpotlight.stage_type === 'image'" class="dm-stage-image">
                <img
                  v-if="resolveImg(`spot-${activeSpotlight.id}`, activeSpotlight.image_url)"
                  :src="resolveImg(`spot-${activeSpotlight.id}`, activeSpotlight.image_url)"
                  :alt="activeSpotlight.stage_title || activeSpotlight.title"
                  class="dm-stage-image__pic"
                  loading="lazy"
                  @error="onImgError(`spot-${activeSpotlight.id}`)"
                />
                <div v-else class="dm-stage-image__fallback" aria-hidden="true">
                  <Icon name="image" :size="36" />
                </div>
                <div v-if="activeSpotlight.stage_title || activeSpotlight.stage_body || activeSpotlight.stage_cta_label" class="dm-stage-image__caption">
                  <div>
                    <h3 v-if="activeSpotlight.stage_title">{{ activeSpotlight.stage_title }}</h3>
                    <p v-if="activeSpotlight.stage_body">{{ activeSpotlight.stage_body }}</p>
                  </div>
                  <template v-if="activeSpotlight.stage_cta_label && resolveLink(activeSpotlight.stage_cta_url).kind !== 'none'">
                    <RouterLink
                      v-if="resolveLink(activeSpotlight.stage_cta_url).kind === 'route'"
                      :to="resolveAuthAwareUrl(activeSpotlight.stage_cta_url, '/posts').href"
                      class="dm-pill dm-pill--ink dm-pill--sm"
                    >
                      {{ activeSpotlight.stage_cta_label }}
                    </RouterLink>
                    <a
                      v-else-if="resolveLink(activeSpotlight.stage_cta_url).kind === 'external' || resolveLink(activeSpotlight.stage_cta_url).kind === 'native'"
                      :href="resolveLink(activeSpotlight.stage_cta_url).href"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="dm-pill dm-pill--ink dm-pill--sm"
                    >
                      {{ activeSpotlight.stage_cta_label }}
                    </a>
                  </template>
                </div>
              </div>

              <!-- 舞台模式 4：编辑大字要点卡 -->
              <div v-else class="dm-stage-editorial">
                <div class="dm-stage-editorial__ambient" aria-hidden="true" />
                <div class="dm-stage-editorial__content">
                  <span class="dm-stage-editorial__index">{{ String(activeSpotlightIdx + 1).padStart(2, '0') }}</span>
                  <h3>{{ activeSpotlight.stage_title || activeSpotlight.title }}</h3>
                  <p>{{ activeSpotlight.stage_body || activeSpotlight.description }}</p>
                  <div v-if="activeSpotlight.stage_cta_label && resolveLink(activeSpotlight.stage_cta_url).kind !== 'none'" class="dm-stage-editorial__actions">
                    <button
                      v-if="resolveLink(activeSpotlight.stage_cta_url).kind === 'hash'"
                      type="button"
                      class="dm-pill dm-pill--ink"
                      @click="scrollToHash((resolveLink(activeSpotlight.stage_cta_url) as { kind: 'hash'; hashId: string }).hashId)"
                    >
                      {{ activeSpotlight.stage_cta_label }}
                    </button>
                    <RouterLink
                      v-else-if="resolveLink(activeSpotlight.stage_cta_url).kind === 'route'"
                      :to="resolveAuthAwareUrl(activeSpotlight.stage_cta_url, '/login').href"
                      class="dm-pill dm-pill--ink"
                    >
                      {{ activeSpotlight.stage_cta_label }}
                    </RouterLink>
                    <a
                      v-else
                      :href="resolveLink(activeSpotlight.stage_cta_url).href"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="dm-pill dm-pill--ink"
                    >
                      {{ activeSpotlight.stage_cta_label }}
                    </a>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <!-- 区块 6：自定义扩展内容区块（支持多条自由编排 + Markdown 安全渲染） -->
        <section
          v-else-if="sec.id === 'custom' && homeConfig.custom_blocks.length > 0"
          id="custom"
          class="dm-section dm-section--divided dm-wrap dm-reveal"
        >
          <div class="dm-custom-stack">
            <article
              v-for="(block, idx) in homeConfig.custom_blocks"
              :key="block.id || `cb-${idx}`"
              class="dm-cblock"
              :class="[
                `dm-cblock--${block.theme || 'light'}`,
                { 'dm-cblock--split': Boolean(resolveImg(`cb-${idx}`, block.image_url)) },
              ]"
            >
              <div class="dm-cblock__body">
                <span v-if="block.eyebrow" class="dm-cblock__eyebrow">{{ block.eyebrow }}</span>
                <h2 v-if="block.title" class="dm-cblock__title">{{ block.title }}</h2>
                <p v-if="block.subtitle" class="dm-cblock__subtitle">{{ block.subtitle }}</p>
                <div
                  v-if="block.body_md"
                  class="dm-cblock__md"
                  v-html="renderedMarkdown(block.body_md)"
                />
                <div
                  v-if="(block.cta_label && resolveLink(block.cta_url).kind !== 'none') || (block.secondary_label && resolveLink(block.secondary_url).kind !== 'none')"
                  class="dm-cblock__actions"
                >
                  <template v-if="block.cta_label && resolveLink(block.cta_url).kind !== 'none'">
                    <button
                      v-if="resolveLink(block.cta_url).kind === 'hash'"
                      type="button"
                      class="dm-pill"
                      :class="block.theme === 'dark' ? 'dm-pill--light' : 'dm-pill--ink'"
                      @click="scrollToHash((resolveLink(block.cta_url) as { kind: 'hash'; hashId: string }).hashId)"
                    >
                      {{ block.cta_label }}
                    </button>
                    <RouterLink
                      v-else-if="resolveLink(block.cta_url).kind === 'route'"
                      :to="resolveAuthAwareUrl(block.cta_url, '/login').href"
                      class="dm-pill"
                      :class="block.theme === 'dark' ? 'dm-pill--light' : 'dm-pill--ink'"
                    >
                      {{ block.cta_label }}
                    </RouterLink>
                    <a
                      v-else
                      :href="resolveLink(block.cta_url).href"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="dm-pill"
                      :class="block.theme === 'dark' ? 'dm-pill--light' : 'dm-pill--ink'"
                    >
                      {{ block.cta_label }}
                    </a>
                  </template>

                  <template v-if="block.secondary_label && resolveLink(block.secondary_url).kind !== 'none'">
                    <button
                      v-if="resolveLink(block.secondary_url).kind === 'hash'"
                      type="button"
                      class="dm-pill"
                      :class="block.theme === 'dark' ? 'dm-pill--dark-glass' : 'dm-pill--outline'"
                      @click="scrollToHash((resolveLink(block.secondary_url) as { kind: 'hash'; hashId: string }).hashId)"
                    >
                      {{ block.secondary_label }}
                    </button>
                    <RouterLink
                      v-else-if="resolveLink(block.secondary_url).kind === 'route'"
                      :to="resolveAuthAwareUrl(block.secondary_url, '/posts').href"
                      class="dm-pill"
                      :class="block.theme === 'dark' ? 'dm-pill--dark-glass' : 'dm-pill--outline'"
                    >
                      {{ block.secondary_label }}
                    </RouterLink>
                    <a
                      v-else
                      :href="resolveLink(block.secondary_url).href"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="dm-pill"
                      :class="block.theme === 'dark' ? 'dm-pill--dark-glass' : 'dm-pill--outline'"
                    >
                      {{ block.secondary_label }}
                    </a>
                  </template>
                </div>
              </div>

              <div v-if="resolveImg(`cb-${idx}`, block.image_url)" class="dm-cblock__media">
                <img
                  :src="resolveImg(`cb-${idx}`, block.image_url)"
                  :alt="block.title || brandName"
                  loading="lazy"
                  @error="onImgError(`cb-${idx}`)"
                />
              </div>
            </article>
          </div>
        </section>
      </template>
    </main>

    <!-- 区块 7：极简分组页脚 + 顶部可选胶囊行动条 -->
    <footer v-if="footerVisible" id="footer" class="dm-footer">
      <div class="dm-wrap">
        <div v-if="homeConfig.footer.show_cta_bar" class="dm-footer-cta">
          <p v-if="homeConfig.footer.cta_heading" class="dm-footer-cta__heading">{{ homeConfig.footer.cta_heading }}</p>
          <p v-if="homeConfig.footer.cta_note" class="dm-footer-cta__note">{{ homeConfig.footer.cta_note }}</p>
          <div class="dm-footer-cta__bar">
            <span class="dm-footer-cta__placeholder">{{ homeConfig.footer.cta_placeholder || '开始一段新的对话' }}</span>
            <template v-if="homeConfig.footer.cta_button_label">
              <button
                v-if="resolveAuthAwareUrl(homeConfig.footer.cta_button_url, '/login').kind === 'hash'"
                type="button"
                class="dm-pill dm-pill--ink dm-pill--sm"
                @click="scrollToHash((resolveAuthAwareUrl(homeConfig.footer.cta_button_url, '/login') as { kind: 'hash'; hashId: string }).hashId)"
              >
                <span>{{ isLoggedIn && (!homeConfig.footer.cta_button_url || homeConfig.footer.cta_button_url.startsWith('/login')) ? '回到聊天' : homeConfig.footer.cta_button_label }}</span>
                <Icon name="chevron" :size="14" class="dm-arrow-shift" />
              </button>
              <RouterLink
                v-else-if="resolveAuthAwareUrl(homeConfig.footer.cta_button_url, '/login').kind === 'route'"
                :to="resolveAuthAwareUrl(homeConfig.footer.cta_button_url, '/login').href"
                class="dm-pill dm-pill--ink dm-pill--sm"
              >
                <span>{{ isLoggedIn && (!homeConfig.footer.cta_button_url || homeConfig.footer.cta_button_url.startsWith('/login')) ? '回到聊天' : homeConfig.footer.cta_button_label }}</span>
                <Icon name="chevron" :size="14" class="dm-arrow-shift" />
              </RouterLink>
              <a
                v-else-if="resolveAuthAwareUrl(homeConfig.footer.cta_button_url, '/login').kind === 'external' || resolveAuthAwareUrl(homeConfig.footer.cta_button_url, '/login').kind === 'native'"
                :href="resolveAuthAwareUrl(homeConfig.footer.cta_button_url, '/login').href"
                target="_blank"
                rel="noopener noreferrer"
                class="dm-pill dm-pill--ink dm-pill--sm"
              >
                <span>{{ homeConfig.footer.cta_button_label }}</span>
                <Icon name="chevron" :size="14" class="dm-arrow-shift" />
              </a>
            </template>
          </div>
        </div>

        <div class="dm-footer__grid">
          <div class="dm-footer__mission">
            <p class="dm-footer__statement">
              <span v-for="(line, i) in missionLines" :key="`ml-${i}`" class="dm-footer__statement-line">{{ line }}</span>
            </p>
          </div>

          <div class="dm-footer__cols">
            <div
              v-for="(col, cidx) in homeConfig.footer.columns"
              :key="`fcol-${cidx}-${col.title}`"
              class="dm-footer__col"
            >
              <p class="dm-footer__heading">{{ col.title }}</p>
              <ul>
                <li v-for="(lnk, lidx) in col.links" :key="`fl-${cidx}-${lidx}`">
                  <button
                    v-if="resolveLink(lnk.url).kind === 'hash'"
                    type="button"
                    class="dm-footer__link"
                    @click="scrollToHash((resolveLink(lnk.url) as { kind: 'hash'; hashId: string }).hashId)"
                  >
                    <span v-if="lnk.icon" class="dm-footer__icon" aria-hidden="true">
                      <Icon :name="safeIconName(lnk.icon)" :size="13" />
                    </span>
                    <span>{{ lnk.label }}</span>
                  </button>
                  <RouterLink
                    v-else-if="resolveLink(lnk.url).kind === 'route'"
                    :to="resolveAuthAwareUrl(lnk.url, '/login').href"
                    class="dm-footer__link"
                  >
                    <span v-if="lnk.icon" class="dm-footer__icon" aria-hidden="true">
                      <Icon :name="safeIconName(lnk.icon)" :size="13" />
                    </span>
                    <span>{{ lnk.label }}</span>
                  </RouterLink>
                  <a
                    v-else-if="resolveLink(lnk.url).kind === 'external' || resolveLink(lnk.url).kind === 'native'"
                    :href="resolveLink(lnk.url).href"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="dm-footer__link"
                  >
                    <span v-if="lnk.icon" class="dm-footer__icon" aria-hidden="true">
                      <Icon :name="safeIconName(lnk.icon)" :size="13" />
                    </span>
                    <span>{{ lnk.label }}</span>
                  </a>
                </li>
              </ul>
            </div>
          </div>
        </div>

        <div class="dm-footer__bottom">
          <div class="dm-footer__bottom-left">
            <RouterLink to="/welcome" class="dm-brand dm-brand--foot">
              <img src="/logo.png" :alt="`${brandName}标志`" width="22" height="22" />
              <strong>{{ brandName }}</strong>
            </RouterLink>
            <p v-if="homeConfig.footer.bottom_note" class="dm-footer__note">{{ homeConfig.footer.bottom_note }}</p>
          </div>

          <nav v-if="homeConfig.footer.bottom_links.length > 0" class="dm-footer__bottom-links" aria-label="底部链接">
            <template v-for="(lnk, idx) in homeConfig.footer.bottom_links" :key="`bl-${idx}-${lnk.label}`">
              <button
                v-if="resolveLink(lnk.url).kind === 'hash'"
                type="button"
                @click="scrollToHash((resolveLink(lnk.url) as { kind: 'hash'; hashId: string }).hashId)"
              >
                {{ lnk.label }}
              </button>
              <RouterLink
                v-else-if="resolveLink(lnk.url).kind === 'route'"
                :to="resolveLink(lnk.url).href"
              >
                {{ lnk.label }}
              </RouterLink>
              <a
                v-else-if="resolveLink(lnk.url).kind === 'external' || resolveLink(lnk.url).kind === 'native'"
                :href="resolveLink(lnk.url).href"
                target="_blank"
                rel="noopener noreferrer"
              >
                {{ lnk.label }}
              </a>
            </template>
          </nav>
        </div>
      </div>
    </footer>
  </div>
</template>

<style scoped>
/* ===== 根容器与主题变量注入 ===== */
.dm-site {
  --dm-canvas: #faf9f5;
  --dm-canvas-rgb: 250, 249, 245;
  --dm-accent: #5b7a4a;
  --dm-accent-rgb: 91, 122, 74;
  --dm-glow-1: #8fa67f;
  --dm-glow-1-rgb: 143, 166, 127;
  --dm-glow-2: #bcd1ca;
  --dm-glow-2-rgb: 188, 209, 202;
  --dm-radius: 24px;
  --dm-heading-font: var(--font-sans);

  height: 100%;
  overflow-y: auto;
  overflow-x: hidden;
  background: var(--dm-canvas);
  color: var(--color-ink);
  font-family: var(--font-sans);
  scroll-behavior: smooth;
}

.dm-site--serif {
  --dm-heading-font: var(--font-display);
}

.dm-site h1,
.dm-site h2,
.dm-site h3,
.dm-site h4 {
  font-family: var(--dm-heading-font);
  word-break: normal;
  overflow-wrap: anywhere;
  text-wrap: wrap;
}

.dm-wrap {
  width: 100%;
  max-width: 1440px;
  margin: 0 auto;
  padding-left: 72px;
  padding-right: 72px;
}

/* ===== 顶栏：5 层渐进渐变模糊（Progressive Gradient Blur），无硬底边线 ===== */
.dm-header {
  position: sticky;
  top: 0;
  z-index: 30;
  height: 64px;
  border-bottom: 0;
}

.dm-header__blur {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 80px;
  z-index: 0;
  pointer-events: none;
}

.dm-header__blur::after {
  content: '';
  position: absolute;
  top: -16px;
  left: 0;
  right: 0;
  bottom: -10px;
  pointer-events: none;
  filter: blur(12px);
  background: linear-gradient(
    to bottom,
    rgba(var(--dm-canvas-rgb), 1) 0%,
    rgba(var(--dm-canvas-rgb), 0.85) 20%,
    rgba(var(--dm-canvas-rgb), 0.55) 45%,
    rgba(var(--dm-canvas-rgb), 0.3) 70%,
    rgba(var(--dm-canvas-rgb), 0) 100%
  );
  transition: opacity 0.22s ease;
}

.dm-header__blur-layer {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: transparent;
  -webkit-backdrop-filter: blur(var(--hb-blur));
  backdrop-filter: blur(var(--hb-blur));
  -webkit-mask-repeat: no-repeat;
  -webkit-mask-size: 100% 100%;
  mask-repeat: no-repeat;
  mask-size: 100% 100%;
  transition: opacity 0.22s ease;
}

.dm-header__blur-layer--1 {
  --hb-blur: 4px;
  -webkit-mask-image: linear-gradient(to bottom, transparent 0%, transparent 40%, #000 60%, #000 80%, transparent 100%);
  mask-image: linear-gradient(to bottom, transparent 0%, transparent 40%, #000 60%, #000 80%, transparent 100%);
}
.dm-header__blur-layer--2 {
  --hb-blur: 6px;
  -webkit-mask-image: linear-gradient(to bottom, transparent 0%, transparent 20%, #000 40%, #000 60%, transparent 80%, transparent 100%);
  mask-image: linear-gradient(to bottom, transparent 0%, transparent 20%, #000 40%, #000 60%, transparent 80%, transparent 100%);
}
.dm-header__blur-layer--3 {
  --hb-blur: 10px;
  -webkit-mask-image: linear-gradient(to bottom, transparent 0%, #000 20%, #000 40%, transparent 60%, transparent 100%);
  mask-image: linear-gradient(to bottom, transparent 0%, #000 20%, #000 40%, transparent 60%, transparent 100%);
}
.dm-header__blur-layer--4 {
  --hb-blur: 18px;
  -webkit-mask-image: linear-gradient(to bottom, #000 0%, #000 20%, transparent 40%, transparent 100%);
  mask-image: linear-gradient(to bottom, #000 0%, #000 20%, transparent 40%, transparent 100%);
}
.dm-header__blur-layer--5 {
  --hb-blur: 34px;
  -webkit-mask-image: linear-gradient(to bottom, #000 0%, transparent 20%, transparent 100%);
  mask-image: linear-gradient(to bottom, #000 0%, transparent 20%, transparent 100%);
}

.dm-header--transparent:not(.is-scrolled):not(.is-open) .dm-header__blur::after,
.dm-header--transparent:not(.is-scrolled):not(.is-open) .dm-header__blur-layer {
  opacity: 0;
}

.dm-header--no-blur.is-scrolled,
.dm-header--no-blur.is-open,
.dm-header--no-blur:not(.dm-header--transparent) {
  background: rgba(var(--dm-canvas-rgb), 0.94);
  border-bottom: 1px solid rgba(20, 20, 19, 0.08);
}

.dm-header__inner {
  position: relative;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  max-width: 1440px;
  height: 64px;
  margin: 0 auto;
  padding: 0 72px;
}

.dm-header__left {
  display: flex;
  align-items: center;
  gap: 36px;
  min-width: 0;
}

.dm-menu-btn {
  display: none;
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 9999px;
  background: rgba(255, 255, 255, 0.42);
  border: 1px solid rgba(20, 20, 19, 0.1);
  -webkit-backdrop-filter: blur(12px);
  backdrop-filter: blur(12px);
  color: var(--color-ink);
  transition: background-color 0.2s ease, border-color 0.2s ease;
}

.dm-menu-btn:hover {
  background: rgba(255, 255, 255, 0.78);
  border-color: rgba(20, 20, 19, 0.2);
}

.dm-brand {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  flex-shrink: 0;
  color: var(--color-ink);
  text-decoration: none;
}

.dm-brand strong {
  font-family: var(--dm-heading-font);
  font-size: 18px;
  font-weight: 500;
  letter-spacing: -0.02em;
}

.dm-brand--foot strong {
  font-size: 16px;
}

.dm-nav {
  display: flex;
  align-items: center;
  gap: 4px;
}

.dm-nav__item {
  display: inline-flex;
  align-items: center;
  height: 34px;
  padding: 0 14px;
  border-radius: 9999px;
  color: var(--color-ink);
  font-size: 14px;
  font-weight: 400;
  text-decoration: none;
  white-space: nowrap;
  transition: background-color 0.18s ease, box-shadow 0.18s ease;
}

.dm-nav__item:hover {
  background: rgba(var(--dm-canvas-rgb), 0.78);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.1);
}

.dm-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

/* 移动端毛玻璃抽屉 */
.dm-drawer {
  position: absolute;
  top: 64px;
  left: 0;
  right: 0;
  z-index: 2;
  padding: 16px 20px 24px;
  background: rgba(var(--dm-canvas-rgb), 0.95);
  -webkit-backdrop-filter: blur(22px);
  backdrop-filter: blur(22px);
  border-bottom: 1px solid rgba(20, 20, 19, 0.08);
  box-shadow: 0 18px 40px rgba(20, 20, 19, 0.08);
}

.dm-drawer__nav {
  display: grid;
  gap: 4px;
  margin-bottom: 16px;
}

.dm-drawer__link {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 12px 14px;
  border-radius: 14px;
  color: var(--color-ink);
  font-size: 15.5px;
  font-weight: 500;
  text-align: left;
  transition: background-color 0.18s ease;
}

.dm-drawer__link:hover {
  background: rgba(20, 20, 19, 0.05);
}

.dm-drawer__actions {
  display: grid;
  gap: 10px;
}

.dm-drawer-enter-active,
.dm-drawer-leave-active {
  transition: opacity 0.22s ease, transform 0.22s cubic-bezier(0.16, 1, 0.3, 1);
}

.dm-drawer-enter-from,
.dm-drawer-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}

/* ===== DeepMind 胶囊按钮系统 ===== */
.dm-pill {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 42px;
  padding: 0 20px;
  border-radius: 9999px;
  font-size: 14px;
  font-weight: 500;
  line-height: 1;
  white-space: nowrap;
  text-decoration: none;
  transition:
    background-color 0.2s cubic-bezier(0.16, 1, 0.3, 1),
    box-shadow 0.2s cubic-bezier(0.16, 1, 0.3, 1),
    color 0.2s ease,
    transform 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}

.dm-pill:active {
  transform: scale(0.982);
}

.dm-pill--sm {
  height: 36px;
  padding: 0 16px;
  font-size: 13.5px;
}

.dm-pill--hero {
  height: 46px;
  padding: 0 24px;
  font-size: 15px;
}

.dm-pill--card {
  height: 36px;
  padding: 0 16px;
  font-size: 13.5px;
}

.dm-pill--ink {
  background: var(--color-ink);
  color: var(--color-on-dark);
  box-shadow: 0 0 0 1px var(--color-ink);
}

.dm-pill--ink:hover {
  background: var(--color-ink-hover);
  box-shadow: 0 0 0 1px var(--color-ink-hover);
  color: #fff;
}

.dm-pill--glass {
  background: rgba(255, 255, 255, 0.42);
  color: var(--color-ink);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.14);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
}

.dm-pill--glass:hover {
  background: rgba(255, 255, 255, 0.82);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.26);
}

.dm-pill--hero-glass {
  background: rgba(255, 255, 255, 0.48);
  color: var(--color-ink);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.14);
  -webkit-backdrop-filter: blur(12px);
  backdrop-filter: blur(12px);
}

.dm-pill--hero-glass:hover {
  background: rgba(255, 255, 255, 0.84);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.26);
}

.dm-pill--outline {
  background: transparent;
  color: var(--color-ink);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.15);
}

.dm-pill--outline:hover {
  background: rgba(20, 20, 19, 0.04);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.28);
}

.dm-pill--light {
  background: #ffffff;
  color: #141413;
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.9);
}

.dm-pill--light:hover {
  background: #f5f4ed;
}

.dm-pill--dark-glass {
  background: rgba(255, 255, 255, 0.08);
  color: #faf9f5;
  box-shadow: 0 0 0 1px rgba(250, 249, 245, 0.24);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
}

.dm-pill--dark-glass:hover {
  background: rgba(255, 255, 255, 0.18);
  box-shadow: 0 0 0 1px rgba(250, 249, 245, 0.42);
}

.dm-arrow-shift {
  transition: transform 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}

.dm-icon-flip {
  transform: rotate(180deg);
}

/* ===== 区块 1：满屏沉浸大气 Hero ===== */
.dm-hero {
  position: relative;
  min-height: 100svh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding-top: 64px;
}

.dm-hero--first {
  margin-top: -64px;
}

.dm-hero--compact {
  min-height: 74vh;
}

.dm-hero__backdrop {
  position: absolute;
  inset: 0;
  pointer-events: none;
  z-index: 0;
  overflow: hidden;
  background:
    radial-gradient(120% 90% at 50% 42%, rgba(var(--dm-canvas-rgb), 0.72) 0%, rgba(var(--dm-canvas-rgb), 0.18) 65%, transparent 100%),
    linear-gradient(
      180deg,
      rgba(var(--dm-glow-2-rgb), 0.48) 0%,
      rgba(var(--dm-glow-1-rgb), 0.26) 46%,
      rgba(var(--dm-glow-2-rgb), 0.34) 76%,
      var(--dm-canvas) 100%
    );
}

.dm-hero__bg-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.dm-hero__bg-scrim {
  position: absolute;
  inset: 0;
  background: #141413;
}

.dm-hero__cloud {
  position: absolute;
  border-radius: 50%;
  filter: blur(88px);
  will-change: transform;
  animation: dm-cloud-drift 18s ease-in-out infinite alternate;
}

.dm-hero__cloud--a {
  top: -12%;
  left: -8%;
  width: 58vw;
  height: 52vh;
  background: radial-gradient(circle, rgba(var(--dm-glow-2-rgb), 0.78) 0%, rgba(var(--dm-glow-2-rgb), 0) 70%);
}

.dm-hero__cloud--b {
  top: -6%;
  right: -10%;
  width: 60vw;
  height: 58vh;
  background: radial-gradient(circle, rgba(var(--dm-glow-1-rgb), 0.56) 0%, rgba(var(--dm-glow-1-rgb), 0) 72%);
  animation-delay: -6s;
}

.dm-hero__cloud--c {
  bottom: 4%;
  left: 14%;
  width: 52vw;
  height: 44vh;
  background: radial-gradient(circle, rgba(227, 218, 204, 0.76) 0%, rgba(227, 218, 204, 0) 72%);
  animation-delay: -11s;
}

.dm-hero__cloud--d {
  top: 26%;
  left: 24%;
  width: 52vw;
  height: 36vh;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.72) 0%, rgba(255, 255, 255, 0) 70%);
  animation-delay: -3s;
}

.dm-hero__stardust {
  position: absolute;
  inset: 0;
  opacity: 0.42;
  background-image:
    radial-gradient(circle, rgba(255, 255, 255, 0.85) 1px, transparent 1.4px),
    radial-gradient(circle, rgba(var(--dm-accent-rgb), 0.24) 1px, transparent 1.4px);
  background-size: 96px 96px, 148px 148px;
  background-position: 18px 24px, 64px 72px;
  animation: dm-dust-float 24s linear infinite;
}

@keyframes dm-cloud-drift {
  0% {
    transform: translate3d(0, 0, 0) scale(1);
  }
  100% {
    transform: translate3d(24px, 16px, 0) scale(1.06);
  }
}

@keyframes dm-dust-float {
  0% {
    transform: translate3d(0, 0, 0);
  }
  100% {
    transform: translate3d(0, -18px, 0);
  }
}

.dm-hero__inner {
  position: relative;
  z-index: 1;
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  max-width: 1120px;
  margin: 0 auto;
  padding: 56px 32px 48px;
  text-align: center;
}

.dm-hero__center {
  margin: auto 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 28px 0;
}

.dm-hero__title {
  margin: 0;
  font-size: clamp(42px, 6.2vw, 84px);
  font-weight: 500;
  line-height: 1.12;
  letter-spacing: -0.032em;
  color: var(--color-ink);
  max-width: 980px;
}

.dm-hero__title-line {
  display: block;
}

.dm-hero__subtitle {
  margin: 22px 0 0;
  max-width: 600px;
  font-size: clamp(15.5px, 1.3vw, 18.5px);
  line-height: 1.6;
  color: var(--color-text-secondary);
}

.dm-hero__bottom {
  padding-top: 24px;
}

.dm-hero__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 12px;
}

.dm-hero--has-image .dm-hero__title {
  color: #faf9f5;
}

.dm-hero--has-image .dm-hero__subtitle {
  color: rgba(250, 249, 245, 0.84);
}

.dm-hero--has-image .dm-pill--hero-glass {
  background: rgba(255, 255, 255, 0.14);
  color: #faf9f5;
  box-shadow: 0 0 0 1px rgba(250, 249, 245, 0.34);
}

.dm-hero--has-image .dm-pill--hero-glass:hover {
  background: rgba(255, 255, 255, 0.24);
}

/* ===== 通用区块节奏与标题（对齐 deepmind.htm 间距与字号阶梯） ===== */
.dm-section {
  padding-top: 72px;
  padding-bottom: 24px;
}

.dm-section--divided {
  margin-top: 84px;
  padding-top: 72px;
  border-top: 1px solid rgba(20, 20, 19, 0.08);
}

.dm-sec-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 56px;
}

.dm-sec-head--compact {
  align-items: center;
  margin-bottom: 44px;
}

.dm-sec-heading-stack {
  min-width: 0;
}

.dm-sec-title {
  margin: 0;
  font-size: clamp(26px, 2.3vw, 34px);
  font-weight: 500;
  line-height: 1.08;
  letter-spacing: -0.02em;
  color: var(--color-ink);
}

.dm-sec-subtitle {
  margin: 6px 0 0;
  font-family: var(--dm-heading-font);
  font-size: clamp(24px, 2.2vw, 32px);
  font-weight: 400;
  line-height: 1.14;
  letter-spacing: -0.018em;
  color: var(--color-text-secondary);
}

/* ===== 区块 2：2:3 竖版焦点展示卡横向轮播（对齐 media--aspect-2-3 + carousel--group-3） ===== */
.dm-showcase {
  padding-top: 56px;
  padding-bottom: 12px;
}

.dm-showcase__viewport {
  width: 100%;
  overflow: hidden;
}

.dm-showcase__track {
  display: flex;
  gap: 24px;
  overflow-x: auto;
  scroll-snap-type: x proximity;
  padding: 4px max(72px, calc((100vw - 1440px) / 2 + 72px)) 4px;
  scroll-padding-left: max(72px, calc((100vw - 1440px) / 2 + 72px));
  scrollbar-width: none;
}

.dm-showcase__track::-webkit-scrollbar {
  display: none;
}

.dm-scard {
  position: relative;
  flex: 0 0 clamp(320px, 28vw, 412px);
  aspect-ratio: 2 / 3;
  max-height: 576px;
  border-radius: var(--dm-radius);
  padding: 24px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  overflow: hidden;
  scroll-snap-align: start;
  color: #faf9f5;
  transition: transform 0.28s cubic-bezier(0.16, 1, 0.3, 1);
}

.dm-scard:hover {
  transform: translateY(-3px);
}

.dm-scard--dark {
  background:
    radial-gradient(88% 68% at 72% 48%, rgba(var(--dm-accent-rgb), 0.28) 0%, transparent 66%),
    radial-gradient(64% 58% at 24% 80%, rgba(var(--dm-glow-2-rgb), 0.16) 0%, transparent 68%),
    #17181b;
}

.dm-scard--olive {
  background:
    radial-gradient(86% 68% at 76% 42%, rgba(var(--dm-glow-1-rgb), 0.36) 0%, transparent 68%),
    radial-gradient(64% 56% at 18% 82%, rgba(var(--dm-accent-rgb), 0.3) 0%, transparent 70%),
    #1b2319;
}

.dm-scard--mist {
  background:
    radial-gradient(92% 72% at 68% 46%, rgba(var(--dm-glow-2-rgb), 0.42) 0%, transparent 68%),
    radial-gradient(68% 58% at 20% 80%, rgba(114, 152, 146, 0.28) 0%, transparent 70%),
    #1c2929;
}

.dm-scard--sand {
  background:
    radial-gradient(88% 68% at 70% 48%, rgba(227, 218, 204, 0.34) 0%, transparent 68%),
    radial-gradient(64% 58% at 20% 80%, rgba(168, 138, 102, 0.28) 0%, transparent 70%),
    #26211c;
}

.dm-scard__bg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  z-index: 0;
}

.dm-scard__scrim {
  position: absolute;
  inset: 0;
  z-index: 1;
  background: linear-gradient(
    to bottom,
    rgba(20, 20, 19, 0.64) 0%,
    rgba(20, 20, 19, 0.14) 44%,
    rgba(20, 20, 19, 0.72) 100%
  );
}

.dm-scard__ambient {
  position: absolute;
  inset: 0;
  z-index: 1;
  pointer-events: none;
  color: #faf9f5;
}

.dm-scard__geo {
  width: 100%;
  height: 100%;
}

.dm-scard__head {
  position: relative;
  z-index: 2;
  max-width: 310px;
}

.dm-scard__head h3 {
  margin: 0;
  font-size: 22px;
  font-weight: 500;
  line-height: 1.26;
  letter-spacing: -0.02em;
  color: #ffffff;
}

.dm-scard__head p {
  margin: 10px 0 0;
  font-size: 14px;
  line-height: 1.55;
  color: rgba(250, 249, 245, 0.76);
}

.dm-scard__actions {
  position: relative;
  z-index: 2;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

/* 轮播双箭头胶囊（1:1 对齐 deepmind.htm .combo-button-group） */
.dm-pager-row {
  margin-top: 32px;
  display: flex;
  justify-content: flex-start;
}

.dm-pager {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px;
  border-radius: 9999px;
  background: var(--color-card);
  -webkit-backdrop-filter: blur(8px);
  backdrop-filter: blur(8px);
}

.dm-pager__btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 42px;
  height: 36px;
  border-radius: 9999px;
  color: var(--color-ink);
  transition: background-color 0.18s ease, opacity 0.18s ease;
}

.dm-pager__btn:hover:not(:disabled) {
  background: rgba(20, 20, 19, 0.08);
}

.dm-pager__btn:disabled {
  opacity: 0.24;
  cursor: not-allowed;
}

/* ===== 区块 3：不对称双栏最新动态（含 DeepMind 标志性 hover 浅底光晕） ===== */
.dm-news-grid {
  display: grid;
  grid-template-columns: 1.05fr 1fr;
  gap: 64px;
  align-items: start;
}

.dm-news-grid--single {
  grid-template-columns: minmax(0, 760px);
}

.dm-news-lead-col {
  position: sticky;
  top: 88px;
}

.dm-news-lead {
  position: relative;
  display: block;
  text-decoration: none;
  color: inherit;
  z-index: 0;
}

.dm-news-lead::before {
  content: '';
  position: absolute;
  inset: -16px;
  border-radius: var(--dm-radius);
  background: transparent;
  transition: background-color 150ms ease;
  z-index: -1;
}

.dm-news-lead:hover::before {
  background: var(--color-card);
}

.dm-news-lead__title {
  margin: 0;
  font-size: clamp(30px, 2.9vw, 42px);
  font-weight: 500;
  line-height: 1.1;
  letter-spacing: -0.023em;
  color: var(--color-ink);
  transition: color 0.18s ease;
}

.dm-news-lead:hover .dm-news-lead__title {
  color: var(--color-ink);
}

.dm-news-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 16px;
  margin-top: 16px;
  font-size: 13.5px;
  color: var(--color-text-secondary);
}

.dm-news-meta__cat {
  color: var(--color-text-secondary);
}

.dm-news-meta__more {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  color: var(--color-ink);
  font-weight: 500;
}

.dm-news-lead:hover .dm-arrow-shift,
.dm-news-row__link:hover .dm-arrow-shift,
.dm-cap-link:hover .dm-arrow-shift,
.dm-pill:hover .dm-arrow-shift {
  transform: translateX(4px);
}

.dm-news-lead__cover {
  margin-top: 28px;
  aspect-ratio: 16 / 10;
  border-radius: min(var(--dm-radius), 24px);
  overflow: hidden;
  background: var(--color-canvas-soft);
}

.dm-news-lead__cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: transform 0.45s cubic-bezier(0.16, 1, 0.3, 1);
}

.dm-news-lead:hover .dm-news-lead__cover img {
  transform: scale(1.025);
}

.dm-cover-art {
  position: relative;
  width: 100%;
  height: 100%;
  display: flex;
  align-items: flex-end;
  padding: 24px;
  color: #faf9f5;
}

.dm-cover-art--obsidian {
  background: radial-gradient(90% 80% at 80% 20%, rgba(var(--dm-accent-rgb), 0.35), transparent 65%), #191a1c;
}

.dm-cover-art--aurora {
  background: radial-gradient(90% 80% at 24% 24%, rgba(var(--dm-glow-2-rgb), 0.52), transparent 65%), #1d2b2a;
}

.dm-cover-art--cobalt {
  background: radial-gradient(90% 80% at 74% 28%, rgba(114, 146, 178, 0.46), transparent 65%), #1c2430;
}

.dm-cover-art--sage {
  background: radial-gradient(90% 80% at 72% 24%, rgba(var(--dm-glow-1-rgb), 0.56), transparent 66%), #243120;
}

.dm-cover-art--sand {
  color: #141413;
  background: radial-gradient(90% 80% at 72% 24%, rgba(227, 218, 204, 0.9), transparent 70%), #ebe5d8;
}

.dm-cover-art__geo {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

.dm-cover-art__label {
  position: relative;
  z-index: 1;
  font-size: 13px;
  font-weight: 500;
  letter-spacing: 0.04em;
  opacity: 0.85;
}

/* 右栏：DeepMind 极简纯排版列表（含 150ms 圆角浅底 hover 反馈） */
.dm-news-list {
  display: flex;
  flex-direction: column;
}

.dm-news-row {
  border-top: 1px solid rgba(20, 20, 19, 0.07);
  padding: 32px 0;
}

.dm-news-row:first-child {
  padding-top: 0;
  border-top: 0;
}

.dm-news-row__link {
  position: relative;
  display: block;
  text-decoration: none;
  color: inherit;
  z-index: 0;
}

.dm-news-row__link::before {
  content: '';
  position: absolute;
  inset: -14px -16px;
  border-radius: 20px;
  background: transparent;
  transition: background-color 150ms ease;
  z-index: -1;
}

.dm-news-row__link:hover::before {
  background: var(--color-card);
}

.dm-news-row__title {
  margin: 0;
  font-size: clamp(22px, 1.95vw, 28px);
  font-weight: 500;
  line-height: 1.16;
  letter-spacing: -0.015em;
  color: var(--color-ink);
  transition: color 0.18s ease;
}

.dm-news-row__link:hover .dm-news-row__title {
  color: var(--color-ink);
}

.dm-news-empty {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 36px;
  border-radius: var(--dm-radius);
  background: rgba(20, 20, 19, 0.035);
}

.dm-news-empty h3 {
  margin: 0 0 8px;
  font-size: 20px;
  font-weight: 500;
}

.dm-news-empty p {
  margin: 0;
  font-size: 14.5px;
  color: var(--color-text-secondary);
}

/* 骨架屏 */
.dm-skel {
  border-radius: 10px;
  background: linear-gradient(90deg, rgba(20, 20, 19, 0.05) 0%, rgba(20, 20, 19, 0.09) 50%, rgba(20, 20, 19, 0.05) 100%);
  background-size: 200% 100%;
  animation: dm-shimmer 1.4s infinite linear;
}

.dm-skel--title {
  height: 44px;
  width: 82%;
}

.dm-skel--row-title {
  height: 28px;
  width: 88%;
}

.dm-skel--meta {
  height: 16px;
  width: 44%;
  margin-top: 14px;
}

.dm-skel--cover {
  margin-top: 28px;
  aspect-ratio: 16 / 10;
  border-radius: min(var(--dm-radius), 24px);
}

@keyframes dm-shimmer {
  from {
    background-position: 100% 0;
  }
  to {
    background-position: -100% 0;
  }
}

/* ===== 区块 4：4 列横向图标能力 / 生态书架（1:1 对齐 card--model） ===== */
.dm-shelf__viewport {
  width: 100%;
  overflow: hidden;
}

.dm-shelf__track {
  display: flex;
  gap: 36px;
  overflow-x: auto;
  scroll-snap-type: x proximity;
  padding: 14px max(72px, calc((100vw - 1440px) / 2 + 72px)) 8px;
  scroll-padding-left: max(72px, calc((100vw - 1440px) / 2 + 72px));
  scrollbar-width: none;
}

.dm-shelf__track::-webkit-scrollbar {
  display: none;
}

.dm-cap-col {
  flex: 0 0 292px;
  display: flex;
  flex-direction: column;
  scroll-snap-align: start;
}

.dm-cap-icon-wrap {
  position: relative;
  width: 76px;
  height: 76px;
  margin-bottom: 28px;
}

.dm-cap-icon {
  width: 76px;
  height: 76px;
  border-radius: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(var(--dm-accent-rgb), 0.06);
  border: 1px solid rgba(var(--dm-accent-rgb), 0.15);
  color: var(--dm-accent);
}

.dm-cap-badge {
  position: absolute;
  top: 0;
  right: 0;
  transform: translate(25%, -50%);
  padding: 4px 8px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--color-card);
  border: 1px solid rgba(20, 20, 19, 0.1);
  color: var(--color-text-secondary);
  font-size: 12px;
  font-weight: 500;
  line-height: 1;
}

.dm-cap-title {
  margin: 0;
  font-size: 20px;
  font-weight: 500;
  line-height: 1.3;
  letter-spacing: -0.02em;
  color: var(--color-ink);
}

.dm-cap-desc {
  margin: 10px 0 0;
  font-size: 14px;
  line-height: 1.6;
  color: var(--color-text-secondary);
}

.dm-cap-links {
  margin-top: 24px;
}

.dm-cap-links__heading {
  margin: 0 0 10px;
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  color: var(--color-muted);
}

.dm-cap-links ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 8px;
}

.dm-cap-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 0;
  font-size: 14px;
  color: var(--color-ink);
  text-decoration: none;
  transition: color 0.18s ease;
}

.dm-cap-link:hover {
  color: var(--dm-accent);
}

.dm-cap-foot {
  margin-top: auto;
  padding-top: 22px;
}

.dm-cap-more {
  padding: 0;
  border: 0;
  background: transparent;
  font-size: 14px;
  font-weight: 500;
  color: var(--color-text-secondary);
  text-decoration: none;
  transition: color 0.18s ease;
}

.dm-cap-more:hover {
  color: var(--color-ink);
}

/* ===== 区块 5：左侧手风琴 + 右侧大圆角交互舞台 ===== */
.dm-spotlight {
  display: grid;
  grid-template-columns: 0.78fr 1.22fr;
  gap: 56px;
  align-items: start;
}

.dm-accordion {
  display: flex;
  flex-direction: column;
}

.dm-acc-item {
  border-top: 1px solid rgba(20, 20, 19, 0.1);
  transition: border-color 0.2s ease;
}

.dm-acc-item:last-child {
  border-bottom: 1px solid rgba(20, 20, 19, 0.1);
}

.dm-acc-item.is-active {
  border-top-color: var(--color-ink);
  border-bottom: 1px solid var(--color-ink);
}

.dm-acc-item.is-active + .dm-acc-item {
  border-top-color: transparent;
}

.dm-acc-btn {
  width: 100%;
  padding: 24px 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  text-align: left;
  color: var(--color-text-secondary);
  transition: color 0.18s ease;
}

.dm-acc-item.is-active .dm-acc-btn,
.dm-acc-btn:hover {
  color: var(--color-ink);
}

.dm-acc-title {
  font-family: var(--dm-heading-font);
  font-size: 20px;
  font-weight: 500;
  line-height: 1.32;
  letter-spacing: -0.02em;
}

.dm-acc-body {
  padding: 0 0 24px;
}

.dm-acc-body p {
  margin: 0;
  font-size: 14.5px;
  line-height: 1.68;
  color: var(--color-text-secondary);
}

.dm-stage {
  min-height: 448px;
  border-radius: var(--dm-radius);
  background: var(--color-card);
  border: 1px solid rgba(20, 20, 19, 0.08);
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

/* 舞台 1：实时对话演示 */
.dm-stage-chat {
  flex: 1;
  padding: 28px 32px;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.dm-stage-chat__bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-bottom: 14px;
  border-bottom: 1px solid rgba(20, 20, 19, 0.08);
}

.dm-stage-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--dm-accent);
  flex-shrink: 0;
}

.dm-stage-chat__label {
  font-size: 14px;
  font-weight: 500;
  color: var(--color-ink);
}

.dm-stage-chat__tag {
  margin-left: auto;
  font-size: 12.5px;
  color: var(--color-muted);
}

.dm-chat-stream {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.dm-bubble {
  margin: 0;
  max-width: 84%;
  padding: 11px 16px;
  border-radius: 18px 18px 18px 6px;
  background: var(--dm-canvas);
  color: var(--color-ink);
  font-size: 14.5px;
  line-height: 1.55;
  align-self: flex-start;
  border: 1px solid rgba(20, 20, 19, 0.06);
}

.dm-bubble--me {
  align-self: flex-end;
  border-radius: 18px 18px 6px 18px;
  background: var(--color-ink);
  color: var(--color-on-dark);
  border-color: transparent;
}

.dm-stage-tool {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  align-self: flex-start;
  padding: 8px 14px;
  border-radius: 9999px;
  background: rgba(var(--dm-accent-rgb), 0.12);
  color: var(--dm-accent);
  font-size: 13px;
  font-weight: 500;
}

.dm-stage-foot {
  margin-top: auto;
  padding-top: 16px;
  border-top: 1px solid rgba(20, 20, 19, 0.08);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
}

.dm-stage-foot p {
  margin: 0;
  flex: 1 1 260px;
  font-size: 13.5px;
  line-height: 1.55;
  color: var(--color-text-secondary);
}

.dm-stage-foot__btns {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

/* 舞台 2：BuugooArena 预览 */
.dm-stage-arena {
  flex: 1;
  padding: 28px 32px;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.dm-stage-arena__top {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.dm-stage-arena__eyebrow {
  display: block;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: var(--dm-accent);
}

.dm-stage-arena__top h3 {
  margin: 4px 0 0;
  font-size: 22px;
  font-weight: 500;
  letter-spacing: -0.02em;
}

.dm-stage-arena__pill {
  height: 28px;
  padding: 0 12px;
  border-radius: 9999px;
  display: inline-flex;
  align-items: center;
  background: rgba(20, 20, 19, 0.06);
  color: var(--color-text-secondary);
  font-size: 12.5px;
  font-weight: 500;
}

.dm-arena-duel {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.dm-arena-card {
  padding: 16px;
  border-radius: 16px;
  background: var(--dm-canvas);
  border: 1px solid rgba(20, 20, 19, 0.08);
}

.dm-arena-card--alt {
  background: rgba(20, 20, 19, 0.03);
}

.dm-arena-card__tag {
  display: inline-block;
  font-size: 12px;
  font-weight: 600;
  color: var(--dm-accent);
  margin-bottom: 8px;
}

.dm-arena-card--alt .dm-arena-card__tag {
  color: var(--color-muted);
}

.dm-arena-card p {
  margin: 0;
  font-size: 13.5px;
  line-height: 1.55;
  color: var(--color-ink);
}

.dm-arena-chart {
  padding: 14px 16px 10px;
  border-radius: 16px;
  background: var(--dm-canvas);
  border: 1px solid rgba(20, 20, 19, 0.07);
}

.dm-arena-chart__head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 8px;
  font-size: 12px;
  color: var(--color-muted);
  margin-bottom: 6px;
}

.dm-arena-svg {
  width: 100%;
  height: 92px;
  display: block;
  color: var(--color-ink);
}

/* 舞台 3：图片展示 */
.dm-stage-image {
  position: relative;
  flex: 1;
  min-height: 448px;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  background: var(--color-canvas-soft);
}

.dm-stage-image__pic {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.dm-stage-image__fallback {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-muted);
}

.dm-stage-image__caption {
  position: relative;
  z-index: 1;
  margin: 20px;
  padding: 18px 22px;
  border-radius: 18px;
  background: rgba(var(--dm-canvas-rgb), 0.9);
  -webkit-backdrop-filter: blur(14px);
  backdrop-filter: blur(14px);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
}

.dm-stage-image__caption h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 500;
}

.dm-stage-image__caption p {
  margin: 4px 0 0;
  font-size: 13.5px;
  color: var(--color-text-secondary);
}

/* 舞台 4：编辑大字卡 */
.dm-stage-editorial {
  position: relative;
  flex: 1;
  min-height: 448px;
  padding: 44px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  overflow: hidden;
}

.dm-stage-editorial__ambient {
  position: absolute;
  right: -80px;
  bottom: -80px;
  width: 340px;
  height: 340px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(var(--dm-glow-1-rgb), 0.32) 0%, rgba(var(--dm-glow-2-rgb), 0.16) 45%, transparent 72%);
  pointer-events: none;
}

.dm-stage-editorial__content {
  position: relative;
  z-index: 1;
  max-width: 520px;
}

.dm-stage-editorial__index {
  display: inline-block;
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.08em;
  color: var(--dm-accent);
  margin-bottom: 16px;
}

.dm-stage-editorial__content h3 {
  margin: 0;
  font-size: clamp(26px, 2.4vw, 34px);
  font-weight: 500;
  line-height: 1.2;
  letter-spacing: -0.025em;
  color: var(--color-ink);
}

.dm-stage-editorial__content p {
  margin: 16px 0 0;
  font-size: 15.5px;
  line-height: 1.7;
  color: var(--color-text-secondary);
}

.dm-stage-editorial__actions {
  margin-top: 28px;
}

/* ===== 区块 6：自定义扩展内容区块 ===== */
.dm-custom-stack {
  display: grid;
  gap: 28px;
}

.dm-cblock {
  border-radius: var(--dm-radius);
  padding: 48px;
  display: grid;
  gap: 36px;
  align-items: center;
}

.dm-cblock--split {
  grid-template-columns: 1.1fr 0.9fr;
}

.dm-cblock--light {
  padding-left: 0;
  padding-right: 0;
  background: transparent;
}

.dm-cblock--card {
  background: var(--color-card);
  border: 1px solid rgba(20, 20, 19, 0.07);
}

.dm-cblock--dark {
  background:
    radial-gradient(85% 75% at 88% 18%, rgba(var(--dm-accent-rgb), 0.26) 0%, transparent 64%),
    #17181b;
  color: #faf9f5;
}

.dm-cblock__eyebrow {
  display: inline-block;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.08em;
  color: var(--dm-accent);
  margin-bottom: 12px;
}

.dm-cblock--dark .dm-cblock__eyebrow {
  color: var(--dm-glow-2);
}

.dm-cblock__title {
  margin: 0;
  font-size: clamp(26px, 2.6vw, 38px);
  font-weight: 500;
  line-height: 1.18;
  letter-spacing: -0.025em;
}

.dm-cblock__subtitle {
  margin: 12px 0 0;
  font-size: 17px;
  line-height: 1.55;
  color: var(--color-text-secondary);
}

.dm-cblock--dark .dm-cblock__subtitle {
  color: rgba(250, 249, 245, 0.76);
}

.dm-cblock__md {
  margin-top: 18px;
  font-size: 15px;
  line-height: 1.72;
  color: var(--color-text-secondary);
}

.dm-cblock--dark .dm-cblock__md {
  color: rgba(250, 249, 245, 0.8);
}

.dm-cblock__md :deep(p) {
  margin: 0 0 12px;
}

.dm-cblock__md :deep(p:last-child) {
  margin-bottom: 0;
}

.dm-cblock__md :deep(a) {
  color: var(--dm-accent);
  text-decoration: underline;
  text-underline-offset: 3px;
}

.dm-cblock--dark .dm-cblock__md :deep(a) {
  color: var(--dm-glow-2);
}

.dm-cblock__md :deep(ul),
.dm-cblock__md :deep(ol) {
  margin: 0 0 12px;
  padding-left: 20px;
}

.dm-cblock__actions {
  margin-top: 26px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.dm-cblock__media {
  border-radius: calc(var(--dm-radius) - 6px);
  overflow: hidden;
  background: var(--color-canvas-soft);
  aspect-ratio: 16 / 10;
}

.dm-cblock__media img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

/* ===== 区块 7：极简分组页脚 ===== */
.dm-footer {
  margin-top: 96px;
  padding-top: 64px;
  padding-bottom: 48px;
  border-top: 1px solid rgba(20, 20, 19, 0.09);
}

.dm-footer-cta {
  padding-bottom: 64px;
  margin-bottom: 56px;
  border-bottom: 1px solid rgba(20, 20, 19, 0.08);
}

.dm-footer-cta__heading {
  margin: 0;
  font-size: 14px;
  font-weight: 500;
  letter-spacing: 0.02em;
  color: var(--color-ink);
}

.dm-footer-cta__note {
  margin: 6px 0 0;
  font-size: 13px;
  color: var(--color-muted);
}

.dm-footer-cta__bar {
  margin-top: 18px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 44px;
  padding: 4px 4px 4px 24px;
  border-radius: 9999px;
  background: var(--color-card);
  transition: background-color 0.2s ease;
}

.dm-footer-cta__bar:hover {
  background: var(--color-card-strong);
}

.dm-footer-cta__placeholder {
  font-size: 14.5px;
  color: var(--color-text-secondary);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dm-footer__grid {
  display: grid;
  grid-template-columns: repeat(12, minmax(0, 1fr));
  gap: 48px;
}

.dm-footer__mission {
  grid-column: span 4;
}

.dm-footer__cols {
  grid-column: span 8;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 32px;
}

.dm-footer__statement {
  margin: 0;
  font-family: var(--dm-heading-font);
  font-size: clamp(22px, 2vw, 28px);
  font-weight: 400;
  line-height: 1.28;
  letter-spacing: -0.022em;
  color: var(--color-ink);
}

.dm-footer__statement-line {
  display: block;
}

.dm-footer__heading {
  margin: 0 0 18px;
  font-size: 15px;
  font-weight: 500;
  color: var(--color-ink);
}

.dm-footer__col ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 12px;
}

.dm-footer__link {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 0;
  border: 0;
  background: transparent;
  font-size: 14px;
  color: var(--color-text-secondary);
  text-align: left;
  text-decoration: none;
  transition: color 0.18s ease;
}

.dm-footer__link:hover {
  color: var(--color-ink);
}

.dm-footer__icon {
  width: 24px;
  height: 24px;
  border-radius: 7px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(20, 20, 19, 0.1);
  background: rgba(20, 20, 19, 0.02);
  color: var(--color-ink);
  flex-shrink: 0;
}

.dm-footer__bottom {
  margin-top: 64px;
  padding-top: 24px;
  border-top: 1px solid rgba(20, 20, 19, 0.08);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
}

.dm-footer__bottom-left {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 18px;
}

.dm-footer__note {
  margin: 0;
  font-size: 13px;
  color: var(--color-muted);
}

.dm-footer__bottom-links {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 20px;
  font-size: 13.5px;
  color: var(--color-text-secondary);
}

.dm-footer__bottom-links a,
.dm-footer__bottom-links button {
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font-size: inherit;
  text-decoration: none;
}

.dm-footer__bottom-links a:hover,
.dm-footer__bottom-links button:hover {
  color: var(--color-ink);
}

/* ===== 滚动渐显 ===== */
.dm-reveal {
  opacity: 0;
  transform: translateY(18px);
  transition: opacity 0.6s cubic-bezier(0.16, 1, 0.3, 1), transform 0.6s cubic-bezier(0.16, 1, 0.3, 1);
}

.dm-reveal.is-in {
  opacity: 1;
  transform: translateY(0);
}

/* ===== 响应式：平板与手机（390px 严格零横向溢出） ===== */
@media (max-width: 1080px) {
  .dm-header__inner {
    padding: 0 40px;
  }
  .dm-wrap {
    padding-left: 40px;
    padding-right: 40px;
  }
  .dm-showcase__track,
  .dm-shelf__track {
    padding-left: 40px;
    padding-right: 40px;
    scroll-padding-left: 40px;
  }
  .dm-news-grid {
    gap: 40px;
  }
  .dm-spotlight {
    grid-template-columns: 1fr;
    gap: 32px;
  }
  .dm-footer__grid {
    grid-template-columns: 1fr;
    gap: 36px;
  }
  .dm-footer__mission,
  .dm-footer__cols {
    grid-column: auto;
  }
  .dm-footer__cols {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 32px;
  }
}

@media (max-width: 900px) {
  .dm-header__inner {
    padding: 0 20px;
  }
  .dm-header__left {
    gap: 10px;
  }
  .dm-menu-btn {
    display: inline-flex;
  }
  .dm-nav {
    display: none;
  }
  .dm-wrap {
    padding-left: 20px;
    padding-right: 20px;
  }
  .dm-showcase__track {
    padding-left: 20px;
    padding-right: 20px;
    scroll-padding-left: 20px;
  }
  .dm-shelf__track {
    padding-left: 20px;
    padding-right: 20px;
    scroll-padding-left: 20px;
  }
  .dm-news-grid {
    grid-template-columns: 1fr;
    gap: 36px;
  }
  .dm-news-lead-col {
    position: static;
  }
  .dm-cblock--split {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .dm-actions .dm-pill--glass {
    display: none;
  }
  .dm-hero__inner {
    padding: 40px 20px 36px;
  }
  .dm-hero__title {
    font-size: clamp(34px, 9.6vw, 48px);
  }
  .dm-hero__actions {
    width: 100%;
    flex-direction: column;
  }
  .dm-hero__actions .dm-pill {
    width: 100%;
    max-width: 300px;
  }
  .dm-scard {
    flex: 0 0 82vw;
    aspect-ratio: 2 / 3;
    max-height: 468px;
    padding: 22px;
  }
  .dm-cap-col {
    flex: 0 0 78vw;
  }
  .dm-sec-head {
    flex-direction: column;
    align-items: flex-start;
    margin-bottom: 28px;
  }
  .dm-section {
    padding-top: 64px;
  }
  .dm-section--divided {
    margin-top: 56px;
    padding-top: 48px;
  }
  .dm-stage,
  .dm-stage-image,
  .dm-stage-editorial {
    min-height: 360px;
  }
  .dm-stage-chat,
  .dm-stage-arena {
    padding: 20px;
  }
  .dm-stage-editorial {
    padding: 28px 22px;
  }
  .dm-arena-duel {
    grid-template-columns: 1fr;
  }
  .dm-cblock {
    padding: 28px 20px;
  }
  .dm-cblock--light {
    padding-left: 0;
    padding-right: 0;
  }
  .dm-footer {
    margin-top: 64px;
    padding-top: 48px;
  }
  .dm-footer-cta__bar {
    flex-direction: column;
    align-items: stretch;
    border-radius: 22px;
    padding: 14px;
  }
  .dm-footer-cta__placeholder {
    white-space: normal;
  }
  .dm-footer__cols {
    grid-template-columns: 1fr;
    gap: 32px;
  }
}

/* ===== 尊重减少动态偏好 ===== */
@media (prefers-reduced-motion: reduce) {
  .dm-site {
    scroll-behavior: auto;
  }
  .dm-hero__cloud,
  .dm-hero__stardust,
  .dm-skel {
    animation: none;
  }
  .dm-reveal {
    opacity: 1;
    transform: none;
    transition: none;
  }
  .dm-scard,
  .dm-news-lead__cover img,
  .dm-pill,
  .dm-arrow-shift {
    transition: none;
  }
}
</style>
