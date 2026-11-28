<script setup lang="ts">
// 公开文章列表页：延续首页的 DeepMind 渐进模糊顶栏与编辑式卡片布局，支持分类、标签与关键词筛选。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from '../Icon.vue'
import { api, type CmsPostSummary } from '../api'
import { setPageSeo, toAbsoluteUrl } from '../seo'
import { needsLogin, session } from '../session'

const route = useRoute()
const router = useRouter()

const name = computed(() => session.meta?.name ?? '布谷')
const isLoggedIn = computed(() => !needsLogin())
const canRegister = computed(() => (session.meta?.registration ?? 'closed') !== 'closed')
const homeTo = computed(() => (isLoggedIn.value ? '/' : '/welcome'))
const startTo = computed(() =>
  isLoggedIn.value ? { path: '/' } : canRegister.value ? { path: '/login', query: { tab: 'register' } } : { path: '/login' },
)

const posts = ref<CmsPostSummary[]>([])
const categories = ref<string[]>([])
const tags = ref<string[]>([])
const loading = ref(true)
const loadError = ref('')
const failedCovers = ref<Record<string, boolean>>({})

const activeCategory = ref('')
const activeTag = ref('')
const searchInput = ref('')
const appliedQuery = ref('')

const mobileMenuOpen = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | undefined

const hasActiveFilter = computed(() => !!(activeCategory.value || activeTag.value || appliedQuery.value))

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

function syncFromRoute() {
  const qCat = typeof route.query.category === 'string' ? route.query.category.trim() : ''
  const qTag = typeof route.query.tag === 'string' ? route.query.tag.trim() : ''
  const qText = typeof route.query.q === 'string' ? route.query.q.trim() : ''
  activeCategory.value = qCat
  activeTag.value = qTag
  searchInput.value = qText
  appliedQuery.value = qText
}

function pushFilterQuery(cat: string, tag: string, q: string) {
  const query: Record<string, string> = {}
  if (cat) query.category = cat
  if (tag) query.tag = tag
  if (q) query.q = q
  router.replace({ path: '/posts', query })
}

function selectCategory(cat: string) {
  const next = activeCategory.value === cat ? '' : cat
  pushFilterQuery(next, activeTag.value, appliedQuery.value)
}

function selectTag(tag: string) {
  const next = activeTag.value === tag ? '' : tag
  pushFilterQuery(activeCategory.value, next, appliedQuery.value)
}

function onSearchSubmit() {
  clearTimeout(searchTimer)
  pushFilterQuery(activeCategory.value, activeTag.value, searchInput.value.trim())
}

function onSearchInput() {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    const trimmed = searchInput.value.trim()
    if (trimmed !== appliedQuery.value) {
      pushFilterQuery(activeCategory.value, activeTag.value, trimmed)
    }
  }, 280)
}

function clearFilters() {
  clearTimeout(searchTimer)
  searchInput.value = ''
  pushFilterQuery('', '', '')
}

let reqSeq = 0
async function fetchPosts() {
  const seq = ++reqSeq
  loading.value = true
  loadError.value = ''
  try {
    const res = await api.cmsPosts({
      category: activeCategory.value || undefined,
      tag: activeTag.value || undefined,
      q: appliedQuery.value || undefined,
    })
    if (seq !== reqSeq) return
    posts.value = Array.isArray(res.posts) ? res.posts : []
    categories.value = Array.isArray(res.categories) ? res.categories : []
    tags.value = Array.isArray(res.tags) ? res.tags : []
  } catch (e) {
    if (seq !== reqSeq) return
    posts.value = []
    loadError.value = e instanceof Error ? e.message : '暂时无法加载文章列表，请稍后重试。'
  } finally {
    if (seq === reqSeq) loading.value = false
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && mobileMenuOpen.value) mobileMenuOpen.value = false
}

watch(
  () => [route.query.category, route.query.tag, route.query.q],
  () => {
    syncFromRoute()
    fetchPosts()
  },
  { immediate: true },
)

watch(
  [name, activeCategory, activeTag, appliedQuery],
  () => {
    const siteName = name.value
    const pageTitle = activeCategory.value
      ? `${activeCategory.value} - 文章与动态 - ${siteName}`
      : activeTag.value
        ? `#${activeTag.value} - 文章与动态 - ${siteName}`
        : `文章与动态 - ${siteName}`
    const desc = `记录${siteName}的产品更新、记忆与情绪设计手记，以及日常使用指南。`
    const homeUrl = toAbsoluteUrl('/welcome')
    const postsUrl = toAbsoluteUrl('/posts')
    setPageSeo({
      title: pageTitle,
      description: desc,
      robots: appliedQuery.value ? 'noindex, follow' : 'index, follow',
      canonicalPath: '/posts',
      ogType: 'website',
      siteName,
      image: '/icon-512.png',
      jsonLd: [
        {
          '@context': 'https://schema.org',
          '@type': 'CollectionPage',
          name: `文章与动态 - ${siteName}`,
          description: desc,
          url: postsUrl,
          inLanguage: 'zh-CN',
        },
        {
          '@context': 'https://schema.org',
          '@type': 'BreadcrumbList',
          itemListElement: [
            { '@type': 'ListItem', position: 1, name: '首页', item: homeUrl },
            { '@type': 'ListItem', position: 2, name: '文章与动态', item: postsUrl },
          ],
        },
      ],
    })
  },
  { immediate: true },
)

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  clearTimeout(searchTimer)
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div class="site">
    <!-- 常驻 5 层渐进渐变模糊顶栏 -->
    <header class="dm-header" :class="{ 'is-open': mobileMenuOpen }">
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
          <RouterLink :to="homeTo" class="dm-brand" @click="mobileMenuOpen = false">
            <img src="/logo.png" :alt="`${name}标志`" width="28" height="28" />
            <strong>{{ name }}</strong>
          </RouterLink>
        </div>

        <nav class="dm-nav" aria-label="主导航">
          <RouterLink :to="homeTo" class="dm-nav__item">首页</RouterLink>
          <RouterLink to="/posts" class="dm-nav__item is-active">文章</RouterLink>
          <a href="https://arena.buugoo.com" target="_blank" rel="noopener noreferrer" class="dm-nav__item">
            BuugooArena
          </a>
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

      <Transition name="dm-drawer">
        <div v-if="mobileMenuOpen" class="dm-drawer" role="dialog" aria-label="移动端导航">
          <nav class="dm-drawer__nav">
            <RouterLink :to="homeTo" class="dm-drawer__link" @click="mobileMenuOpen = false">
              <span>返回首页</span>
              <Icon name="chevron" :size="16" />
            </RouterLink>
            <RouterLink to="/posts" class="dm-drawer__link" @click="mobileMenuOpen = false">
              <span>全部文章</span>
              <Icon name="chevron" :size="16" />
            </RouterLink>
            <a
              href="https://arena.buugoo.com"
              target="_blank"
              rel="noopener noreferrer"
              class="dm-drawer__link"
              @click="mobileMenuOpen = false"
            >
              <span>旗下评测 · BuugooArena</span>
              <Icon name="chevron" :size="16" />
            </a>
          </nav>
        </div>
      </Transition>
    </header>

    <main class="wrap posts-main">
      <!-- 页面导言与搜索栏 -->
      <section class="posts-hero">
        <div class="posts-hero__copy">
          <span class="meta-pill">文章与动态</span>
          <h1>记录{{ name }}的演进与思考</h1>
          <p class="posts-hero__desc">产品更新、记忆与情绪设计手记、以及日常使用指南，都在这里。</p>
        </div>

        <div class="posts-hero__tools">
          <form class="search-bar" role="search" @submit.prevent="onSearchSubmit">
            <Icon name="search" :size="17" class="search-bar__ico" />
            <input
              v-model="searchInput"
              type="search"
              placeholder="搜索文章标题、摘要或主题"
              aria-label="搜索文章"
              @input="onSearchInput"
            />
            <button v-if="searchInput" type="button" class="search-bar__clear" aria-label="清空搜索" @click="searchInput = ''; onSearchSubmit()">
              <Icon name="x" :size="15" />
            </button>
          </form>
          <a href="/rss.xml" target="_blank" rel="noopener noreferrer" class="pill-btn pill-btn--outline rss-pill">
            RSS 订阅
          </a>
        </div>
      </section>

      <!-- 分类与标签筛选栏 -->
      <section v-if="categories.length > 0 || tags.length > 0 || hasActiveFilter" class="filter-bar" aria-label="文章筛选">
        <div v-if="categories.length > 0" class="filter-row">
          <button
            type="button"
            class="filter-pill"
            :class="{ 'is-active': !activeCategory }"
            @click="pushFilterQuery('', activeTag, appliedQuery)"
          >
            全部分类
          </button>
          <button
            v-for="cat in categories"
            :key="cat"
            type="button"
            class="filter-pill"
            :class="{ 'is-active': activeCategory === cat }"
            @click="selectCategory(cat)"
          >
            {{ cat }}
          </button>
        </div>

        <div v-if="tags.length > 0" class="filter-tags">
          <span class="filter-tags__label">标签</span>
          <button
            v-for="tag in tags"
            :key="tag"
            type="button"
            class="tag-chip"
            :class="{ 'is-active': activeTag === tag }"
            @click="selectTag(tag)"
          >
            #{{ tag }}
          </button>
          <button v-if="hasActiveFilter" type="button" class="filter-reset" @click="clearFilters">
            清除筛选
          </button>
        </div>
      </section>

      <!-- 加载状态 -->
      <section v-if="loading" class="posts-grid" aria-busy="true">
        <div v-for="n in 6" :key="n" class="post-card post-card--skel">
          <div class="skel skel--cover" />
          <div class="post-card__body">
            <div class="skel skel--meta" />
            <div class="skel skel--title" />
            <div class="skel skel--line" />
          </div>
        </div>
      </section>

      <!-- 错误状态 -->
      <section v-else-if="loadError" class="state-panel">
        <h2>加载文章失败</h2>
        <p>{{ loadError }}</p>
        <button type="button" class="pill-btn pill-btn--ink" @click="fetchPosts">重新加载</button>
      </section>

      <!-- 文章网格 -->
      <section v-else-if="posts.length > 0" class="posts-grid">
        <article
          v-for="(post, idx) in posts"
          :key="post.id"
          class="post-card-wrap"
          :class="{ 'post-card-wrap--featured': idx === 0 && !hasActiveFilter && posts.length >= 2 }"
        >
          <RouterLink
            :to="`/posts/${post.slug}`"
            class="post-card"
            :class="{ 'post-card--featured': idx === 0 && !hasActiveFilter && posts.length >= 2 }"
          >
            <div class="post-card__cover">
              <img
                v-if="resolveCoverSrc(post)"
                :src="resolveCoverSrc(post)"
                :alt="post.title"
                loading="lazy"
                @error="onCoverError(post.slug)"
              />
              <div v-else class="cover-art" :class="coverThemeClass(post.cover_theme, idx)" aria-hidden="true">
                <svg class="cover-art__geo" viewBox="0 0 640 400" fill="none">
                  <circle cx="510" cy="96" r="144" stroke="currentColor" stroke-opacity="0.24" stroke-width="1.2" />
                  <circle cx="510" cy="96" r="84" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.2" />
                  <path d="M40 330 C 180 230, 320 290, 600 130" stroke="currentColor" stroke-opacity="0.26" stroke-width="1.4" />
                </svg>
                <span class="cover-art__label">{{ post.category || name }}</span>
              </div>
            </div>

            <div class="post-card__body">
              <div class="post-meta">
                <span v-if="post.category" class="post-cat">{{ post.category }}</span>
                <span v-if="post.pinned" class="post-flag">置顶</span>
                <span v-else-if="post.featured" class="post-flag">精选</span>
                <time v-if="post.published_at || post.created_at" :datetime="post.published_at || post.created_at">
                  {{ formatDate(post.published_at || post.created_at) }}
                </time>
              </div>
              <h2>{{ post.title }}</h2>
              <p v-if="post.summary" class="post-card__summary">{{ post.summary }}</p>
              <div class="post-card__foot">
                <div v-if="post.tags && post.tags.length" class="post-card__tags">
                  <span v-for="t in post.tags.slice(0, 3)" :key="t">#{{ t }}</span>
                </div>
                <span class="read-more">
                  <span>阅读全文</span>
                  <Icon name="chevron" :size="14" class="read-more__arrow" />
                </span>
              </div>
            </div>
          </RouterLink>
        </article>
      </section>

      <!-- 空状态 -->
      <section v-else class="state-panel">
        <h2>{{ hasActiveFilter ? '没有找到匹配的文章' : '还没有公开发布的文章' }}</h2>
        <p>
          {{
            hasActiveFilter
              ? '可以换个关键词，或清除当前的分类和标签筛选再看看。'
              : '等管理员在后台发布内容后，这里会第一时间展示。'
          }}
        </p>
        <div class="state-panel__actions">
          <button v-if="hasActiveFilter" type="button" class="pill-btn pill-btn--ink" @click="clearFilters">清除筛选</button>
          <RouterLink :to="homeTo" class="pill-btn pill-btn--outline">返回首页</RouterLink>
        </div>
      </section>
    </main>

    <footer class="dm-footer">
      <div class="wrap dm-footer__row">
        <RouterLink :to="homeTo" class="dm-brand dm-brand--foot">
          <img src="/logo.png" :alt="`${name}标志`" width="22" height="22" />
          <strong>{{ name }}</strong>
        </RouterLink>
        <div class="dm-footer__links">
          <RouterLink :to="homeTo">首页</RouterLink>
          <a href="/rss.xml" target="_blank" rel="noopener noreferrer">RSS 订阅</a>
          <a href="https://arena.buugoo.com" target="_blank" rel="noopener noreferrer">BuugooArena</a>
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
}

.site h1,
.site h2,
.site h3,
.site h4 {
  word-break: normal;
  overflow-wrap: anywhere;
  text-wrap: wrap;
}

.wrap {
  width: 100%;
  max-width: 1440px;
  margin: 0 auto;
  padding-left: 72px;
  padding-right: 72px;
}

/* ===== 常驻 5 层渐进渐变模糊顶栏 ===== */
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
    rgba(250, 249, 245, 1) 0%,
    rgba(250, 249, 245, 0.85) 20%,
    rgba(250, 249, 245, 0.55) 45%,
    rgba(250, 249, 245, 0.3) 70%,
    rgba(250, 249, 245, 0) 100%
  );
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
  gap: 10px;
}
.dm-menu-btn {
  display: none;
  width: 36px;
  height: 36px;
  align-items: center;
  justify-content: center;
  border-radius: 9999px;
  background: rgba(255, 255, 255, 0.42);
  border: 1px solid rgba(20, 20, 19, 0.1);
  -webkit-backdrop-filter: blur(12px);
  backdrop-filter: blur(12px);
}
.dm-brand {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  color: var(--color-ink);
  text-decoration: none;
}
.dm-brand strong {
  font-family: var(--font-sans);
  font-size: 18px;
  font-weight: 500;
  letter-spacing: -0.02em;
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
  transition: background-color 0.2s ease, box-shadow 0.2s ease;
}
.dm-nav__item:hover,
.dm-nav__item.is-active {
  background: rgba(250, 249, 245, 0.85);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.12);
}
.dm-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.dm-drawer {
  position: absolute;
  top: 64px;
  left: 0;
  right: 0;
  z-index: 2;
  padding: 14px 20px 20px;
  background: rgba(250, 249, 245, 0.95);
  -webkit-backdrop-filter: blur(22px);
  backdrop-filter: blur(22px);
  border-bottom: 1px solid rgba(20, 20, 19, 0.08);
}
.dm-drawer__nav {
  display: grid;
  gap: 4px;
}
.dm-drawer__link {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
  border-radius: 14px;
  color: var(--color-ink);
  font-size: 15.5px;
  font-weight: 500;
}
.dm-drawer-enter-active,
.dm-drawer-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}
.dm-drawer-enter-from,
.dm-drawer-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}

/* 胶囊按钮 */
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
  transition: background-color 0.2s ease, box-shadow 0.2s ease;
}
.pill-btn--sm {
  height: 36px;
  padding: 0 16px;
  font-size: 14px;
}
.pill-btn--ink {
  background: var(--color-ink);
  color: var(--color-on-dark);
}
.pill-btn--ink:hover {
  background: var(--color-ink-hover);
  color: #fff;
}
.pill-btn--glass {
  background: rgba(255, 255, 255, 0.45);
  color: var(--color-ink);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.13);
}
.pill-btn--glass:hover {
  background: rgba(255, 255, 255, 0.8);
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
}

/* ===== 页面主体 ===== */
.posts-main {
  padding-top: 48px;
  padding-bottom: 84px;
}

.posts-hero {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
  gap: 24px 36px;
  padding-bottom: 36px;
  border-bottom: 1px solid rgba(20, 20, 19, 0.07);
}
.posts-hero__copy {
  max-width: 680px;
}
.posts-hero h1 {
  margin: 16px 0 12px;
  font-family: var(--font-sans);
  font-size: clamp(34px, 4.4vw, 56px);
  font-weight: 500;
  line-height: 1.1;
  letter-spacing: -0.03em;
  word-break: normal;
  overflow-wrap: anywhere;
}
.posts-hero__desc {
  margin: 0;
  color: var(--color-text-secondary);
  font-size: 17px;
  line-height: 1.65;
}

.posts-hero__tools {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  width: min(468px, 100%);
}
.rss-pill {
  height: 46px;
  padding: 0 18px;
}

.search-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1 1 260px;
  min-width: 0;
  height: 46px;
  padding: 0 16px;
  border-radius: 9999px;
  background: var(--color-card);
  border: 1px solid rgba(20, 20, 19, 0.09);
  transition: border-color 0.2s ease, background-color 0.2s ease;
}
.search-bar:focus-within {
  background: var(--color-surface);
  border-color: rgba(20, 20, 19, 0.28);
}
.search-bar__ico {
  color: var(--color-muted);
  flex-shrink: 0;
}
.search-bar input {
  flex: 1;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--color-ink);
  font-size: 14.5px;
}
.search-bar input::-webkit-search-cancel-button {
  display: none;
}
.search-bar__clear {
  display: inline-grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: 9999px;
  color: var(--color-muted);
}
.search-bar__clear:hover {
  background: rgba(20, 20, 19, 0.06);
  color: var(--color-ink);
}

/* 筛选条 */
.filter-bar {
  display: grid;
  gap: 14px;
  padding: 24px 0 32px;
}
.filter-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.filter-pill {
  height: 36px;
  padding: 0 16px;
  border-radius: 9999px;
  font-size: 14px;
  font-weight: 500;
  color: var(--color-text-secondary);
  background: transparent;
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.12);
  transition: background-color 0.18s ease, color 0.18s ease, box-shadow 0.18s ease;
}
.filter-pill:hover {
  color: var(--color-ink);
  box-shadow: 0 0 0 1px rgba(20, 20, 19, 0.26);
}
.filter-pill.is-active {
  background: var(--color-ink);
  color: var(--color-on-dark);
  box-shadow: 0 0 0 1px var(--color-ink);
}

.filter-tags {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.filter-tags__label {
  font-size: 13px;
  color: var(--color-muted);
  margin-right: 2px;
}
.tag-chip {
  height: 28px;
  padding: 0 11px;
  border-radius: 9999px;
  font-size: 13px;
  color: var(--color-text-secondary);
  background: var(--color-card);
  transition: background-color 0.18s ease, color 0.18s ease;
}
.tag-chip:hover {
  color: var(--color-ink);
  background: var(--color-card-strong);
}
.tag-chip.is-active {
  background: var(--color-accent-tint);
  color: var(--color-accent-active);
  font-weight: 500;
}
.filter-reset {
  margin-left: 6px;
  font-size: 13px;
  color: var(--color-muted);
  text-decoration: underline;
  text-underline-offset: 3px;
}
.filter-reset:hover {
  color: var(--color-ink);
}

/* 文章卡片网格 */
.posts-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 28px 24px;
  padding-top: 8px;
}

.post-card-wrap {
  display: flex;
  min-width: 0;
}
.post-card-wrap--featured {
  grid-column: 1 / -1;
}

.post-card {
  display: flex;
  flex: 1;
  flex-direction: column;
  border-radius: var(--radius-card);
  border: 1px solid rgba(20, 20, 19, 0.05);
  background: var(--color-card);
  overflow: hidden;
  text-decoration: none;
  color: inherit;
  transition: transform 0.28s cubic-bezier(0.16, 1, 0.3, 1);
}
.post-card:hover {
  transform: translateY(-2px);
}

.post-card--featured {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr);
  align-items: stretch;
}
.post-card--featured .post-card__cover {
  aspect-ratio: auto;
  min-height: 320px;
}
.post-card--featured .post-card__body {
  padding: 36px 40px;
  justify-content: center;
}
.post-card--featured h2 {
  font-size: clamp(26px, 2.6vw, 36px);
  line-height: 1.18;
  letter-spacing: -0.028em;
}

.post-card__cover {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 10;
  overflow: hidden;
  background: var(--color-card-strong);
}
.post-card__cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: transform 0.45s cubic-bezier(0.16, 1, 0.3, 1), filter 0.35s ease;
}
.post-card:hover .post-card__cover img {
  transform: scale3d(1.04, 1.04, 1);
  filter: contrast(1.04) brightness(0.98);
}

.post-card__body {
  display: flex;
  flex: 1;
  flex-direction: column;
  padding: 22px 24px 24px;
}

.post-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 10px;
  font-size: 13px;
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
  font-size: 12px;
  font-weight: 500;
}
.post-flag {
  color: var(--color-accent-active);
  font-weight: 500;
}

.post-card h2 {
  margin: 12px 0 10px;
  font-family: var(--font-sans);
  font-size: 21px;
  font-weight: 500;
  line-height: 1.3;
  letter-spacing: -0.022em;
  color: var(--color-ink);
  word-break: normal;
  overflow-wrap: anywhere;
}
.post-card__summary {
  margin: 0 0 18px;
  color: var(--color-text-secondary);
  font-size: 14.5px;
  line-height: 1.68;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.post-card__foot {
  margin-top: auto;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding-top: 12px;
  border-top: 1px solid rgba(20, 20, 19, 0.06);
}
.post-card__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 12.5px;
  color: var(--color-muted);
}
.read-more {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 13.5px;
  font-weight: 500;
  color: var(--color-ink);
  margin-left: auto;
}
.read-more__arrow {
  transition: transform 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}
.post-card:hover .read-more__arrow {
  transform: translateX(4px);
}

/* 5 种 DeepMind 风格抽象几何渐变封面 */
.cover-art {
  position: relative;
  width: 100%;
  height: 100%;
  display: flex;
  align-items: flex-end;
  padding: 18px 20px;
  overflow: hidden;
  transition: transform 0.45s cubic-bezier(0.16, 1, 0.3, 1);
}
.post-card:hover .cover-art {
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
  font-size: 12.5px;
  font-weight: 500;
  padding: 5px 11px;
  border-radius: 9999px;
  background: rgba(255, 255, 255, 0.14);
  -webkit-backdrop-filter: blur(8px);
  backdrop-filter: blur(8px);
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

/* 骨架与空/错误态 */
.post-card--skel {
  pointer-events: none;
}
.skel {
  border-radius: 10px;
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
.skel--cover {
  aspect-ratio: 16 / 10;
  border-radius: 0;
}
.skel--meta {
  height: 16px;
  width: 45%;
  margin-bottom: 12px;
}
.skel--title {
  height: 24px;
  width: 86%;
  margin-bottom: 10px;
}
.skel--line {
  height: 16px;
  width: 72%;
}

.state-panel {
  margin-top: 20px;
  padding: 56px 32px;
  border-radius: var(--radius-card);
  border: 1px solid rgba(20, 20, 19, 0.05);
  background: var(--color-card);
  text-align: center;
}
.state-panel h2 {
  margin: 0 0 10px;
  font-family: var(--font-sans);
  font-size: 26px;
  font-weight: 500;
  letter-spacing: -0.024em;
}
.state-panel p {
  margin: 0 auto 24px;
  max-width: 34em;
  color: var(--color-text-secondary);
  font-size: 15.5px;
  line-height: 1.7;
}
.state-panel__actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 12px;
}

/* 页脚 */
.dm-footer {
  padding: 28px 0 40px;
  border-top: 1px solid rgba(20, 20, 19, 0.08);
}
.dm-footer__row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.dm-brand--foot strong {
  font-size: 17px;
}
.dm-footer__links {
  display: flex;
  gap: 20px;
  font-size: 14px;
  color: var(--color-text-secondary);
}
.dm-footer__links a:hover {
  color: var(--color-ink);
}

@media (prefers-reduced-motion: reduce) {
  .skel {
    animation: none;
  }
  .post-card,
  .post-card__cover img,
  .cover-art,
  .read-more__arrow,
  .pill-btn {
    transition: none;
  }
  .post-card:hover,
  .post-card:hover .post-card__cover img,
  .post-card:hover .cover-art,
  .post-card:hover .read-more__arrow {
    transform: none;
    filter: none;
  }
}

@media (max-width: 1024px) {
  .wrap,
  .dm-header__inner {
    padding-left: 36px;
    padding-right: 36px;
  }
  .posts-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .post-card--featured {
    grid-template-columns: minmax(0, 1fr);
  }
  .post-card--featured .post-card__cover {
    min-height: 0;
    aspect-ratio: 16 / 10;
  }
  .post-card--featured .post-card__body {
    padding: 24px;
  }
}

@media (max-width: 900px) {
  .dm-menu-btn {
    display: inline-flex;
  }
  .dm-nav {
    display: none;
  }
  .posts-main {
    padding-top: 28px;
    padding-bottom: 56px;
  }
  .search-bar {
    width: 100%;
  }
}

@media (max-width: 768px) {
  .wrap,
  .dm-header__inner {
    padding-left: 20px;
    padding-right: 20px;
  }
}

@media (max-width: 640px) {
  .posts-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 20px;
  }
}
</style>
