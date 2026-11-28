<script setup lang="ts">
// 管理后台 → 文章管理：撰写、筛选、发布与管理对外公开的布谷文章与动态。
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ApiError } from '../api'
import { dateTime } from '../format'
import { renderMarkdown } from '../markdown'
import { toast } from '../toast'
import Modal from './Modal.vue'

type PostStatus = 'draft' | 'published' | 'archived'
type CoverTheme = 'obsidian' | 'aurora' | 'cobalt' | 'sage' | 'sand'
type EditorMode = 'edit' | 'split' | 'preview'

interface CmsPost {
  id: number
  slug: string
  category: string
  tags: string[]
  title: string
  summary: string
  content: string
  author: string
  cover_url: string
  cover_theme: string
  status: PostStatus
  featured: boolean
  pinned: boolean
  sort_order: number
  published_at: string | null
  created_at: string
  updated_at: string
}

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

const PRESET_CATEGORIES = ['动态', '产品', '技术', '指南', '更新日志']
const COVER_THEMES: { value: CoverTheme; label: string; swatch: string }[] = [
  { value: 'obsidian', label: '曜石深空', swatch: 'linear-gradient(135deg, #171923, #334155)' },
  { value: 'aurora', label: '极光青蓝', swatch: 'linear-gradient(135deg, #0f766e, #38bdf8)' },
  { value: 'cobalt', label: '钴蓝深海', swatch: 'linear-gradient(135deg, #1d4ed8, #60a5fa)' },
  { value: 'sage', label: '鼠尾草绿', swatch: 'linear-gradient(135deg, #4c6a3d, #8fa67f)' },
  { value: 'sand', label: '暖砂纸色', swatch: 'linear-gradient(135deg, #c2a989, #e8dfd1)' },
]
const STATUS_LABELS: Record<PostStatus, string> = {
  published: '已发布',
  draft: '草稿',
  archived: '已归档',
}
const SLUG_RE = /^[a-z0-9-]+$/

const posts = ref<CmsPost[] | null>(null)
const knownCategories = ref<string[]>([...PRESET_CATEGORIES])
const error = ref('')
const loading = ref(false)
const seeding = ref(false)
const togglingId = ref<number | null>(null)

const filter = reactive({
  status: '',
  category: '',
  q: '',
})

const editing = ref<CmsPost | null>(null)
const isNew = ref(false)
const saving = ref(false)
const formError = ref('')
const editorMode = ref<EditorMode>('split')
const removing = ref<CmsPost | null>(null)
const deleting = ref(false)

const form = reactive({
  title: '',
  slug: '',
  category: '动态',
  author: '布谷团队',
  status: 'draft' as PostStatus,
  sort_order: 0 as number | string,
  featured: false,
  pinned: false,
  cover_url: '',
  cover_theme: 'obsidian' as CoverTheme,
  tagsText: '',
  summary: '',
  content: '',
  published_at_local: '',
})

const hasFilter = computed(() => Boolean(filter.status || filter.category || filter.q.trim()))
const renderedContent = computed(() => renderMarkdown(form.content || '（暂无正文内容）'))
const categoryOptions = computed(() => {
  const set = new Set<string>(knownCategories.value)
  if (filter.category) set.add(filter.category)
  return [...set]
})

function mergeCategories(list: CmsPost[]) {
  const set = new Set<string>(knownCategories.value)
  for (const p of list) {
    if (p.category?.trim()) set.add(p.category.trim())
  }
  knownCategories.value = [...set]
}

function toLocalInput(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function fromLocalInput(local: string): string | null {
  const trimmed = local.trim()
  if (!trimmed) return null
  const d = new Date(trimmed)
  if (Number.isNaN(d.getTime())) return null
  return d.toISOString()
}

function normalizeTheme(theme: string): CoverTheme {
  return COVER_THEMES.some((t) => t.value === theme) ? (theme as CoverTheme) : 'obsidian'
}

function parseTags(raw: string): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const part of raw.split(/[,，]/)) {
    const tag = part.trim()
    if (!tag || seen.has(tag)) continue
    seen.add(tag)
    out.push(tag)
  }
  return out
}

function isSafeCoverUrl(url: string): boolean {
  const u = url.trim()
  if (!u) return true
  if (u.startsWith('/') && !u.startsWith('//')) return true
  return /^https?:\/\//i.test(u)
}

function generateSlug() {
  const ascii = form.title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 80)
  if (ascii && SLUG_RE.test(ascii)) {
    form.slug = ascii
    return
  }
  const now = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  const stamp = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}`
  const rand = Math.random().toString(36).slice(2, 6).replace(/[^a-z0-9]/g, 'a')
  form.slug = `post-${stamp}-${rand}`
}

async function load() {
  loading.value = true
  try {
    const qs = new URLSearchParams()
    if (filter.status) qs.set('status', filter.status)
    if (filter.category) qs.set('category', filter.category)
    if (filter.q.trim()) qs.set('q', filter.q.trim())
    const suffix = qs.toString() ? `?${qs.toString()}` : ''
    const resp = await props.call<{ posts: CmsPost[] }>('GET', `/cms/posts${suffix}`)
    const list = resp.posts ?? []
    posts.value = list
    mergeCategories(list)
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载文章列表失败'
  } finally {
    loading.value = false
  }
}

let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(
  () => [filter.status, filter.category],
  () => {
    clearTimeout(searchTimer)
    load()
  },
)
watch(
  () => filter.q,
  () => {
    clearTimeout(searchTimer)
    searchTimer = setTimeout(() => {
      load()
    }, 260)
  },
)

onMounted(load)
onBeforeUnmount(() => clearTimeout(searchTimer))

function clearFilters() {
  filter.status = ''
  filter.category = ''
  filter.q = ''
}

function openCreate() {
  isNew.value = true
  editing.value = {
    id: 0,
    slug: '',
    category: '动态',
    tags: [],
    title: '',
    summary: '',
    content: '',
    author: '布谷团队',
    cover_url: '',
    cover_theme: 'obsidian',
    status: 'draft',
    featured: false,
    pinned: false,
    sort_order: 0,
    published_at: null,
    created_at: '',
    updated_at: '',
  }
  Object.assign(form, {
    title: '',
    slug: '',
    category: '动态',
    author: '布谷团队',
    status: 'draft' as PostStatus,
    sort_order: 0,
    featured: false,
    pinned: false,
    cover_url: '',
    cover_theme: 'obsidian' as CoverTheme,
    tagsText: '',
    summary: '',
    content: '',
    published_at_local: '',
  })
  formError.value = ''
  editorMode.value = window.innerWidth <= 767 ? 'edit' : 'split'
}

function openEdit(p: CmsPost) {
  isNew.value = false
  editing.value = p
  Object.assign(form, {
    title: p.title,
    slug: p.slug,
    category: p.category || '',
    author: p.author || '',
    status: p.status,
    sort_order: p.sort_order ?? 0,
    featured: !!p.featured,
    pinned: !!p.pinned,
    cover_url: p.cover_url || '',
    cover_theme: normalizeTheme(p.cover_theme),
    tagsText: (p.tags ?? []).join(', '),
    summary: p.summary || '',
    content: p.content || '',
    published_at_local: toLocalInput(p.published_at),
  })
  formError.value = ''
  editorMode.value = window.innerWidth <= 767 ? 'edit' : 'split'
}

function closeEditor() {
  if (saving.value) return
  editing.value = null
}

async function save() {
  if (saving.value || !editing.value) return
  formError.value = ''

  const title = form.title.trim()
  const slug = form.slug.trim().toLowerCase()
  const coverUrl = form.cover_url.trim()

  if (!title) {
    formError.value = '请填写文章标题'
    return
  }
  if (!slug) {
    formError.value = '请填写文章标识（Slug），或点击右侧自动生成'
    return
  }
  if (!SLUG_RE.test(slug) || slug.length > 120) {
    formError.value = '文章标识只能包含小写字母、数字和短横线，且不超过 120 个字符'
    return
  }
  if (!isSafeCoverUrl(coverUrl)) {
    formError.value = '封面图链接只支持 https://、http:// 或以 / 开头的站内路径'
    return
  }

  const sortOrder = Math.trunc(Number(String(form.sort_order).trim()) || 0)
  const body = {
    slug,
    category: form.category.trim(),
    tags: parseTags(form.tagsText),
    title,
    summary: form.summary.trim(),
    content: form.content,
    author: form.author.trim(),
    cover_url: coverUrl,
    cover_theme: form.cover_theme,
    status: form.status,
    featured: form.featured,
    pinned: form.pinned,
    sort_order: sortOrder,
    published_at: fromLocalInput(form.published_at_local),
  }

  saving.value = true
  try {
    if (isNew.value) {
      await props.call<{ post: CmsPost }>('POST', '/cms/posts', body)
      toast(body.status === 'published' ? '文章已创建并发布' : '文章已保存为草稿')
    } else {
      await props.call<{ post: CmsPost }>('PUT', `/cms/posts/${editing.value.id}`, body)
      toast('文章修改已保存')
    }
    editing.value = null
    await load()
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function togglePublish(p: CmsPost) {
  if (togglingId.value !== null) return
  const nextStatus: PostStatus = p.status === 'published' ? 'draft' : 'published'
  togglingId.value = p.id
  try {
    await props.call<{ post: CmsPost }>('PUT', `/cms/posts/${p.id}`, {
      slug: p.slug,
      category: p.category,
      tags: p.tags ?? [],
      title: p.title,
      summary: p.summary,
      content: p.content,
      author: p.author,
      cover_url: p.cover_url,
      cover_theme: normalizeTheme(p.cover_theme),
      status: nextStatus,
      featured: p.featured,
      pinned: p.pinned,
      sort_order: Number(String(p.sort_order ?? 0)) || 0,
      published_at: p.published_at,
    })
    toast(nextStatus === 'published' ? `已发布「${p.title}」` : `已将「${p.title}」撤回为草稿`)
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '状态切换失败')
  } finally {
    togglingId.value = null
  }
}

async function confirmRemove() {
  const p = removing.value
  if (!p || deleting.value) return
  deleting.value = true
  try {
    await props.call('DELETE', `/cms/posts/${p.id}`)
    removing.value = null
    toast(`已删除「${p.title}」`)
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '删除失败')
  } finally {
    deleting.value = false
  }
}

async function seedPosts() {
  if (seeding.value) return
  seeding.value = true
  try {
    const resp = await props.call<{ created: number }>('POST', '/cms/seed')
    const count = Number(resp?.created) || 0
    if (count > 0) {
      toast(`已载入 ${count} 篇示例文章`)
      await load()
    } else {
      toast('示例文章已经都在列表里了')
    }
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '载入示例文章失败')
  } finally {
    seeding.value = false
  }
}
</script>

<template>
  <section class="panel panel--inset">
    <div class="head">
      <h2>文章管理</h2>
      <div class="head__btns">
        <a class="btn btn-sm btn-outline" href="/welcome" target="_blank" rel="noopener noreferrer">查看首页</a>
        <a class="btn btn-sm btn-outline" href="/posts" target="_blank" rel="noopener noreferrer">查看文章页</a>
        <button type="button" class="btn btn-sm" :disabled="seeding" @click="seedPosts">
          {{ seeding ? '正在载入' : '载入示例文章' }}
        </button>
        <button type="button" class="btn btn-sm btn-primary" @click="openCreate">新建文章</button>
      </div>
    </div>

    <p class="muted intro">
      在这里撰写和管理对外公开的文章与动态。状态设为「已发布」后会展示在文章页，勾选「首页精选」的文章会优先展示在首页。
    </p>

    <div class="filters">
      <select v-model="filter.status" class="text-input filter-select" aria-label="按状态筛选">
        <option value="">全部状态</option>
        <option value="published">已发布</option>
        <option value="draft">草稿</option>
        <option value="archived">已归档</option>
      </select>
      <select v-model="filter.category" class="text-input filter-select" aria-label="按分类筛选">
        <option value="">全部分类</option>
        <option v-for="c in categoryOptions" :key="c" :value="c">{{ c }}</option>
      </select>
      <input
        v-model="filter.q"
        class="text-input filter-search"
        type="search"
        placeholder="搜索标题、摘要或标识"
        aria-label="搜索文章"
      />
      <button v-if="hasFilter" type="button" class="btn btn-sm btn-ghost" @click="clearFilters">清空筛选</button>
    </div>

    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <div v-else-if="posts === null" class="skeleton skel" aria-hidden="true" />
    <p v-else-if="!posts.length" class="caption empty">
      {{ hasFilter ? '没有找到符合当前筛选条件的文章。' : '还没有文章，可以点击「新建文章」或「载入示例文章」开始体验。' }}
    </p>

    <div v-else class="post-list" :class="{ 'is-loading': loading }">
      <article v-for="p in posts" :key="p.id" class="post-row">
        <div class="post-row__main">
          <div class="post-row__title-line">
            <p class="post-row__title">{{ p.title }}</p>
            <span class="status-badge" :class="`status-badge--${p.status}`">{{ STATUS_LABELS[p.status] || p.status }}</span>
            <span v-if="p.pinned" class="chip-note chip-compact">置顶</span>
            <span v-if="p.featured" class="badge-pill badge-featured">首页精选</span>
          </div>

          <p class="caption post-row__meta">
            <span>{{ p.category || '未分类' }}</span>
            <span class="sep">·</span>
            <code class="slug-code">/{{ p.slug }}</code>
            <template v-if="p.author">
              <span class="sep">·</span>
              <span>{{ p.author }}</span>
            </template>
            <template v-if="p.sort_order">
              <span class="sep">·</span>
              <span>排序 {{ p.sort_order }}</span>
            </template>
            <span class="sep">·</span>
            <span>更新于 {{ dateTime(p.updated_at) }}</span>
          </p>

          <p v-if="p.summary" class="caption post-row__summary">{{ p.summary }}</p>

          <div v-if="p.tags?.length" class="post-row__tags">
            <span v-for="t in p.tags" :key="t" class="badge-pill">{{ t }}</span>
          </div>
        </div>

        <div class="post-row__actions">
          <button type="button" class="btn btn-sm" @click="openEdit(p)">编辑</button>
          <button
            type="button"
            class="btn btn-sm"
            :disabled="togglingId === p.id"
            @click="togglePublish(p)"
          >
            {{ togglingId === p.id ? '处理中' : p.status === 'published' ? '撤回草稿' : '发布' }}
          </button>
          <a
            v-if="p.status === 'published'"
            class="btn btn-sm btn-outline"
            :href="`/posts/${encodeURIComponent(p.slug)}`"
            target="_blank"
            rel="noopener noreferrer"
          >
            查看前台
          </a>
          <button type="button" class="btn btn-sm btn-danger-lite" @click="removing = p">删除</button>
        </div>
      </article>
    </div>

    <Modal
      :open="!!editing"
      :title="isNew ? '新建文章' : '编辑文章'"
      subtitle="支持 Markdown 语法和数学公式，可在下方实时分栏预览最终排版。"
      wide
      @close="closeEditor"
    >
      <form v-if="editing" class="editor-form" @submit.prevent="save">
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="cms-title">标题</label>
            <input
              id="cms-title"
              v-model="form.title"
              class="text-input"
              maxlength="160"
              placeholder="例如：布谷记忆系统是如何工作的"
              required
            />
          </div>
          <div class="field">
            <label class="field__label" for="cms-slug">文章标识（Slug）</label>
            <div class="slug-row">
              <input
                id="cms-slug"
                v-model="form.slug"
                class="text-input mono"
                maxlength="120"
                spellcheck="false"
                placeholder="例如：memory-system-intro"
                required
              />
              <button type="button" class="btn btn-sm" @click="generateSlug">自动生成</button>
            </div>
            <p class="field__hint">只能包含小写字母、数字和短横线，用于前台访问路径 /posts/{{ form.slug || 'slug' }}。</p>
          </div>
        </div>

        <div class="form-row">
          <div class="field">
            <label class="field__label" for="cms-category">分类</label>
            <input
              id="cms-category"
              v-model="form.category"
              class="text-input"
              maxlength="48"
              placeholder="可直接输入，或点选下方常用分类"
            />
            <div class="preset-cats" role="group" aria-label="常用分类快捷选项">
              <button
                v-for="cat in PRESET_CATEGORIES"
                :key="cat"
                type="button"
                class="preset-cat"
                :class="{ 'is-active': form.category === cat }"
                @click="form.category = cat"
              >
                {{ cat }}
              </button>
            </div>
          </div>

          <div class="field">
            <label class="field__label" for="cms-author">作者</label>
            <input id="cms-author" v-model="form.author" class="text-input" maxlength="64" placeholder="例如：布谷团队" />
          </div>
        </div>

        <div class="meta-grid">
          <div class="field">
            <label class="field__label" for="cms-status">状态</label>
            <select id="cms-status" v-model="form.status" class="text-input">
              <option value="draft">草稿（仅后台可见）</option>
              <option value="published">已发布（公开可见）</option>
              <option value="archived">已归档（隐藏）</option>
            </select>
          </div>

          <div class="field">
            <label class="field__label" for="cms-sort">排序数字</label>
            <input id="cms-sort" v-model="form.sort_order" class="text-input" type="number" step="1" />
            <p class="field__hint">数字越小越靠前，默认为 0。</p>
          </div>

          <div class="field">
            <label class="field__label" for="cms-published-at">发布时间（选填）</label>
            <input id="cms-published-at" v-model="form.published_at_local" class="text-input" type="datetime-local" />
            <p class="field__hint">留空时，首次发布会自动填入当前时间。</p>
          </div>
        </div>

        <div class="flags-row">
          <label class="check-row">
            <input v-model="form.featured" type="checkbox" />
            <span>首页精选（优先展示在首页文章板块）</span>
          </label>
          <label class="check-row">
            <input v-model="form.pinned" type="checkbox" />
            <span>置顶（始终排在文章列表最前面）</span>
          </label>
        </div>

        <div class="form-row">
          <div class="field">
            <label class="field__label" for="cms-cover-url">封面图链接（选填）</label>
            <input
              id="cms-cover-url"
              v-model="form.cover_url"
              class="text-input"
              maxlength="512"
              placeholder="支持 https:// 或 / 开头的站内路径，留空使用几何封面"
            />
          </div>
          <div class="field">
            <span class="field__label">几何封面主题</span>
            <div class="theme-picker" role="radiogroup" aria-label="几何封面主题">
              <button
                v-for="t in COVER_THEMES"
                :key="t.value"
                type="button"
                class="theme-option"
                :class="{ 'is-active': form.cover_theme === t.value }"
                :aria-checked="form.cover_theme === t.value"
                role="radio"
                @click="form.cover_theme = t.value"
              >
                <span class="theme-swatch" :style="{ background: t.swatch }" aria-hidden="true" />
                <span>{{ t.label }}</span>
              </button>
            </div>
          </div>
        </div>

        <div class="field">
          <label class="field__label" for="cms-tags">标签（逗号分隔）</label>
          <input
            id="cms-tags"
            v-model="form.tagsText"
            class="text-input"
            placeholder="例如：记忆系统, 更新日志, 使用技巧"
          />
        </div>

        <div class="field">
          <label class="field__label" for="cms-summary">摘要</label>
          <textarea
            id="cms-summary"
            v-model="form.summary"
            class="textarea summary-input"
            maxlength="500"
            placeholder="简明概括文章要点，会显示在首页精选和文章列表卡片中"
          />
        </div>

        <div class="field">
          <div class="editor-head">
            <label class="field__label" for="cms-content">正文（Markdown）</label>
            <div class="type-switch editor-switch" role="group" aria-label="编辑视图切换">
              <button type="button" :class="{ 'is-active': editorMode === 'edit' }" @click="editorMode = 'edit'">编辑</button>
              <button type="button" :class="{ 'is-active': editorMode === 'split' }" @click="editorMode = 'split'">分栏</button>
              <button type="button" :class="{ 'is-active': editorMode === 'preview' }" @click="editorMode = 'preview'">预览</button>
            </div>
          </div>

          <div class="editor-body" :class="`editor-body--${editorMode}`">
            <textarea
              v-if="editorMode === 'edit' || editorMode === 'split'"
              id="cms-content"
              v-model="form.content"
              class="textarea md-textarea"
              placeholder="支持标题（#）、列表（- ）、代码块、链接与公式（$...$ 或 $$...$$）"
            />
            <!-- eslint-disable-next-line vue/no-v-html -->
            <div
              v-if="editorMode === 'preview' || editorMode === 'split'"
              class="md md-preview"
              v-html="renderedContent"
            />
          </div>
        </div>

        <p v-if="formError" class="form-flash form-flash--error">{{ formError }}</p>

        <div class="modal-actions">
          <button type="button" class="btn" :disabled="saving" @click="closeEditor">取消</button>
          <button
            type="submit"
            class="btn btn-primary"
            :disabled="saving || !form.title.trim() || !form.slug.trim()"
          >
            {{ saving ? '正在保存' : isNew ? '创建文章' : '保存修改' }}
          </button>
        </div>
      </form>
    </Modal>

    <Modal
      :open="!!removing"
      :title="`删除「${removing?.title}」？`"
      subtitle="删除后无法恢复，前台文章页也不再展示这篇文章。"
      @close="removing = null"
    >
      <div class="modal-actions">
        <button type="button" class="btn" :disabled="deleting" @click="removing = null">取消</button>
        <button type="button" class="btn btn-danger" :disabled="deleting" @click="confirmRemove">
          {{ deleting ? '正在删除' : '确认删除' }}
        </button>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
.panel {
  border-radius: 24px;
  border: 1px solid rgba(20, 20, 19, .05);
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px 16px;
  flex-wrap: wrap;
}
.head h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 500;
  letter-spacing: -.02em;
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
.filters {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}
.filter-select {
  width: auto;
  min-width: 132px;
  min-height: 38px;
  padding: 6px 12px;
  border-radius: 12px;
}
.filter-search {
  flex: 1 1 220px;
  min-height: 38px;
  padding: 6px 14px;
  border-radius: 9999px;
}
.skel {
  height: 76px;
  border-radius: 16px;
}
.empty {
  margin: 12px 0 4px;
}
.post-list {
  display: grid;
  gap: 2px;
}
.post-list.is-loading {
  opacity: .65;
  transition: opacity .15s ease;
}
.post-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 16px 12px;
  border-top: 1px solid rgba(20, 20, 19, .08);
  border-radius: 14px;
  transition: background-color var(--motion-fast) var(--ease-editorial);
}
.post-row:first-child {
  border-top-color: rgba(20, 20, 19, .08);
}
.post-row:hover {
  background: rgba(20, 20, 19, .028);
}
.post-row__main {
  flex: 1;
  min-width: 0;
}
.post-row__title-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.post-row__title {
  margin: 0;
  color: var(--color-ink);
  font-size: 15px;
  font-weight: 500;
  letter-spacing: -.01em;
  line-height: 1.45;
  overflow-wrap: anywhere;
}
.status-badge {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 9px;
  border-radius: 9999px;
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
}
.status-badge--published {
  background: var(--color-success-tint);
  color: var(--color-success);
}
.status-badge--draft {
  background: var(--color-canvas-soft);
  color: var(--color-text-secondary);
}
.status-badge--archived {
  background: var(--color-divider);
  color: var(--color-muted);
}
.chip-compact {
  height: 22px;
  padding: 0 8px;
  font-size: 12px;
}
.badge-featured {
  min-height: 22px;
  border-color: var(--color-accent);
  color: var(--color-accent-active);
}
.post-row__meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 5px;
  margin: 5px 0 0;
}
.sep {
  color: var(--color-tertiary);
}
.slug-code {
  padding: 1px 7px;
  border-radius: 9999px;
  background: var(--color-canvas-soft);
  color: var(--color-text-secondary);
  overflow-wrap: anywhere;
}
.post-row__summary {
  margin: 6px 0 0;
  max-width: 78ch;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.post-row__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}
.post-row__actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  flex-shrink: 0;
}

.slug-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.slug-row .text-input {
  flex: 1;
}
.mono {
  font-family: var(--font-mono);
  font-size: 13px;
}
.preset-cats {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 2px;
}
.preset-cat {
  min-height: 28px;
  padding: 0 12px;
  border: 1px solid rgba(20, 20, 19, .12);
  border-radius: 9999px;
  background: var(--color-surface);
  color: var(--color-text-secondary);
  font-size: 12px;
  transition: background-color var(--motion-fast) var(--ease-editorial), color var(--motion-fast) var(--ease-editorial), border-color var(--motion-fast) var(--ease-editorial);
}
.preset-cat:hover {
  border-color: rgba(20, 20, 19, .26);
  color: var(--color-ink);
}
.preset-cat.is-active {
  border-color: var(--color-ink);
  background: var(--color-ink);
  color: #fff;
}
.meta-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  align-items: start;
}
.flags-row {
  display: flex;
  flex-wrap: wrap;
  gap: 12px 24px;
  margin-bottom: 14px;
}
.theme-picker {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.theme-option {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 34px;
  padding: 0 12px;
  border: 1px solid rgba(20, 20, 19, .12);
  border-radius: 9999px;
  background: var(--color-surface);
  color: var(--color-text-secondary);
  font-size: 13px;
  transition: border-color var(--motion-fast) var(--ease-editorial), color var(--motion-fast) var(--ease-editorial), box-shadow var(--motion-fast) var(--ease-editorial);
}
.theme-option:hover {
  border-color: rgba(20, 20, 19, .26);
  color: var(--color-ink);
}
.theme-option.is-active {
  border-color: var(--color-ink);
  color: var(--color-ink);
  font-weight: 500;
  box-shadow: 0 0 0 1px var(--color-ink);
}
.theme-swatch {
  width: 14px;
  height: 14px;
  border-radius: 50%;
  flex: 0 0 14px;
}
.summary-input {
  min-height: 76px;
}
.editor-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.editor-switch {
  min-width: 196px;
}
.editor-body {
  display: grid;
  gap: 16px;
}
.editor-body--split {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  align-items: stretch;
}
.md-textarea {
  min-height: 400px;
  border-radius: 14px;
  font-family: var(--font-mono);
  font-size: 13px;
  line-height: 1.68;
  tab-size: 2;
}
.md-preview {
  min-height: 400px;
  max-height: 540px;
  overflow-y: auto;
  padding: 16px 18px;
  border: 1px solid rgba(20, 20, 19, .09);
  border-radius: 14px;
  background: var(--color-surface);
  font-size: 14px;
  line-height: 1.75;
  overflow-wrap: anywhere;
}
.md-preview :deep(h1),
.md-preview :deep(h2),
.md-preview :deep(h3) {
  margin: 1em 0 .45em;
  line-height: 1.35;
}
.md-preview :deep(h1:first-child),
.md-preview :deep(h2:first-child),
.md-preview :deep(h3:first-child) {
  margin-top: 0;
}
.md-preview :deep(blockquote) {
  margin: .6em 0;
  padding: 2px 12px;
  border-left: 3px solid var(--color-hairline-strong);
  color: var(--color-text-secondary);
}
.md-preview :deep(img) {
  max-width: 100%;
  border-radius: 12px;
}

@media (max-width: 767px) {
  .panel {
    border-radius: 20px;
  }
  .filter-select {
    flex: 1 1 calc(50% - 5px);
  }
  .filter-search {
    flex: 1 1 100%;
  }
  .post-row {
    flex-direction: column;
    gap: 10px;
    padding: 14px 4px;
  }
  .post-row__actions {
    width: 100%;
  }
  .meta-grid {
    grid-template-columns: 1fr;
  }
  .editor-body--split {
    grid-template-columns: 1fr;
  }
  .md-textarea,
  .md-preview {
    min-height: 240px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .post-list.is-loading,
  .post-row,
  .preset-cat,
  .theme-option {
    transition: none;
  }
}
</style>
