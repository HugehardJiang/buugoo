<script setup lang="ts">
// 公开文章详情页：延续 DeepMind 渐进模糊顶栏与大留白阅读体验，Markdown 经 DOMPurify 清理并按需渲染 KaTeX 公式。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import Icon from '../Icon.vue'
import { ApiError, api, type CmsPostDetail, type CmsPostSummary } from '../api'
import { renderMarkdown } from '../markdown'
import { setPageSeo, toAbsoluteUrl } from '../seo'
import { needsLogin, session } from '../session'

const route = useRoute()

const name = computed(() => session.meta?.name ?? '布谷')
const isLoggedIn = computed(() => !needsLogin())
const canRegister = computed(() => (session.meta?.registration ?? 'closed') !== 'closed')
const homeTo = computed(() => (isLoggedIn.value ? '/' : '/welcome'))
const startTo = computed(() =>
  isLoggedIn.value ? { path: '/' } : canRegister.value ? { path: '/login', query: { tab: 'register' } } : { path: '/login' },
)

const root = ref<HTMLElement | null>(null)
const post = ref<CmsPostDetail | null>(null)
const related = ref<CmsPostSummary[]>([])
const loading = ref(true)
const notFound = ref(false)
const loadError = ref('')
const failedCovers = ref<Record<string, boolean>>({})
const mobileMenuOpen = ref(false)

// renderMarkdown 内部自动处理 DOMPurify 清理与 KaTeX 按需懒加载（通过 mathReady 响应式重渲）
const renderedHtml = computed(() => (post.value?.content ? renderMarkdown(post.value.content) : ''))

const readingMinutes = computed(() => {
  const text = (post.value?.content ?? '').replace(/[#*_`>\-\s]/g, '')
  if (!text) return 1
  return Math.max(1, Math.round(text.length / 360))
})

function safeCoverUrl(raw?: string): string {
  const u = (raw ?? '').trim()
  if (!u) return ''
  if (u.startsWith('/') && !u.startsWith('//')) return u
  if (/^https?:\/\//i.test(u)) return u
  return ''
}

function resolveCoverSrc(item: CmsPostSummary): string {
  const url = safeCoverUrl(item.cover_url)
  if (!url || failedCovers.value[item.slug]) return ''
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

let reqSeq = 0
async function fetchPost(slug: string) {
  const seq = ++reqSeq
  loading.value = true
  notFound.value = false
  loadError.value = ''
  post.value = null
  related.value = []
  try {
    const res = await api.cmsPost(slug)
    if (seq !== reqSeq) return
    post.value = res.post
    related.value = Array.isArray(res.related) ? res.related : []
    root.value?.scrollTo({ top: 0 })
  } catch (e) {
    if (seq !== reqSeq) return
    if (e instanceof ApiError && e.status === 404) {
      notFound.value = true
    } else {
      loadError.value = e instanceof Error ? e.message : '加载文章失败，请稍后重试。'
    }
  } finally {
    if (seq === reqSeq) loading.value = false
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && mobileMenuOpen.value) mobileMenuOpen.value = false
}

watch(
  () => String(route.params.slug ?? ''),
  (slug) => {
    mobileMenuOpen.value = false
    if (!slug) {
      notFound.value = true
      loading.value = false
      return
    }
    fetchPost(slug)
  },
  { immediate: true },
)

function plainSnippet(markdownText: string): string {
  return markdownText
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`[^`]*`/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/[#>*_~\-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 160)
}

watch(
  [name, post, notFound, loadError],
  () => {
    const siteName = name.value
    if (notFound.value) {
      setPageSeo({
        title: `文章不存在 - ${siteName}`,
        description: '这篇内容可能已经下线，或者链接地址有误。',
        robots: 'noindex, nofollow',
        siteName,
        jsonLd: null,
      })
      return
    }
    const p = post.value
    if (!p) return
    const desc = p.summary?.trim() || plainSnippet(p.content)
    const canonicalPath = `/posts/${encodeURIComponent(p.slug)}`
    const postUrl = toAbsoluteUrl(canonicalPath)
    const homeUrl = toAbsoluteUrl('/welcome')
    const postsUrl = toAbsoluteUrl('/posts')
    const coverImg = safeCoverUrl(p.cover_url) || '/icon-512.png'
    const absImg = toAbsoluteUrl(coverImg)
    setPageSeo({
      title: `${p.title} - ${siteName}`,
      description: desc,
      robots: 'index, follow',
      canonicalPath,
      ogType: 'article',
      siteName,
      image: coverImg,
      publishedTime: p.published_at || p.created_at,
      modifiedTime: p.updated_at,
      section: p.category,
      tags: p.tags,
      jsonLd: [
        {
          '@context': 'https://schema.org',
          '@type': 'BlogPosting',
          headline: p.title,
          description: desc,
          image: [absImg],
          datePublished: p.published_at || p.created_at,
          dateModified: p.updated_at || p.published_at || p.created_at,
          author: p.author
            ? { '@type': 'Person', name: p.author }
            : { '@type': 'Organization', name: siteName },
          publisher: {
            '@type': 'Organization',
            name: siteName,
            logo: { '@type': 'ImageObject', url: toAbsoluteUrl('/icon-512.png') },
          },
          mainEntityOfPage: { '@type': 'WebPage', '@id': postUrl },
          articleSection: p.category || undefined,
          keywords: p.tags?.length ? p.tags.join(', ') : undefined,
          inLanguage: 'zh-CN',
        },
        {
          '@context': 'https://schema.org',
          '@type': 'BreadcrumbList',
          itemListElement: [
            { '@type': 'ListItem', position: 1, name: '首页', item: homeUrl },
            { '@type': 'ListItem', position: 2, name: '文章与动态', item: postsUrl },
            { '@type': 'ListItem', position: 3, name: p.title, item: postUrl },
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
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div ref="root" class="site">
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

    <main class="wrap detail-main">
      <div class="back-row">
        <RouterLink to="/posts" class="back-pill">
          <Icon name="chevron" :size="14" class="back-pill__ico" />
          <span>返回文章列表</span>
        </RouterLink>
      </div>

      <!-- 加载骨架 -->
      <article v-if="loading" class="article-shell" aria-busy="true">
        <div class="article-head">
          <div class="skel skel--meta" />
          <div class="skel skel--title" />
          <div class="skel skel--summary" />
        </div>
        <div class="skel skel--cover" />
        <div class="article-body-skel">
          <div class="skel skel--line" />
          <div class="skel skel--line" />
          <div class="skel skel--short" />
        </div>
      </article>

      <!-- 404 状态 -->
      <section v-else-if="notFound" class="state-panel">
        <h1>文章不存在或尚未发布</h1>
        <p>这篇内容可能已经下线，或者链接地址有误。</p>
        <div class="state-panel__actions">
          <RouterLink to="/posts" class="pill-btn pill-btn--ink">浏览全部文章</RouterLink>
          <RouterLink :to="homeTo" class="pill-btn pill-btn--outline">返回首页</RouterLink>
        </div>
      </section>

      <!-- 加载失败 -->
      <section v-else-if="loadError" class="state-panel">
        <h1>暂时无法打开文章</h1>
        <p>{{ loadError }}</p>
        <div class="state-panel__actions">
          <button type="button" class="pill-btn pill-btn--ink" @click="fetchPost(String(route.params.slug ?? ''))">重试</button>
          <RouterLink to="/posts" class="pill-btn pill-btn--outline">返回文章列表</RouterLink>
        </div>
      </section>

      <!-- 文章正文 -->
      <template v-else-if="post">
        <article class="article-shell">
          <header class="article-head">
            <div class="post-meta">
              <RouterLink
                v-if="post.category"
                :to="{ path: '/posts', query: { category: post.category } }"
                class="post-cat"
              >
                {{ post.category }}
              </RouterLink>
              <time v-if="post.published_at || post.created_at" :datetime="post.published_at || post.created_at">
                {{ formatDate(post.published_at || post.created_at) }}
              </time>
              <span v-if="post.author">作者 · {{ post.author }}</span>
              <span>预计阅读 {{ readingMinutes }} 分钟</span>
            </div>

            <h1>{{ post.title }}</h1>
            <p v-if="post.summary" class="article-summary">{{ post.summary }}</p>

            <div v-if="post.tags && post.tags.length" class="article-tags">
              <RouterLink
                v-for="tag in post.tags"
                :key="tag"
                :to="{ path: '/posts', query: { tag } }"
                class="tag-chip"
              >
                #{{ tag }}
              </RouterLink>
            </div>
          </header>

          <figure class="article-cover">
            <img
              v-if="resolveCoverSrc(post)"
              :src="resolveCoverSrc(post)"
              :alt="post.title"
              @error="onCoverError(post.slug)"
            />
            <div v-else class="cover-art" :class="coverThemeClass(post.cover_theme, 0)" aria-hidden="true">
              <svg class="cover-art__geo" viewBox="0 0 960 540" fill="none">
                <circle cx="760" cy="130" r="210" stroke="currentColor" stroke-opacity="0.22" stroke-width="1.3" />
                <circle cx="760" cy="130" r="126" stroke="currentColor" stroke-opacity="0.3" stroke-width="1.3" />
                <path d="M56 440 C 260 300, 480 390, 900 176" stroke="currentColor" stroke-opacity="0.25" stroke-width="1.5" />
                <circle cx="272" cy="364" r="6" fill="currentColor" fill-opacity="0.55" />
              </svg>
              <span class="cover-art__label">{{ post.category || name }}</span>
            </div>
          </figure>

          <!-- Markdown 与 KaTeX 正文容器 -->
          <div class="article-prose md" v-html="renderedHtml" />
        </article>

        <!-- 相关阅读 -->
        <section v-if="related.length > 0" class="related-block">
          <div class="related-head">
            <h2>相关阅读</h2>
            <RouterLink to="/posts" class="pill-btn pill-btn--outline pill-btn--sm">查看全部</RouterLink>
          </div>
          <div class="related-grid">
            <article v-for="(item, idx) in related" :key="item.id" class="related-card-wrap">
              <RouterLink :to="`/posts/${item.slug}`" class="related-card">
                <div class="related-card__cover">
                  <img
                    v-if="resolveCoverSrc(item)"
                    :src="resolveCoverSrc(item)"
                    :alt="item.title"
                    loading="lazy"
                    @error="onCoverError(item.slug)"
                  />
                  <div v-else class="cover-art cover-art--compact" :class="coverThemeClass(item.cover_theme, idx + 1)" aria-hidden="true">
                    <svg class="cover-art__geo" viewBox="0 0 360 220" fill="none">
                      <circle cx="280" cy="58" r="84" stroke="currentColor" stroke-opacity="0.26" stroke-width="1.2" />
                      <path d="M20 182 C 120 120, 210 156, 340 62" stroke="currentColor" stroke-opacity="0.28" stroke-width="1.2" />
                    </svg>
                  </div>
                </div>
                <div class="related-card__body">
                  <div class="post-meta">
                    <span v-if="item.category" class="post-cat">{{ item.category }}</span>
                    <time v-if="item.published_at || item.created_at" :datetime="item.published_at || item.created_at">
                      {{ formatDate(item.published_at || item.created_at) }}
                    </time>
                  </div>
                  <h3>{{ item.title }}</h3>
                  <span class="read-more">
                    <span>阅读全文</span>
                    <Icon name="chevron" :size="14" class="read-more__arrow" />
                  </span>
                </div>
              </RouterLink>
            </article>
          </div>
        </section>
      </template>
    </main>

    <footer class="dm-footer">
      <div class="wrap dm-footer__row">
        <RouterLink :to="homeTo" class="dm-brand dm-brand--foot">
          <img src="/logo.png" :alt="`${name}标志`" width="22" height="22" />
          <strong>{{ name }}</strong>
        </RouterLink>
        <div class="dm-footer__links">
          <RouterLink :to="homeTo">首页</RouterLink>
          <RouterLink to="/posts">全部文章</RouterLink>
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

/* ===== 文章主体 ===== */
.detail-main {
  padding-top: 32px;
  padding-bottom: 84px;
}

.back-row {
  max-width: 920px;
  margin: 0 auto 28px;
}
.back-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 36px;
  padding: 0 14px 0 10px;
  border-radius: 9999px;
  color: var(--color-text-secondary);
  font-size: 14px;
  font-weight: 500;
  transition: background-color 0.18s ease, color 0.18s ease;
}
.back-pill:hover {
  background: rgba(20, 20, 19, 0.05);
  color: var(--color-ink);
}
.back-pill__ico {
  transform: rotate(180deg);
}

.article-shell {
  max-width: 920px;
  margin: 0 auto;
}

.article-head {
  max-width: 780px;
  margin: 0 auto 36px;
}

.post-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 14px;
  font-size: 13.5px;
  color: var(--color-muted);
}
.post-cat {
  display: inline-flex;
  align-items: center;
  height: 26px;
  padding: 0 12px;
  border-radius: 9999px;
  background: var(--md-surface-container);
  color: var(--color-ink);
  font-size: 12.5px;
  font-weight: 500;
  text-decoration: none;
  transition: background-color 0.18s var(--md-ease), color 0.18s var(--md-ease);
}
.post-cat:hover {
  background: var(--md-primary-container);
  color: var(--md-on-primary-container);
}

.article-head h1 {
  margin: 18px 0 18px;
  font-family: var(--font-sans);
  font-size: clamp(32px, 4.2vw, 52px);
  font-weight: 500;
  line-height: 1.14;
  letter-spacing: -0.03em;
  word-break: normal;
  overflow-wrap: anywhere;
  text-wrap: wrap;
}

.article-summary {
  margin: 0 0 20px;
  color: var(--color-text-secondary);
  font-size: clamp(17px, 1.5vw, 19.5px);
  line-height: 1.7;
}

.article-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.tag-chip {
  display: inline-flex;
  align-items: center;
  height: 28px;
  padding: 0 12px;
  border-radius: 9999px;
  background: var(--md-surface-container-low);
  color: var(--color-text-secondary);
  font-size: 13px;
  text-decoration: none;
  transition: background-color 0.18s var(--md-ease), color 0.18s var(--md-ease);
}
.tag-chip:hover {
  background: var(--md-surface-container);
  color: var(--color-ink);
}

.article-cover {
  margin: 0 0 44px;
  width: 100%;
  aspect-ratio: 16 / 9;
  border-radius: 24px;
  overflow: hidden;
  background: var(--md-surface-container-low);
}
.article-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

/* 5 种 DeepMind 风格抽象几何渐变封面 */
.cover-art {
  position: relative;
  width: 100%;
  height: 100%;
  display: flex;
  align-items: flex-end;
  padding: 24px 28px;
  overflow: hidden;
}
.cover-art--compact {
  padding: 0;
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
  padding: 6px 13px;
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

/* ===== Markdown 与 KaTeX 编辑排版 ===== */
.article-prose {
  max-width: 760px;
  margin: 0 auto;
  font-size: 16.5px;
  line-height: 1.85;
  color: var(--color-ink);
  overflow-wrap: break-word;
}
.article-prose :deep(h1),
.article-prose :deep(h2),
.article-prose :deep(h3),
.article-prose :deep(h4) {
  font-family: var(--font-sans);
  font-weight: 500;
  color: var(--color-ink);
  letter-spacing: -0.022em;
  line-height: 1.3;
  word-break: normal;
  overflow-wrap: anywhere;
  text-wrap: wrap;
}
.article-prose :deep(h2) {
  margin: 44px 0 16px;
  font-size: clamp(24px, 2.4vw, 30px);
}
.article-prose :deep(h3) {
  margin: 32px 0 12px;
  font-size: clamp(19px, 1.8vw, 22px);
}
.article-prose :deep(p) {
  margin: 0 0 1.25em;
}
.article-prose :deep(ul),
.article-prose :deep(ol) {
  margin: 0 0 1.3em;
  padding-left: 1.5em;
}
.article-prose :deep(li) {
  margin-bottom: 0.45em;
}
.article-prose :deep(blockquote) {
  margin: 24px 0;
  padding: 16px 22px;
  border-left: 3px solid var(--color-accent);
  border-radius: 0 16px 16px 0;
  background: var(--color-card);
  color: var(--color-text-secondary);
}
.article-prose :deep(blockquote p:last-child) {
  margin-bottom: 0;
}
.article-prose :deep(pre) {
  margin: 24px 0;
  padding: 18px 20px;
  border-radius: 16px;
  background: #1c1d21;
  color: #f4f3ee;
  overflow-x: auto;
  font-size: 13.5px;
  line-height: 1.65;
}
.article-prose :deep(pre code) {
  background: none;
  color: inherit;
  padding: 0;
  font-size: inherit;
}
.article-prose :deep(code) {
  padding: 2px 6px;
  border-radius: 6px;
  background: var(--color-card-strong);
  font-size: 0.88em;
}
.article-prose :deep(hr) {
  margin: 36px 0;
  border: 0;
  border-top: 1px solid rgba(20, 20, 19, 0.1);
}
.article-prose :deep(img) {
  max-width: 100%;
  height: auto;
  border-radius: 16px;
  margin: 20px 0;
}
.article-prose :deep(table) {
  display: block;
  width: 100%;
  overflow-x: auto;
  border-collapse: collapse;
  margin: 24px 0;
  font-size: 14.5px;
}
.article-prose :deep(th),
.article-prose :deep(td) {
  padding: 10px 14px;
  border-bottom: 1px solid rgba(20, 20, 19, 0.1);
  text-align: left;
  white-space: nowrap;
}
.article-prose :deep(th) {
  font-weight: 600;
  background: var(--color-card);
}
.article-prose :deep(.math-display) {
  margin: 20px 0;
  padding: 8px 4px;
  overflow-x: auto;
  overflow-y: hidden;
  max-width: 100%;
}

/* ===== 相关阅读 ===== */
.related-block {
  max-width: 1040px;
  margin: 72px auto 0;
  padding-top: 48px;
  border-top: 1px solid rgba(20, 20, 19, 0.08);
}
.related-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 28px;
}
.related-head h2 {
  margin: 0;
  font-family: var(--font-sans);
  font-size: 28px;
  font-weight: 500;
  letter-spacing: -0.024em;
}
.related-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 20px;
}
.related-card-wrap {
  display: flex;
  min-width: 0;
}
.related-card {
  display: flex;
  flex: 1;
  flex-direction: column;
  border: 0;
  border-radius: 24px;
  background: var(--md-surface-container-low);
  overflow: hidden;
  text-decoration: none;
  color: inherit;
  transition: transform 0.25s var(--md-ease), background-color 0.22s var(--md-ease), box-shadow 0.25s var(--md-ease);
}
.related-card:hover {
  transform: translateY(-2px);
  background: var(--md-surface-container-lowest);
  box-shadow: var(--md-elev-2);
}
.related-card__cover {
  aspect-ratio: 16 / 10;
  overflow: hidden;
  background: var(--md-surface-container);
}
.related-card__cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: transform 0.45s cubic-bezier(0.16, 1, 0.3, 1);
}
.related-card:hover .related-card__cover img {
  transform: scale3d(1.04, 1.04, 1);
}
.related-card__body {
  display: flex;
  flex: 1;
  flex-direction: column;
  padding: 18px 20px 20px;
}
.related-card h3 {
  margin: 10px 0 14px;
  font-family: var(--font-sans);
  font-size: 18.5px;
  font-weight: 500;
  letter-spacing: -0.02em;
  line-height: 1.35;
  word-break: normal;
  overflow-wrap: anywhere;
}
.read-more {
  margin-top: auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 13.5px;
  font-weight: 500;
}
.read-more__arrow {
  transition: transform 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}
.related-card:hover .read-more__arrow {
  transform: translateX(4px);
}

/* 骨架与状态 */
.skel {
  border-radius: 12px;
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
.skel--meta {
  height: 20px;
  width: 36%;
  margin-bottom: 18px;
}
.skel--title {
  height: 44px;
  width: 84%;
  margin-bottom: 16px;
}
.skel--summary {
  height: 22px;
  width: 72%;
}
.skel--cover {
  aspect-ratio: 16 / 9;
  border-radius: 24px;
  margin-bottom: 36px;
}
.article-body-skel {
  max-width: 760px;
  margin: 0 auto;
  display: grid;
  gap: 14px;
}
.skel--line {
  height: 18px;
  width: 100%;
}
.skel--short {
  height: 18px;
  width: 68%;
}

.state-panel {
  max-width: 680px;
  margin: 32px auto;
  padding: 56px 32px;
  border: 0;
  border-radius: 24px;
  background: var(--md-surface-container-low);
  text-align: center;
}
.state-panel h1 {
  margin: 0 0 12px;
  font-family: var(--font-sans);
  font-size: 28px;
  font-weight: 500;
  letter-spacing: -0.024em;
}
.state-panel p {
  margin: 0 0 26px;
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
  .related-card,
  .related-card__cover img,
  .read-more__arrow,
  .pill-btn {
    transition: none;
  }
  .related-card:hover,
  .related-card:hover .related-card__cover img,
  .related-card:hover .read-more__arrow {
    transform: none;
  }
}

@media (max-width: 1024px) {
  .wrap,
  .dm-header__inner {
    padding-left: 36px;
    padding-right: 36px;
  }
}

@media (max-width: 900px) {
  .dm-menu-btn {
    display: inline-flex;
  }
  .dm-nav {
    display: none;
  }
  .detail-main {
    padding-top: 20px;
    padding-bottom: 56px;
  }
  .article-cover {
    border-radius: 18px;
    margin-bottom: 32px;
  }
  .article-prose {
    font-size: 16px;
  }
  .related-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 768px) {
  .wrap,
  .dm-header__inner {
    padding-left: 20px;
    padding-right: 20px;
  }
}
</style>
