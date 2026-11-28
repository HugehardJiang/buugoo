<script setup lang="ts">
// 多用户版介绍主页（网站首页）：采用 DeepMind 风格的渐进模糊顶栏、编辑式排版与圆角视觉卡片。
// 保留动态标题/标语、注册策略与旗下产品 BuugooArena 外链，并接入公开 CMS 精选文章。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Illo from '../components/Illo.vue'
import Icon from '../Icon.vue'
import { api, type CmsPostSummary } from '../api'
import { setPageSeo, toAbsoluteUrl } from '../seo'
import { needsLogin, session } from '../session'

const name = computed(() => session.meta?.name ?? '布谷')
const title = computed(() => session.meta?.home?.title?.trim() || `${name.value}，一个真的记得你的 AI 朋友`)
const tagline = computed(
  () =>
    session.meta?.home?.tagline?.trim() ||
    '像微信上的老朋友一样聊天：记得你说过的事，有自己的脾气和心情，到了时间会主动来找你。',
)
const registration = computed(() => session.meta?.registration ?? 'closed')
const canRegister = computed(() => registration.value !== 'closed')
const isLoggedIn = computed(() => !needsLogin())
const startTo = computed(() =>
  isLoggedIn.value ? { path: '/' } : canRegister.value ? { path: '/login', query: { tab: 'register' } } : { path: '/login' },
)
const startLabel = computed(() => (isLoggedIn.value ? '进入工作台' : canRegister.value ? `开始认识${name.value}` : '登录'))

// 核心能力卡片（保留全部五大能力并按 DeepMind 风格深/浅圆角卡编排）
const capabilities = computed(() => [
  {
    id: 'memory',
    illo: 'memory' as const,
    tag: '长期记忆',
    title: '你说过的，它都记得',
    text: `你提过的人、事、喜好，${name.value}会悄悄记下来，下次聊到时自然地想起，不用你一遍遍解释。记得太多时它会自己整理，把相近的合成一条。`,
    points: ['所有对话共用同一份记忆', '想看的时候可以翻开查看、删除'],
    tone: 'dark' as const,
    wide: true,
  },
  {
    id: 'emotion',
    illo: 'emotion' as const,
    tag: '情绪与性格',
    title: '有脾气，也有温度',
    text: `${name.value}会开心、担心、想念，被冒犯了也会生气，慢慢又会消气。你还可以给它选一种性格：温柔治愈、毒舌损友、理性军师，或者有主见会犟嘴的「人工智鸟」。`,
    points: ['心情会随着你们的相处变化', '换了性格，记得的事都还在'],
    tone: 'light' as const,
    wide: false,
  },
  {
    id: 'remind',
    illo: 'remind' as const,
    tag: '提醒与定时任务',
    title: '到了时间，它会来找你',
    text: '「半小时后叫我起来活动」是提醒；「每天早上八点搜一下科技新闻发我」是定时任务，到点了它真的会去搜、去算，再把结果发给你。',
    points: ['一次、每天、每周、每月都行', '很久没聊时，也会主动问问你'],
    tone: 'light' as const,
    wide: false,
  },
  {
    id: 'memo',
    illo: 'memo' as const,
    tag: '备忘录',
    title: '一句话，帮你记进本子里',
    text: '「把牛奶和鸡蛋记到购物清单里」——它会挑一个合适的页面写进去，没有就新建一页。清单、笔记、资料长期保存，删之前一定先问你。',
    points: ['可以有很多页，网页上也能直接编辑', '平时不会占用聊天的额度'],
    tone: 'light' as const,
    wide: false,
  },
  {
    id: 'channels',
    illo: 'channels' as const,
    tag: '多端随处可聊',
    title: '网页、微信、QQ，都是同一个它',
    text: '扫码连上自己的微信，或者绑定 QQ、Telegram，在哪里聊都共用同一份记忆和心情，提醒也会发到那里。它还能联网搜索、运行代码、用管理员装好的技能和插件。',
    points: ['收发图片和文件都可以', '聊天记录在网页上也能看到'],
    tone: 'dark' as const,
    wide: true,
  },
])

const steps = computed(() => [
  {
    n: '01',
    title: registration.value === 'invite' ? '用邀请码注册' : '注册一个账号',
    text: registration.value === 'invite' ? '向管理员要一个邀请码，填好就能注册。' : '起个名字、设个密码就好。',
  },
  {
    n: '02',
    title: '选一种相处方式',
    text: '在设置里挑一个你喜欢的性格，随时可以换。',
  },
  {
    n: '03',
    title: '开始聊天',
    text: '随便说点什么。想在微信里聊，就在「聊天软件」里扫码连上。',
  },
])

const arenaColumns = [
  {
    num: '01',
    title: '网感与接梗',
    desc: '用真实中文梗与群聊语境，区分自然接茬和「客服老干部」。',
    href: 'https://arena.buugoo.com/',
  },
  {
    num: '02',
    title: '说人话',
    desc: '检验 AI 能不能把复杂问题讲直白通透，告别套话和官腔。',
    href: 'https://arena.buugoo.com/?col=plain',
  },
]

const arenaPoints = [
  '零系统提示词干扰，直接看模型原生表达',
  '真人匿名双盲投票，独立 K=32 Elo 积分与帕累托前沿排榜',
  '不用注册就能围观实时榜单、参与投票或投稿出题',
]

// 公开 CMS 文章数据
const posts = ref<CmsPostSummary[]>([])
const postsLoading = ref(true)
const failedCovers = ref<Record<string, boolean>>({})

const leadPost = computed(() => posts.value[0] ?? null)
const sidePosts = computed(() => posts.value.slice(1, 6))

function safeCoverUrl(raw?: string): string {
  const u = (raw ?? '').trim()
  if (!u) return ''
  if (u.startsWith('/') && !u.startsWith('//')) return u
  if (/^https?:\/\//i.test(u)) return u
  return ''
}

function resolveCoverSrc(post: CmsPostSummary): string {
  const url = safeCoverUrl(post.cover_url)
  if (!url || failedCovers.value[post.slug]) return ''
  return url
}

function onCoverError(slug: string) {
  failedCovers.value = { ...failedCovers.value, [slug]: true }
}

const VALID_THEMES = ['obsidian', 'aurora', 'cobalt', 'sage', 'sand'] as const
function coverThemeClass(theme?: string, index = 0): string {
  const t = (theme ?? '').trim().toLowerCase()
  if ((VALID_THEMES as readonly string[]).includes(t)) return `cover-art--${t}`
  return `cover-art--${VALID_THEMES[index % VALID_THEMES.length]}`
}

function formatDate(iso?: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getFullYear()} 年 ${d.getMonth() + 1} 月 ${d.getDate()} 日`
}

// 顶栏滚动态与移动端抽屉菜单（监听 .site 滚动容器）
const root = ref<HTMLElement | null>(null)
const isScrolled = ref(false)
const mobileMenuOpen = ref(false)
let observer: IntersectionObserver | undefined

function onSiteScroll() {
  const top = root.value?.scrollTop ?? 0
  isScrolled.value = top > 12
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && mobileMenuOpen.value) mobileMenuOpen.value = false
}

function scrollToSection(id: string) {
  mobileMenuOpen.value = false
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

async function loadPosts() {
  postsLoading.value = true
  try {
    const res = await api.cmsPosts({ limit: 6 })
    posts.value = Array.isArray(res.posts) ? res.posts : []
  } catch {
    posts.value = []
  } finally {
    postsLoading.value = false
  }
}

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  loadPosts()
  await nextTick()
  onSiteScroll()
  const els = root.value?.querySelectorAll('.reveal') ?? []
  if (!('IntersectionObserver' in window)) {
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
    { threshold: 0.12 },
  )
  els.forEach((el) => observer!.observe(el))
})

watch(
  [name, title, tagline],
  () => {
    const homeUrl = toAbsoluteUrl('/welcome')
    const logoUrl = toAbsoluteUrl('/icon-512.png')
    setPageSeo({
      title: title.value,
      description: tagline.value,
      robots: 'index, follow',
      canonicalPath: '/welcome',
      ogType: 'website',
      siteName: name.value,
      image: '/icon-512.png',
      jsonLd: [
        {
          '@context': 'https://schema.org',
          '@type': 'WebSite',
          name: name.value,
          url: homeUrl,
          description: tagline.value,
          inLanguage: 'zh-CN',
        },
        {
          '@context': 'https://schema.org',
          '@type': 'SoftwareApplication',
          name: name.value,
          applicationCategory: 'CommunicationApplication',
          operatingSystem: 'Web, WeChat, QQ, Telegram',
          description: tagline.value,
          url: homeUrl,
        },
        {
          '@context': 'https://schema.org',
          '@type': 'Organization',
          name: name.value,
          url: homeUrl,
          logo: logoUrl,
        },
      ],
    })
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  observer?.disconnect()
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div ref="root" class="site" @scroll.passive="onSiteScroll">
    <!-- 顶栏：5 层渐进渐变模糊（Progressive Gradient Blur），无硬底边线 -->
    <header class="dm-header dm-header--transparent" :class="{ 'is-scrolled': isScrolled, 'is-open': mobileMenuOpen }">
      <div class="dm-header__blur" aria-hidden="true">
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
            <img src="/logo.png" :alt="`${name}标志`" width="28" height="28" />
            <strong>{{ name }}</strong>
          </RouterLink>
        </div>

        <nav class="dm-nav" aria-label="主导航">
          <RouterLink to="/posts" class="dm-nav__item">文章</RouterLink>
          <button type="button" class="dm-nav__item" @click="scrollToSection('features')">能做什么</button>
          <button type="button" class="dm-nav__item" @click="scrollToSection('start')">怎么开始</button>
          <button type="button" class="dm-nav__item" @click="scrollToSection('ecosystem')">旗下产品</button>
        </nav>

        <div class="dm-actions">
          <template v-if="isLoggedIn">
            <RouterLink to="/" class="pill-btn pill-btn--ink pill-btn--sm">进入工作台</RouterLink>
          </template>
          <template v-else>
            <RouterLink to="/login" class="pill-btn pill-btn--glass pill-btn--sm">登录</RouterLink>
            <RouterLink v-if="canRegister" :to="startTo" class="pill-btn pill-btn--ink pill-btn--sm">注册</RouterLink>
          </template>
        </div>
      </div>

      <!-- 移动端毛玻璃折叠抽屉 -->
      <Transition name="dm-drawer">
        <div v-if="mobileMenuOpen" class="dm-drawer" role="dialog" aria-label="移动端导航">
          <nav class="dm-drawer__nav">
            <RouterLink to="/posts" class="dm-drawer__link" @click="mobileMenuOpen = false">
              <span>文章与动态</span>
              <Icon name="chevron" :size="16" />
            </RouterLink>
            <button type="button" class="dm-drawer__link" @click="scrollToSection('features')">
              <span>能做什么</span>
              <Icon name="chevron" :size="16" />
            </button>
            <button type="button" class="dm-drawer__link" @click="scrollToSection('start')">
              <span>怎么开始</span>
              <Icon name="chevron" :size="16" />
            </button>
            <button type="button" class="dm-drawer__link" @click="scrollToSection('ecosystem')">
              <span>旗下产品 · BuugooArena</span>
              <Icon name="chevron" :size="16" />
            </button>
          </nav>
          <div class="dm-drawer__actions">
            <RouterLink :to="startTo" class="pill-btn pill-btn--ink" @click="mobileMenuOpen = false">{{ startLabel }}</RouterLink>
            <RouterLink v-if="!isLoggedIn && canRegister" to="/login" class="pill-btn pill-btn--glass" @click="mobileMenuOpen = false">
              已有账号，去登录
            </RouterLink>
          </div>
        </div>
      </Transition>
    </header>

    <main>
      <!-- Hero：顶栏下方无缝铺开柔和径向光晕与大留白编辑排版 -->
      <section class="hero-stage">
        <div class="hero-ambient" aria-hidden="true">
          <span class="hero-ambient__orb hero-ambient__orb--a" />
          <span class="hero-ambient__orb hero-ambient__orb--b" />
          <span class="hero-ambient__orb hero-ambient__orb--c" />
        </div>

        <div class="wrap hero-inner">
          <div class="hero-copy">
            <span class="meta-pill">你的 AI 朋友</span>
            <h1>{{ title }}</h1>
            <p class="hero-lede">{{ tagline }}</p>
            <div class="hero-cta">
              <RouterLink :to="startTo" class="pill-btn pill-btn--ink pill-btn--lg">
                <span>{{ startLabel }}</span>
                <Icon name="chevron" :size="16" class="btn-arrow" />
              </RouterLink>
              <RouterLink v-if="!isLoggedIn && canRegister" to="/login" class="pill-btn pill-btn--glass pill-btn--lg">
                我已经有账号
              </RouterLink>
              <RouterLink v-else to="/posts" class="pill-btn pill-btn--glass pill-btn--lg">
                阅读最新文章
              </RouterLink>
            </div>
            <p v-if="!isLoggedIn && registration === 'invite'" class="hero-note">
              现在需要邀请码才能注册，可以向管理员要一个。
            </p>
            <p v-else-if="!isLoggedIn && registration === 'closed'" class="hero-note">
              暂时不开放注册，已经有账号的可以直接登录。
            </p>
          </div>

          <!-- DeepMind 风格大圆角视觉媒体舞台：实时对话演示 + 原创插画 -->
          <div class="hero-showcase">
            <article class="stage-card stage-card--dialogue">
              <div class="stage-card__top">
                <span class="stage-dot" aria-hidden="true" />
                <span class="stage-label">真实相处片段</span>
                <span class="stage-meta">记忆 · 情绪 · 提醒联动</span>
              </div>
              <div class="stage-bubbles">
                <p class="chat-bubble chat-bubble--me">今天面试好紧张</p>
                <p class="chat-bubble">诶，是上周说的那家做游戏的公司吗？</p>
                <p class="chat-bubble">你准备了那么久，作品集我看过的，稳的</p>
                <p class="chat-bubble chat-bubble--me">嗯嗯，下午三点</p>
                <p class="chat-bubble">那两点半我喊你一声，面完回来跟我讲讲</p>
              </div>
              <div class="stage-tool">
                <Icon name="clock" :size="14" />
                <span>已自动设好提醒 · 今天 14:30</span>
              </div>
            </article>

            <article class="stage-card stage-card--visual">
              <div class="stage-visual__art">
                <Illo name="hero" />
              </div>
              <div class="stage-visual__caption">
                <p class="stage-visual__quote">
                  不是一个有问必答的工具，而是一个<em>记得你</em>、<em>在乎你</em>、偶尔<em>会主动找你</em>的朋友。
                </p>
              </div>
            </article>
          </div>
        </div>
      </section>

      <!-- 最新文章与动态：DeepMind 标志性不对称双栏编辑式网格 -->
      <section id="articles" class="section-block wrap reveal">
        <div class="sec-head">
          <div>
            <h2>最新文章与动态</h2>
            <p class="sec-sub">记录{{ name }}的设计想法、版本更新与使用指南。</p>
          </div>
          <RouterLink to="/posts" class="pill-btn pill-btn--outline pill-btn--sm">
            <span>查看全部文章</span>
            <Icon name="chevron" :size="14" class="btn-arrow" />
          </RouterLink>
        </div>

        <!-- 加载骨架 -->
        <div v-if="postsLoading" class="editorial-grid editorial-grid--loading" aria-busy="true">
          <div class="editorial-lead">
            <div class="skel skel--title" />
            <div class="skel skel--meta" />
            <div class="skel skel--cover" />
          </div>
          <div class="editorial-list">
            <div v-for="n in 3" :key="n" class="editorial-item editorial-item--skel">
              <div class="skel skel--line" />
              <div class="skel skel--short" />
            </div>
          </div>
        </div>

        <!-- 有文章时的 DeepMind 不对称双栏（左 Sticky 主推 + 右精简列表） -->
        <div v-else-if="leadPost" class="editorial-grid" :class="{ 'editorial-grid--single': sidePosts.length === 0 }">
          <article class="editorial-lead-wrap">
            <RouterLink :to="`/posts/${leadPost.slug}`" class="editorial-lead">
              <div class="editorial-lead__head">
                <div class="post-meta">
                  <span v-if="leadPost.category" class="post-cat">{{ leadPost.category }}</span>
                  <span v-if="leadPost.pinned" class="post-flag">置顶</span>
                  <time v-if="leadPost.published_at || leadPost.created_at" :datetime="leadPost.published_at || leadPost.created_at">
                    {{ formatDate(leadPost.published_at || leadPost.created_at) }}
                  </time>
                </div>
                <h3>{{ leadPost.title }}</h3>
                <p v-if="leadPost.summary" class="editorial-lead__summary">{{ leadPost.summary }}</p>
                <span class="read-more">
                  <span>阅读全文</span>
                  <Icon name="chevron" :size="15" class="read-more__arrow" />
                </span>
              </div>

              <div class="post-cover post-cover--lead">
                <img
                  v-if="resolveCoverSrc(leadPost)"
                  :src="resolveCoverSrc(leadPost)"
                  :alt="leadPost.title"
                  loading="lazy"
                  @error="onCoverError(leadPost.slug)"
                />
                <div v-else class="cover-art" :class="coverThemeClass(leadPost.cover_theme, 0)" aria-hidden="true">
                  <svg class="cover-art__geo" viewBox="0 0 640 400" fill="none">
                    <circle cx="510" cy="96" r="148" stroke="currentColor" stroke-opacity="0.22" stroke-width="1.2" />
                    <circle cx="510" cy="96" r="88" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.2" />
                    <path d="M40 330 C 180 230, 320 290, 600 130" stroke="currentColor" stroke-opacity="0.26" stroke-width="1.4" />
                    <circle cx="176" cy="272" r="5" fill="currentColor" fill-opacity="0.55" />
                  </svg>
                  <span class="cover-art__label">{{ leadPost.category || name }}</span>
                </div>
              </div>
            </RouterLink>
          </article>

          <div v-if="sidePosts.length > 0" class="editorial-list">
            <article v-for="(post, idx) in sidePosts" :key="post.id" class="editorial-item-wrap">
              <RouterLink :to="`/posts/${post.slug}`" class="editorial-item">
                <div class="editorial-item__body">
                  <div class="post-meta">
                    <span v-if="post.category" class="post-cat">{{ post.category }}</span>
                    <span v-if="post.featured" class="post-flag">精选</span>
                    <time v-if="post.published_at || post.created_at" :datetime="post.published_at || post.created_at">
                      {{ formatDate(post.published_at || post.created_at) }}
                    </time>
                  </div>
                  <h3>{{ post.title }}</h3>
                  <p v-if="post.summary" class="editorial-item__summary">{{ post.summary }}</p>
                  <span class="read-more">
                    <span>阅读全文</span>
                    <Icon name="chevron" :size="14" class="read-more__arrow" />
                  </span>
                </div>
                <div class="editorial-item__thumb">
                  <img
                    v-if="resolveCoverSrc(post)"
                    :src="resolveCoverSrc(post)"
                    :alt="post.title"
                    loading="lazy"
                    @error="onCoverError(post.slug)"
                  />
                  <div v-else class="cover-art cover-art--compact" :class="coverThemeClass(post.cover_theme, idx + 1)" aria-hidden="true">
                    <svg class="cover-art__geo" viewBox="0 0 240 160" fill="none">
                      <circle cx="180" cy="44" r="56" stroke="currentColor" stroke-opacity="0.26" stroke-width="1.2" />
                      <path d="M16 132 C 80 86, 134 114, 224 44" stroke="currentColor" stroke-opacity="0.3" stroke-width="1.2" />
                    </svg>
                  </div>
                </div>
              </RouterLink>
            </article>
          </div>
        </div>

        <!-- 空数据时的克制引导卡 -->
        <div v-else class="editorial-empty">
          <div class="editorial-empty__body">
            <span class="meta-pill">内容专栏</span>
            <h3>文章与产品动态即将上线</h3>
            <p>管理员发布第一篇文章后，这里会自动展示最新精选内容、使用技巧与更新记录。</p>
          </div>
          <RouterLink to="/posts" class="pill-btn pill-btn--outline">
            <span>前往文章页</span>
            <Icon name="chevron" :size="15" class="btn-arrow" />
          </RouterLink>
        </div>
      </section>

      <!-- 核心能力展示区：DeepMind 深/浅混排 Bento 圆角卡 -->
      <section id="features" class="section-block wrap reveal">
        <div class="sec-head">
          <div>
            <h2>核心能力</h2>
            <p class="sec-sub">围绕长期陪伴打磨的五件事，每一件都在悄悄记住你的习惯。</p>
          </div>
        </div>

        <div class="bento-grid">
          <article
            v-for="cap in capabilities"
            :key="cap.id"
            class="bento-card"
            :class="[`bento-card--${cap.tone}`, { 'bento-card--wide': cap.wide }]"
          >
            <div class="bento-card__content">
              <span class="bento-tag" :class="{ 'bento-tag--dark': cap.tone === 'dark' }">{{ cap.tag }}</span>
              <h3>{{ cap.title }}</h3>
              <p class="bento-card__desc">{{ cap.text }}</p>
              <ul class="bento-points">
                <li v-for="pt in cap.points" :key="pt">
                  <Icon name="check" :size="15" />
                  <span>{{ pt }}</span>
                </li>
              </ul>
            </div>
            <div class="bento-card__art">
              <Illo :name="cap.illo" />
            </div>
          </article>
        </div>
      </section>

      <!-- 三步上手 -->
      <section id="start" class="section-block wrap reveal">
        <div class="sec-head">
          <div>
            <h2>三步，认识一个新朋友</h2>
            <p class="sec-sub">不需要复杂配置，打开浏览器或连上常用聊天软件就能开始。</p>
          </div>
        </div>

        <ol class="steps-grid">
          <li v-for="s in steps" :key="s.n" class="step-card">
            <span class="step-card__num">{{ s.n }}</span>
            <h3>{{ s.title }}</h3>
            <p>{{ s.text }}</p>
          </li>
        </ol>
      </section>

      <!-- 旗下产品 BuugooArena -->
      <section id="ecosystem" class="section-block wrap reveal">
        <div class="sec-head">
          <div>
            <h2>旗下产品</h2>
            <p class="sec-sub">
              打磨{{ name }}的对话手感时，我们发现常见跑分测不出谁接得住梗、谁能好好说话，于是把这套真人双盲评测开放了出来。
            </p>
          </div>
          <a
            href="https://arena.buugoo.com"
            target="_blank"
            rel="noopener noreferrer"
            class="pill-btn pill-btn--outline pill-btn--sm"
          >
            <span>访问 BuugooArena</span>
            <Icon name="chevron" :size="14" class="btn-arrow" />
          </a>
        </div>

        <article class="arena-panel">
          <div class="arena-panel__art">
            <Illo name="arena" />
          </div>
          <div class="arena-panel__body">
            <div class="arena-panel__meta">
              <span class="arena-panel__name">BuugooArena</span>
              <span class="arena-panel__domain">arena.buugoo.com</span>
              <span class="meta-pill">免登录 · 免费开放</span>
            </div>
            <h3>布谷大模型盲测竞技场</h3>
            <p class="arena-panel__desc">
              不加系统提示词包装，把同一个真实中文问题直接抛给两个匿名模型，由真人投票判断谁更像活人、谁在说人话，再用独立 K=32 Elo 积分与帕累托前沿曲线实时排榜。
            </p>

            <div class="arena-cols">
              <a
                v-for="col in arenaColumns"
                :key="col.href"
                :href="col.href"
                target="_blank"
                rel="noopener noreferrer"
                class="arena-col"
              >
                <div class="arena-col__top">
                  <span class="arena-col__num">{{ col.num }}</span>
                  <strong>{{ col.title }}</strong>
                  <Icon name="chevron" :size="15" class="arena-col__arrow" />
                </div>
                <p>{{ col.desc }}</p>
              </a>
            </div>

            <ul class="arena-points">
              <li v-for="point in arenaPoints" :key="point">
                <Icon name="check" :size="15" />
                <span>{{ point }}</span>
              </li>
            </ul>

            <div class="arena-actions">
              <a
                href="https://arena.buugoo.com"
                target="_blank"
                rel="noopener noreferrer"
                class="pill-btn pill-btn--ink pill-btn--lg"
              >
                <span>看看实时总榜</span>
                <Icon name="chevron" :size="15" class="btn-arrow" />
              </a>
              <a
                href="https://arena.buugoo.com/arena"
                target="_blank"
                rel="noopener noreferrer"
                class="pill-btn pill-btn--outline pill-btn--lg"
              >
                <span>去盲测一局</span>
              </a>
            </div>
          </div>
        </article>
      </section>

      <!-- 收尾高对比深色行动卡 -->
      <section class="final-block wrap reveal">
        <div class="final-card">
          <div class="final-card__glow" aria-hidden="true" />
          <div class="final-card__text">
            <h2>{{ name }}在等你开口</h2>
            <p>说一句「你好」就行。剩下的，它会慢慢记住。</p>
            <div class="final-card__actions">
              <RouterLink :to="startTo" class="pill-btn pill-btn--light pill-btn--lg">
                <span>{{ startLabel }}</span>
                <Icon name="chevron" :size="16" class="btn-arrow" />
              </RouterLink>
              <RouterLink to="/posts" class="pill-btn pill-btn--dark-glass pill-btn--lg">
                <span>浏览全部文章</span>
              </RouterLink>
            </div>
          </div>
          <div class="final-card__art">
            <Illo name="cta" />
          </div>
        </div>
      </section>
    </main>

    <!-- 极简分组页脚 -->
    <footer class="dm-footer">
      <div class="wrap">
        <div class="dm-footer__grid">
          <div class="dm-footer__mission">
            <RouterLink to="/welcome" class="dm-brand dm-brand--foot">
              <img src="/logo.png" :alt="`${name}标志`" width="24" height="24" />
              <strong>{{ name }}</strong>
            </RouterLink>
            <p class="dm-footer__statement">一个记得你说过的事、有自己心情、会按时来找你的 AI 朋友。</p>
          </div>

          <div class="dm-footer__col">
            <p class="dm-footer__heading">产品能力</p>
            <ul>
              <li><button type="button" @click="scrollToSection('features')">记忆与情绪</button></li>
              <li><button type="button" @click="scrollToSection('start')">三步上手</button></li>
              <li><RouterLink :to="startTo">{{ startLabel }}</RouterLink></li>
            </ul>
          </div>

          <div class="dm-footer__col">
            <p class="dm-footer__heading">文章与动态</p>
            <ul>
              <li><RouterLink to="/posts">全部文章</RouterLink></li>
              <li><button type="button" @click="scrollToSection('articles')">最新精选</button></li>
              <li><a href="/rss.xml" target="_blank" rel="noopener noreferrer">RSS 订阅</a></li>
            </ul>
          </div>

          <div class="dm-footer__col">
            <p class="dm-footer__heading">旗下生态</p>
            <ul>
              <li>
                <a href="https://arena.buugoo.com" target="_blank" rel="noopener noreferrer">BuugooArena 实时总榜</a>
              </li>
              <li>
                <a href="https://arena.buugoo.com/" target="_blank" rel="noopener noreferrer">网感与接梗榜单</a>
              </li>
              <li>
                <a href="https://arena.buugoo.com/?col=plain" target="_blank" rel="noopener noreferrer">说人话榜单</a>
              </li>
              <li>
                <a href="https://arena.buugoo.com/arena" target="_blank" rel="noopener noreferrer">参与匿名盲测</a>
              </li>
            </ul>
          </div>
        </div>

        <div class="dm-footer__bottom">
          <p>和{{ name }}聊的内容只属于你自己：记忆和心情可以在设置里翻开查看，也可以随时删除。</p>
        </div>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.site {
  height: 100%;
  overflow-y: auto;
  overflow-x: hidden;
  background: var(--color-canvas);
  color: var(--color-ink);
  scroll-behavior: smooth;
}

.wrap {
  width: 100%;
  max-width: 1320px;
  margin: 0 auto;
  padding-left: 48px;
  padding-right: 48px;
}

/* ===== 顶栏：DeepMind 5 层渐进渐变模糊（Progressive Gradient Blur） ===== */
.dm-header {
  position: sticky;
  top: 0;
  z-index: 30;
  height: 64px;
  isolation: isolate;
  border-bottom: 0;
}

.dm-header__blur {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 80px;
  z-index: -1;
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
    rgba(250, 249, 245, 1) 0%,
    rgba(250, 249, 245, 0.85) 20%,
    rgba(250, 249, 245, 0.55) 45%,
    rgba(250, 249, 245, 0.3) 70%,
    rgba(250, 249, 245, 0) 100%
  );
  transition: opacity 0.22s ease;
}

.dm-header__blur-layer {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: transparent;
  backdrop-filter: blur(var(--hb-blur));
  -webkit-backdrop-filter: blur(var(--hb-blur));
  mask-repeat: no-repeat;
  mask-size: 100% 100%;
  -webkit-mask-repeat: no-repeat;
  -webkit-mask-size: 100% 100%;
  transition: opacity 0.22s ease;
}

.dm-header__blur-layer--1 {
  --hb-blur: 4px;
  mask-image: linear-gradient(to bottom, transparent 0%, transparent 40%, #000 60%, #000 80%, transparent 100%);
  -webkit-mask-image: linear-gradient(to bottom, transparent 0%, transparent 40%, #000 60%, #000 80%, transparent 100%);
}
.dm-header__blur-layer--2 {
  --hb-blur: 6px;
  mask-image: linear-gradient(to bottom, transparent 0%, transparent 20%, #000 40%, #000 60%, transparent 80%, transparent 100%);
  -webkit-mask-image: linear-gradient(to bottom, transparent 0%, transparent 20%, #000 40%, #000 60%, transparent 80%, transparent 100%);
}
.dm-header__blur-layer--3 {
  --hb-blur: 10px;
  mask-image: linear-gradient(to bottom, transparent 0%, #000 20%, #000 40%, transparent 60%, transparent 100%);
  -webkit-mask-image: linear-gradient(to bottom, transparent 0%, #000 20%, #000 40%, transparent 60%, transparent 100%);
}
.dm-header__blur-layer--4 {
  --hb-blur: 18px;
  mask-image: linear-gradient(to bottom, #000 0%, #000 20%, transparent 40%, transparent 100%);
  -webkit-mask-image: linear-gradient(to bottom, #000 0%, #000 20%, transparent 40%, transparent 100%);
}
.dm-header__blur-layer--5 {
  --hb-blur: 34px;
  mask-image: linear-gradient(to bottom, #000 0%, transparent 20%, transparent 100%);
  -webkit-mask-image: linear-gradient(to bottom, #000 0%, transparent 20%, transparent 100%);
}

/* Hero 顶部全透，滚动 >12px 或展开移动端菜单后 0.22s 丝滑浮现 */
.dm-header--transparent:not(.is-scrolled):not(.is-open) .dm-header__blur::after,
.dm-header--transparent:not(.is-scrolled):not(.is-open) .dm-header__blur-layer {
  opacity: 0;
}

.dm-header__inner {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  max-width: 1440px;
  height: 64px;
  margin: 0 auto;
  padding: 0 48px;
}

.dm-header__left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.dm-menu-btn {
  display: none;
  width: 36px;
  height: 36px;
  align-items: center;
  justify-content: center;
  border-radius: 9999px;
  background: rgba(255, 255, 255, 0.36);
  border: 1px solid rgba(20, 20, 19, 0.09);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  color: var(--color-ink);
  transition: background-color 0.2s ease, border-color 0.2s ease;
}
.dm-menu-btn:hover {
  background: rgba(255, 255, 255, 0.72);
  border-color: rgba(20, 20, 19, 0.18);
}

.dm-brand {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  color: var(--color-ink);
  text-decoration: none;
}
.dm-brand strong {
  font-family: var(--font-display);
  font-size: 21px;
  font-weight: 500;
  letter-spacing: -0.015em;
}

.dm-nav {
  display: flex;
  align-items: center;
  gap: 4px;
}

.dm-nav__item {
  display: inline-flex;
  align-items: center;
  height: 36px;
  padding: 0 16px;
  border-radius: 9999px;
  color: var(--color-ink);
  font-size: 14.5px;
  font-weight: 500;
  text-decoration: none;
  transition: background-color 0.2s ease, box-shadow 0.2s ease;
}

.dm-nav__item:hover {
  background: rgba(250, 249, 245, 0.82);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.12);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
}

.dm-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 移动端毛玻璃抽屉 */
.dm-drawer {
  position: absolute;
  top: 64px;
  left: 0;
  right: 0;
  padding: 16px 20px 24px;
  background: rgba(250, 249, 245, 0.94);
  backdrop-filter: blur(22px);
  -webkit-backdrop-filter: blur(22px);
  border-bottom: 1px solid rgba(20, 20, 19, 0.08);
  box-shadow: 0 16px 40px rgba(20, 20, 19, 0.08);
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
  font-size: 16px;
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

/* ===== DeepMind 风格胶囊按钮系统 ===== */
.pill-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 42px;
  padding: 0 20px;
  border-radius: 9999px;
  font-size: 14.5px;
  font-weight: 500;
  line-height: 1;
  white-space: nowrap;
  text-decoration: none;
  transition:
    background-color 0.22s cubic-bezier(0.16, 1, 0.3, 1),
    box-shadow 0.22s cubic-bezier(0.16, 1, 0.3, 1),
    color 0.22s ease,
    transform 0.22s cubic-bezier(0.16, 1, 0.3, 1);
}
.pill-btn:active {
  transform: scale(0.985);
}
.pill-btn--sm {
  height: 36px;
  padding: 0 16px;
  font-size: 14px;
}
.pill-btn--lg {
  height: 48px;
  padding: 0 24px;
  font-size: 15.5px;
}
.pill-btn--ink {
  background: var(--color-ink);
  color: var(--color-on-dark);
  box-shadow: 0 0 0 1px var(--color-ink);
}
.pill-btn--ink:hover {
  background: var(--color-ink-hover);
  box-shadow: 0 0 0 1px var(--color-ink-hover);
  color: #fff;
}
.pill-btn--glass {
  background: rgba(255, 255, 255, 0.42);
  color: var(--color-ink);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.13);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
}
.pill-btn--glass:hover {
  background: rgba(255, 255, 255, 0.78);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.24);
}
.pill-btn--outline {
  background: transparent;
  color: var(--color-ink);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.14);
}
.pill-btn--outline:hover {
  background: rgba(20, 20, 19, 0.04);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.28);
}
.pill-btn--light {
  background: var(--color-on-dark);
  color: var(--color-ink);
}
.pill-btn--light:hover {
  background: #fff;
}
.pill-btn--dark-glass {
  background: rgba(255, 255, 255, 0.08);
  color: var(--color-on-dark);
  box-shadow: 0 0 0 1px rgba(250, 249, 245, 0.22);
}
.pill-btn--dark-glass:hover {
  background: rgba(255, 255, 255, 0.16);
  box-shadow: 0 0 0 1px rgba(250, 249, 245, 0.38);
}
.btn-arrow {
  transition: transform 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}
.pill-btn:hover .btn-arrow {
  transform: translateX(3px);
}

.meta-pill {
  display: inline-flex;
  align-items: center;
  height: 28px;
  padding: 0 12px;
  border-radius: 9999px;
  background: rgba(20, 20, 19, 0.05);
  color: var(--color-text-secondary);
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
}

/* ===== Hero 区块与柔和天光氛围 ===== */
.hero-stage {
  position: relative;
  margin-top: -64px;
  padding-top: 136px;
  padding-bottom: 84px;
  overflow: hidden;
}

.hero-ambient {
  position: absolute;
  inset: 0;
  pointer-events: none;
  z-index: 0;
  overflow: hidden;
}
.hero-ambient__orb {
  position: absolute;
  border-radius: 50%;
  filter: blur(76px);
  opacity: 0.62;
  animation: orb-drift 16s ease-in-out infinite alternate;
}
.hero-ambient__orb--a {
  top: -120px;
  left: 8%;
  width: 520px;
  height: 440px;
  background: radial-gradient(circle, rgba(188, 209, 202, 0.72) 0%, rgba(188, 209, 202, 0) 70%);
}
.hero-ambient__orb--b {
  top: -80px;
  right: 6%;
  width: 540px;
  height: 460px;
  background: radial-gradient(circle, rgba(203, 202, 219, 0.65) 0%, rgba(203, 202, 219, 0) 70%);
  animation-delay: -5s;
}
.hero-ambient__orb--c {
  top: 220px;
  left: 38%;
  width: 460px;
  height: 360px;
  background: radial-gradient(circle, rgba(227, 218, 204, 0.75) 0%, rgba(227, 218, 204, 0) 72%);
  animation-delay: -10s;
}
@keyframes orb-drift {
  0% {
    transform: translate3d(0, 0, 0) scale(1);
  }
  100% {
    transform: translate3d(20px, 14px, 0) scale(1.06);
  }
}

.hero-inner {
  position: relative;
  z-index: 1;
}

.hero-copy {
  max-width: 880px;
  margin: 0 auto 56px;
  text-align: center;
}

.hero-copy h1 {
  margin: 20px auto 0;
  font-family: var(--font-display);
  font-size: clamp(40px, 5.2vw, 72px);
  font-weight: 500;
  line-height: 1.08;
  letter-spacing: -0.025em;
  text-wrap: balance;
  color: var(--color-ink);
}

.hero-lede {
  max-width: 36em;
  margin: 24px auto 34px;
  color: var(--color-text-secondary);
  font-size: clamp(17px, 1.5vw, 20px);
  line-height: 1.65;
}

.hero-cta {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 12px;
}

.hero-note {
  margin: 18px 0 0;
  color: var(--color-muted);
  font-size: 13.5px;
}

/* Hero 底部圆角视觉舞台 */
.hero-showcase {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr);
  gap: 24px;
  align-items: stretch;
}

.stage-card {
  border-radius: 24px;
  padding: 28px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.stage-card--dialogue {
  background: #18191d;
  color: var(--color-on-dark);
  border: 1px solid rgba(255, 255, 255, 0.08);
  box-shadow: 0 24px 60px rgba(20, 20, 19, 0.12);
}

.stage-card__top {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 22px;
  font-size: 13px;
}
.stage-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-accent);
  box-shadow: 0 0 0 4px rgba(91, 122, 74, 0.24);
}
.stage-label {
  color: rgba(250, 249, 245, 0.92);
  font-weight: 500;
}
.stage-meta {
  margin-left: auto;
  color: rgba(250, 249, 245, 0.56);
}

.stage-bubbles {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.chat-bubble {
  align-self: flex-start;
  max-width: 84%;
  margin: 0;
  padding: 11px 16px;
  border-radius: 18px 18px 18px 6px;
  background: rgba(255, 255, 255, 0.1);
  color: rgba(250, 249, 245, 0.94);
  font-size: 15px;
  line-height: 1.55;
  animation: bubble-in 0.55s cubic-bezier(0.16, 1, 0.3, 1) both;
}
.chat-bubble--me {
  align-self: flex-end;
  border-radius: 18px 18px 6px 18px;
  background: var(--color-accent);
  color: #fff;
}
.chat-bubble:nth-child(2) {
  animation-delay: 0.15s;
}
.chat-bubble:nth-child(3) {
  animation-delay: 0.3s;
}
.chat-bubble:nth-child(4) {
  animation-delay: 0.45s;
}
.chat-bubble:nth-child(5) {
  animation-delay: 0.6s;
}
@keyframes bubble-in {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

.stage-tool {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  margin-top: 20px;
  padding: 8px 14px;
  align-self: flex-start;
  border-radius: 9999px;
  background: rgba(255, 255, 255, 0.08);
  color: rgba(250, 249, 245, 0.78);
  font-size: 12.5px;
}

.stage-card--visual {
  background: var(--color-card);
  border: 1px solid rgba(20, 20, 19, 0.06);
  gap: 20px;
}
.stage-visual__art {
  width: 100%;
  max-width: 420px;
  margin: auto;
}
.stage-visual__quote {
  margin: 0;
  font-family: var(--font-display);
  font-size: clamp(19px, 1.8vw, 24px);
  font-weight: 500;
  line-height: 1.48;
  color: var(--color-ink);
  text-wrap: balance;
}
.stage-visual__quote em {
  font-style: normal;
  background: linear-gradient(transparent 64%, var(--illo-sand) 64%);
  padding: 0 2px;
}

/* ===== 通用分区头 ===== */
.section-block {
  padding-top: 84px;
  padding-bottom: 84px;
  border-top: 1px solid rgba(20, 20, 19, 0.07);
  scroll-margin-top: 72px;
}

.sec-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px 24px;
  margin-bottom: 44px;
}

.sec-head h2 {
  margin: 0;
  font-family: var(--font-display);
  font-size: clamp(28px, 2.8vw, 38px);
  font-weight: 500;
  line-height: 1.15;
  letter-spacing: -0.018em;
  color: var(--color-ink);
}

.sec-sub {
  margin: 10px 0 0;
  max-width: 46em;
  color: var(--color-text-secondary);
  font-size: 16px;
  line-height: 1.65;
}

/* ===== 最新文章与动态（DeepMind 不对称双栏） ===== */
.editorial-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.06fr) minmax(0, 1fr);
  gap: 56px;
  align-items: start;
}
.editorial-grid--single {
  grid-template-columns: minmax(0, 1fr);
  max-width: 820px;
}

.editorial-lead-wrap {
  position: sticky;
  top: 92px;
}

.editorial-lead {
  display: flex;
  flex-direction: column;
  gap: 24px;
  text-decoration: none;
  color: inherit;
}

.editorial-lead__head h3 {
  margin: 12px 0 12px;
  font-family: var(--font-display);
  font-size: clamp(28px, 2.9vw, 40px);
  font-weight: 500;
  line-height: 1.18;
  letter-spacing: -0.02em;
  color: var(--color-ink);
  transition: color 0.2s ease;
}
.editorial-lead:hover .editorial-lead__head h3 {
  color: var(--color-ink-hover);
}

.editorial-lead__summary {
  margin: 0 0 16px;
  color: var(--color-text-secondary);
  font-size: 16px;
  line-height: 1.7;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.post-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
  font-size: 13.5px;
  color: var(--color-muted);
}
.post-cat {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding: 0 10px;
  border-radius: 9999px;
  background: rgba(20, 20, 19, 0.06);
  color: var(--color-ink);
  font-size: 12.5px;
  font-weight: 500;
}
.post-flag {
  color: var(--color-accent-active);
  font-weight: 500;
}

.read-more {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 14px;
  font-weight: 500;
  color: var(--color-ink);
}
.read-more__arrow {
  transition: transform 0.22s cubic-bezier(0.16, 1, 0.3, 1);
}
.editorial-lead:hover .read-more__arrow,
.editorial-item:hover .read-more__arrow {
  transform: translateX(4px);
}

.post-cover {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 10;
  border-radius: 22px;
  overflow: hidden;
  background: var(--color-card);
}
.post-cover img,
.editorial-item__thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: transform 0.45s cubic-bezier(0.16, 1, 0.3, 1), filter 0.35s ease;
}
.editorial-lead:hover .post-cover img,
.editorial-item:hover .editorial-item__thumb img {
  transform: scale3d(1.04, 1.04, 1);
  filter: contrast(1.04) brightness(0.98);
}

/* 右侧文章列表 */
.editorial-list {
  display: flex;
  flex-direction: column;
}
.editorial-item-wrap {
  border-top: 1px solid rgba(20, 20, 19, 0.08);
}
.editorial-item-wrap:first-child {
  border-top: 0;
}
.editorial-item-wrap:first-child .editorial-item {
  padding-top: 0;
}
.editorial-item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 156px;
  gap: 20px;
  align-items: center;
  padding: 26px 0;
  text-decoration: none;
  color: inherit;
}
.editorial-item__body {
  min-width: 0;
}
.editorial-item h3 {
  margin: 10px 0 8px;
  font-family: var(--font-display);
  font-size: clamp(20px, 1.8vw, 24px);
  font-weight: 500;
  line-height: 1.28;
  letter-spacing: -0.01em;
  color: var(--color-ink);
  transition: color 0.2s ease;
}
.editorial-item:hover h3 {
  color: var(--color-ink-hover);
}
.editorial-item__summary {
  margin: 0 0 12px;
  color: var(--color-text-secondary);
  font-size: 14.5px;
  line-height: 1.65;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.editorial-item__thumb {
  width: 156px;
  aspect-ratio: 4 / 3;
  border-radius: 16px;
  overflow: hidden;
  background: var(--color-card);
  flex-shrink: 0;
}

/* 5 种 DeepMind 风格抽象几何渐变封面（无外链图时兜底） */
.cover-art {
  position: relative;
  width: 100%;
  height: 100%;
  display: flex;
  align-items: flex-end;
  padding: 20px 22px;
  overflow: hidden;
  transition: transform 0.45s cubic-bezier(0.16, 1, 0.3, 1);
}
.editorial-lead:hover .cover-art,
.editorial-item:hover .cover-art {
  transform: scale3d(1.03, 1.03, 1);
}
.cover-art__geo {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}
.cover-art__label {
  position: relative;
  z-index: 1;
  font-size: 13px;
  font-weight: 500;
  padding: 5px 12px;
  border-radius: 9999px;
  background: rgba(255, 255, 255, 0.14);
  backdrop-filter: blur(8px);
}
.cover-art--compact {
  padding: 0;
}
.cover-art--obsidian {
  color: #f4f3ee;
  background:
    radial-gradient(circle at 82% 20%, rgba(132, 168, 120, 0.42), transparent 52%),
    radial-gradient(circle at 18% 86%, rgba(128, 146, 204, 0.32), transparent 56%),
    linear-gradient(135deg, #15161a 0%, #24262b 100%);
}
.cover-art--aurora {
  color: #f4f7f4;
  background:
    radial-gradient(circle at 22% 18%, rgba(154, 198, 184, 0.58), transparent 54%),
    radial-gradient(circle at 82% 78%, rgba(164, 158, 212, 0.46), transparent 58%),
    linear-gradient(135deg, #1d2a30 0%, #2c3e45 55%, #37483c 100%);
}
.cover-art--cobalt {
  color: #f2f5fb;
  background:
    radial-gradient(circle at 76% 24%, rgba(120, 166, 224, 0.54), transparent 54%),
    radial-gradient(circle at 20% 82%, rgba(168, 178, 224, 0.36), transparent 56%),
    linear-gradient(135deg, #18233a 0%, #253b64 100%);
}
.cover-art--sage {
  color: #f5f7f2;
  background:
    radial-gradient(circle at 78% 22%, rgba(196, 218, 178, 0.48), transparent 52%),
    radial-gradient(circle at 22% 84%, rgba(114, 150, 98, 0.42), transparent 56%),
    linear-gradient(135deg, #253322 0%, #445d39 100%);
}
.cover-art--sand {
  color: #262522;
  background:
    radial-gradient(circle at 80% 24%, rgba(255, 255, 255, 0.65), transparent 52%),
    radial-gradient(circle at 22% 84%, rgba(198, 182, 154, 0.52), transparent 56%),
    linear-gradient(135deg, #e5dccb 0%, #f3eee4 100%);
}
.cover-art--sand .cover-art__label {
  background: rgba(20, 20, 19, 0.08);
}

/* 骨架与空状态 */
.skel {
  border-radius: 14px;
  background: linear-gradient(90deg, var(--color-canvas-soft) 0%, var(--color-surface-hover) 45%, var(--color-canvas-soft) 90%);
  background-size: 200% 100%;
  animation: skel-move 1.4s linear infinite;
}
@keyframes skel-move {
  from {
    background-position: 100% 0;
  }
  to {
    background-position: -100% 0;
  }
}
.skel--title {
  height: 44px;
  width: 82%;
}
.skel--meta {
  height: 18px;
  width: 44%;
}
.skel--cover {
  aspect-ratio: 16 / 10;
  width: 100%;
  border-radius: 22px;
}
.editorial-item--skel {
  display: grid;
  grid-template-columns: 1fr;
  gap: 12px;
}
.skel--line {
  height: 24px;
  width: 86%;
}
.skel--short {
  height: 16px;
  width: 60%;
}

.editorial-empty {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 36px 40px;
  border-radius: 24px;
  background: var(--color-card);
}
.editorial-empty h3 {
  margin: 12px 0 8px;
  font-family: var(--font-display);
  font-size: 24px;
  font-weight: 500;
}
.editorial-empty p {
  margin: 0;
  color: var(--color-text-secondary);
  font-size: 15.5px;
  line-height: 1.7;
}

/* ===== 核心能力 Bento 展示卡 ===== */
.bento-grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 20px;
}

.bento-card {
  grid-column: span 2;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 24px;
  padding: 28px;
  border-radius: 24px;
  overflow: hidden;
}

.bento-card--wide {
  grid-column: span 6;
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 0.85fr);
  align-items: center;
  gap: 40px;
  padding: 36px 40px;
}

.bento-card--light {
  background: var(--color-card);
  color: var(--color-ink);
}
.bento-card--dark {
  background: #18191d;
  color: var(--color-on-dark);
}

.bento-tag {
  display: inline-flex;
  align-items: center;
  height: 26px;
  padding: 0 11px;
  border-radius: 9999px;
  background: rgba(20, 20, 19, 0.06);
  color: var(--color-text-secondary);
  font-size: 12.5px;
  font-weight: 500;
}
.bento-tag--dark {
  background: rgba(255, 255, 255, 0.1);
  color: rgba(250, 249, 245, 0.82);
}

.bento-card h3 {
  margin: 16px 0 12px;
  font-family: var(--font-display);
  font-size: clamp(21px, 2vw, 28px);
  font-weight: 500;
  line-height: 1.26;
  letter-spacing: -0.015em;
}
.bento-card--dark h3 {
  color: var(--color-on-dark);
}

.bento-card__desc {
  margin: 0 0 18px;
  font-size: 15.5px;
  line-height: 1.75;
  color: var(--color-text-secondary);
}
.bento-card--dark .bento-card__desc {
  color: rgba(250, 249, 245, 0.72);
}

.bento-points {
  display: grid;
  gap: 9px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.bento-points li {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 14.5px;
  line-height: 1.55;
}
.bento-points li :deep(svg) {
  margin-top: 3px;
  flex-shrink: 0;
  color: var(--color-accent);
}
.bento-card--dark .bento-points li :deep(svg) {
  color: var(--illo-sage);
}

.bento-card__art {
  width: 100%;
  border-radius: 18px;
  padding: 20px;
  background: rgba(250, 249, 245, 0.65);
}
.bento-card--dark .bento-card__art {
  background: rgba(250, 249, 245, 0.94);
  padding: 24px;
}
.bento-card__art :deep(.illo) {
  max-width: 320px;
  margin: 0 auto;
}

/* ===== 三步上手 ===== */
.steps-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 20px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.step-card {
  padding: 32px;
  border-radius: 24px;
  background: var(--color-card);
}
.step-card__num {
  display: inline-grid;
  place-items: center;
  width: 42px;
  height: 42px;
  border-radius: 9999px;
  border: 1px solid rgba(20, 20, 19, 0.16);
  font-family: var(--font-mono);
  font-size: 14px;
  font-weight: 500;
  color: var(--color-ink);
}
.step-card:nth-child(1) .step-card__num {
  background: var(--illo-mist);
}
.step-card:nth-child(2) .step-card__num {
  background: var(--illo-sand);
}
.step-card:nth-child(3) .step-card__num {
  background: var(--illo-lavender);
}
.step-card h3 {
  margin: 22px 0 10px;
  font-family: var(--font-display);
  font-size: 22px;
  font-weight: 500;
  line-height: 1.3;
}
.step-card p {
  margin: 0;
  color: var(--color-text-secondary);
  font-size: 15.5px;
  line-height: 1.72;
}

/* ===== 旗下产品 BuugooArena ===== */
.arena-panel {
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.15fr);
  gap: 48px;
  align-items: center;
  padding: 44px;
  border-radius: 24px;
  background: var(--color-card);
}
.arena-panel__art {
  width: 100%;
  max-width: 400px;
  margin: 0 auto;
}
.arena-panel__body {
  min-width: 0;
}
.arena-panel__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
  margin-bottom: 14px;
}
.arena-panel__name {
  font-family: var(--font-display);
  font-size: 21px;
  font-weight: 500;
  color: var(--color-ink);
}
.arena-panel__domain {
  font-family: var(--font-mono);
  font-size: 13px;
  color: var(--color-muted);
}
.arena-panel__body h3 {
  margin: 0 0 12px;
  font-family: var(--font-display);
  font-size: clamp(24px, 2.4vw, 34px);
  font-weight: 500;
  line-height: 1.22;
  letter-spacing: -0.015em;
}
.arena-panel__desc {
  margin: 0 0 22px;
  color: var(--color-text-secondary);
  font-size: 15.5px;
  line-height: 1.75;
}
.arena-cols {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 22px;
}
.arena-col {
  display: block;
  padding: 18px 20px;
  border-radius: 18px;
  border: 1px solid rgba(20, 20, 19, 0.08);
  background: var(--color-canvas);
  text-decoration: none;
  transition: border-color 0.2s ease, transform 0.22s cubic-bezier(0.16, 1, 0.3, 1);
}
.arena-col:hover {
  border-color: rgba(20, 20, 19, 0.22);
  transform: translateY(-2px);
}
.arena-col__top {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.arena-col__num {
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 600;
  color: var(--color-accent);
}
.arena-col__top strong {
  font-size: 15px;
  font-weight: 600;
  color: var(--color-ink);
}
.arena-col__arrow {
  margin-left: auto;
  flex-shrink: 0;
  color: var(--color-muted);
  transition: transform 0.2s cubic-bezier(0.16, 1, 0.3, 1), color 0.2s ease;
}
.arena-col:hover .arena-col__arrow {
  color: var(--color-ink);
  transform: translateX(3px);
}
.arena-col p {
  margin: 0;
  color: var(--color-text-secondary);
  font-size: 13.5px;
  line-height: 1.62;
}
.arena-points {
  display: grid;
  gap: 9px;
  margin: 0 0 28px;
  padding: 0;
  list-style: none;
}
.arena-points li {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 14.5px;
  line-height: 1.55;
  color: var(--color-ink);
}
.arena-points li :deep(svg) {
  margin-top: 3px;
  flex-shrink: 0;
  color: var(--color-accent);
}
.arena-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

/* ===== 收尾行动卡 ===== */
.final-block {
  padding-top: 24px;
  padding-bottom: 84px;
}
.final-card {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 0.9fr);
  align-items: center;
  gap: 36px;
  padding: 64px 72px;
  border-radius: 28px;
  background: var(--color-surface-dark);
  color: var(--color-on-dark);
  overflow: hidden;
}
.final-card__glow {
  position: absolute;
  top: -120px;
  right: -80px;
  width: 420px;
  height: 420px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(143, 166, 127, 0.26) 0%, rgba(143, 166, 127, 0) 70%);
  pointer-events: none;
}
.final-card__text {
  position: relative;
  z-index: 1;
}
.final-card h2 {
  margin: 0 0 14px;
  font-family: var(--font-display);
  font-size: clamp(30px, 3.4vw, 46px);
  font-weight: 500;
  line-height: 1.14;
  letter-spacing: -0.02em;
  color: var(--color-on-dark);
}
.final-card p {
  margin: 0 0 30px;
  color: rgba(250, 249, 245, 0.72);
  font-size: 17.5px;
  line-height: 1.65;
}
.final-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.final-card__art {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 340px;
  justify-self: end;
}

/* ===== 极简分组页脚 ===== */
.dm-footer {
  padding: 56px 0 44px;
  border-top: 1px solid rgba(20, 20, 19, 0.08);
}
.dm-footer__grid {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) repeat(3, minmax(0, 1fr));
  gap: 36px;
  padding-bottom: 40px;
}
.dm-brand--foot strong {
  font-size: 18px;
}
.dm-footer__statement {
  margin: 16px 0 0;
  max-width: 20em;
  font-family: var(--font-display);
  font-size: clamp(20px, 2vw, 24px);
  font-weight: 500;
  line-height: 1.42;
  color: var(--color-ink);
}
.dm-footer__heading {
  margin: 0 0 14px;
  font-size: 14px;
  font-weight: 600;
  color: var(--color-ink);
}
.dm-footer__col ul {
  display: grid;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.dm-footer__col a,
.dm-footer__col button {
  padding: 0;
  color: var(--color-text-secondary);
  font-size: 14.5px;
  line-height: 1.5;
  text-align: left;
  transition: color 0.18s ease;
}
.dm-footer__col a:hover,
.dm-footer__col button:hover {
  color: var(--color-ink);
}
.dm-footer__bottom {
  padding-top: 24px;
  border-top: 1px solid rgba(20, 20, 19, 0.07);
}
.dm-footer__bottom p {
  margin: 0;
  color: var(--color-muted);
  font-size: 13px;
  line-height: 1.65;
}

/* ===== 滚动渐显 ===== */
.reveal {
  opacity: 0;
  transform: translateY(18px);
  transition: opacity 0.65s cubic-bezier(0.16, 1, 0.3, 1), transform 0.65s cubic-bezier(0.16, 1, 0.3, 1);
}
.reveal.is-in {
  opacity: 1;
  transform: none;
}

/* ===== 减少动态偏好 ===== */
@media (prefers-reduced-motion: reduce) {
  .site {
    scroll-behavior: auto;
  }
  .reveal {
    opacity: 1;
    transform: none;
    transition: none;
  }
  .hero-ambient__orb,
  .chat-bubble,
  .skel {
    animation: none;
  }
  .dm-header__blur::after,
  .dm-header__blur-layer,
  .pill-btn,
  .btn-arrow,
  .read-more__arrow,
  .post-cover img,
  .editorial-item__thumb img,
  .cover-art,
  .arena-col,
  .arena-col__arrow {
    transition: none;
  }
  .editorial-lead:hover .post-cover img,
  .editorial-item:hover .editorial-item__thumb img,
  .editorial-lead:hover .cover-art,
  .editorial-item:hover .cover-art,
  .editorial-lead:hover .read-more__arrow,
  .editorial-item:hover .read-more__arrow,
  .pill-btn:hover .btn-arrow,
  .arena-col:hover,
  .arena-col:hover .arena-col__arrow {
    transform: none;
    filter: none;
  }
}

/* ===== 响应式布局 ===== */
@media (max-width: 1120px) {
  .wrap,
  .dm-header__inner {
    padding-left: 32px;
    padding-right: 32px;
  }
  .bento-card {
    grid-column: span 3;
  }
  .bento-card--wide {
    grid-column: span 6;
  }
}

@media (max-width: 900px) {
  .wrap,
  .dm-header__inner {
    padding-left: 20px;
    padding-right: 20px;
  }
  .dm-menu-btn {
    display: inline-flex;
  }
  .dm-nav {
    display: none;
  }
  .hero-stage {
    padding-top: 108px;
    padding-bottom: 56px;
  }
  .hero-copy {
    text-align: left;
    margin-bottom: 36px;
  }
  .hero-copy h1 {
    margin-left: 0;
    margin-right: 0;
    font-size: clamp(34px, 8.6vw, 44px);
    line-height: 1.12;
  }
  .hero-lede {
    margin: 18px 0 26px;
    font-size: 16.5px;
  }
  .hero-cta {
    justify-content: flex-start;
  }
  .hero-showcase {
    grid-template-columns: minmax(0, 1fr);
  }
  .section-block {
    padding-top: 56px;
    padding-bottom: 56px;
  }
  .sec-head {
    margin-bottom: 28px;
  }
  .editorial-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 36px;
  }
  .editorial-lead-wrap {
    position: static;
  }
  .editorial-item {
    grid-template-columns: minmax(0, 1fr) 112px;
    gap: 14px;
    padding: 20px 0;
  }
  .editorial-item__thumb {
    width: 112px;
  }
  .bento-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .bento-card,
  .bento-card--wide {
    grid-column: auto;
    grid-template-columns: minmax(0, 1fr);
    padding: 24px 22px;
    gap: 20px;
  }
  .steps-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .arena-panel {
    grid-template-columns: minmax(0, 1fr);
    gap: 28px;
    padding: 28px 22px;
  }
  .arena-cols {
    grid-template-columns: minmax(0, 1fr);
  }
  .final-card {
    grid-template-columns: minmax(0, 1fr);
    padding: 40px 26px;
  }
  .final-card__art {
    justify-self: center;
    max-width: 260px;
  }
  .dm-footer__grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 28px;
  }
  .dm-footer__mission {
    grid-column: 1 / -1;
  }
}

@media (max-width: 480px) {
  .stage-card {
    padding: 22px 18px;
  }
  .editorial-item {
    grid-template-columns: minmax(0, 1fr);
  }
  .editorial-item__thumb {
    width: 100%;
    aspect-ratio: 16 / 9;
  }
  .editorial-empty {
    padding: 24px 20px;
  }
  .hero-cta .pill-btn,
  .arena-actions .pill-btn,
  .final-card__actions .pill-btn {
    flex: 1 1 100%;
  }
  .dm-footer__grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
