<script setup lang="ts">
// 文件管理：聊天中上传的图片与文档、布谷运行代码生成的图片。可以多选删除、全选，或一键删除 7 天前的全部文件。
// 空间用满后不能再上传文件，删除一些文件即可恢复。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError, type FileItem, type Usage } from '../api'
import { blobURL, download, forget } from '../blobs'
import Icon from '../Icon.vue'
import Modal from '../components/Modal.vue'
import { dateTime, formatSize } from '../format'
import { openLightbox } from '../lightbox'
import { session } from '../session'
import { toast } from '../toast'

const PAGE = 60
const OLD_DAYS = 7

const router = useRouter()
const name = computed(() => session.meta?.name ?? '布谷')
const kind = ref<'' | 'image' | 'document'>('')
const files = ref<FileItem[]>([])
const total = ref(0)
const usage = ref<Usage | null>(null)
const loading = ref(true)
const loadingMore = ref(false)
const error = ref('')
const selected = ref<Set<number>>(new Set())
const confirm = ref<'selected' | 'old' | null>(null)
const deleting = ref(false)
const thumbs = ref<Record<number, string>>({})

const hasMore = computed(() => files.value.length < total.value)
const allSelected = computed(() => files.value.length > 0 && files.value.every((f) => selected.value.has(f.id)))
const someSelected = computed(() => selected.value.size > 0 && !allSelected.value)
const selectedBytes = computed(() => files.value.reduce((n, f) => (selected.value.has(f.id) ? n + f.size : n), 0))
const percent = computed(() => {
  const u = usage.value
  if (!u || !u.quota_bytes) return 0
  return Math.min(100, (u.used_bytes / u.quota_bytes) * 100)
})
const level = computed(() => (usage.value?.full ? 'full' : percent.value >= 80 ? 'warn' : 'ok'))

function fail(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback
}

// 缩略图进入可视区域时才加载。
let observer: IntersectionObserver | null = null
const pendingThumbs = new Map<Element, number>()
function observeThumb(el: unknown, id: number) {
  if (!(el instanceof Element) || thumbs.value[id]) return
  if (!observer) {
    observer = new IntersectionObserver(
      (entries) => {
        for (const en of entries) {
          if (!en.isIntersecting) continue
          const fid = pendingThumbs.get(en.target)
          observer?.unobserve(en.target)
          pendingThumbs.delete(en.target)
          if (fid) blobURL(fid).then((u) => (thumbs.value = { ...thumbs.value, [fid]: u })).catch(() => {})
        }
      },
      { rootMargin: '200px' },
    )
  }
  if (!pendingThumbs.has(el)) {
    pendingThumbs.set(el, id)
    observer.observe(el)
  }
}
onBeforeUnmount(() => observer?.disconnect())

async function load(reset = true) {
  if (reset) {
    loading.value = true
    error.value = ''
  } else loadingMore.value = true
  try {
    const r = await api.files({ kind: kind.value, limit: PAGE, offset: reset ? 0 : files.value.length })
    files.value = reset ? r.files : [...files.value, ...r.files.filter((f) => !files.value.some((x) => x.id === f.id))]
    total.value = r.total
    usage.value = r.usage
    if (reset) selected.value = new Set()
  } catch (e) {
    error.value = fail(e, '加载失败')
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

function setKind(k: typeof kind.value) {
  if (kind.value === k) return
  kind.value = k
  load()
}

function toggle(id: number) {
  const s = new Set(selected.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  selected.value = s
}

function toggleAll() {
  selected.value = allSelected.value ? new Set() : new Set(files.value.map((f) => f.id))
}

function open(f: FileItem) {
  if (f.kind === 'image') openLightbox(f.id, f.filename)
  else download(f.id, f.filename).catch(() => toast('下载失败'))
}

function goConversation(f: FileItem) {
  if (f.conversation_id) router.push({ path: '/', query: { c: String(f.conversation_id) } })
}

async function doDelete() {
  if (!confirm.value) return
  deleting.value = true
  const which = confirm.value
  const before = usage.value?.used_bytes ?? 0
  try {
    const r =
      which === 'selected'
        ? await api.deleteFiles({ ids: [...selected.value] })
        : await api.deleteFiles({ older_than_days: OLD_DAYS })
    usage.value = r.usage
    confirm.value = null
    const freed = Math.max(0, before - r.usage.used_bytes)
    toast(r.deleted ? `删除了 ${r.deleted} 个文件${freed ? `，腾出 ${formatSize(freed)}` : ''}` : `没有 ${OLD_DAYS} 天前的文件`)
    if (which === 'selected') {
      for (const id of selected.value) forget(id)
      files.value = files.value.filter((f) => !selected.value.has(f.id))
      total.value = Math.max(0, total.value - r.deleted)
      selected.value = new Set()
    } else {
      await load()
    }
  } catch (e) {
    toast(fail(e, '删除失败'))
  } finally {
    deleting.value = false
  }
}

// 今年的文件不显示年份，卡片窄时也放得下。
function shortDate(iso: string) {
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  const md = `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  return d.getFullYear() === new Date().getFullYear() ? md : `${d.getFullYear()}-${md}`
}

function sourceLabel(f: FileItem) {
  return f.source === 'generated' ? '代码生成' : '上传'
}

function ext(f: FileItem) {
  const i = f.filename.lastIndexOf('.')
  return i > 0 ? f.filename.slice(i + 1).toUpperCase().slice(0, 5) : 'FILE'
}

onMounted(() => load())
</script>

<template>
  <main class="main">
    <header class="topbar">
      <div class="topbar__titles">
        <h1>文件</h1>
      </div>
    </header>

    <div class="page">
      <div class="page__inner wide">
        <section v-if="usage" class="panel panel--inset usage" :class="`is-${level}`">
          <div class="usage__head">
            <div>
              <p class="usage__label">文件空间</p>
              <p class="usage__value">
                {{ formatSize(usage.used_bytes) }}
                <span v-if="usage.quota_bytes" class="muted">/ {{ formatSize(usage.quota_bytes) }}</span>
                <span v-else class="muted">· 不限空间</span>
              </p>
            </div>
            <button class="btn btn-danger-lite" :disabled="loading || !total" @click="confirm = 'old'">
              <Icon name="trash" :size="15" />删除 {{ OLD_DAYS }} 天前的文件
            </button>
          </div>
          <div v-if="usage.quota_bytes" class="workspace-progress usage__bar"><i :style="{ width: percent + '%' }" /></div>
          <p v-if="usage.workspace_bytes" class="caption usage__ws">
            其中 {{ formatSize(usage.workspace_bytes) }} 是{{ name }}运行代码时在对话里保存的文件，删除对应的对话就会清掉
          </p>
          <Transition name="fade">
            <p v-if="usage.full" class="usage__note usage__note--full">
              空间已经用满，现在不能上传文件，{{ name }}运行代码画的图也不会保存。删除一些不需要的文件就能恢复。
            </p>
            <p v-else-if="level === 'warn'" class="usage__note">空间快用完了。用满后将不能上传文件，可以先清理一些旧文件。</p>
            <p v-else class="usage__note muted">聊天时上传的图片和文档、{{ name }}运行代码画的图都在这里。删除后聊天记录里会显示「文件已删除」。</p>
          </Transition>
        </section>

        <div class="toolbar">
          <div class="type-switch filter" role="group" aria-label="文件类型">
            <button type="button" :class="{ 'is-active': kind === '' }" @click="setKind('')">全部</button>
            <button type="button" :class="{ 'is-active': kind === 'image' }" @click="setKind('image')">图片</button>
            <button type="button" :class="{ 'is-active': kind === 'document' }" @click="setKind('document')">文档</button>
          </div>
          <span class="spacer" />
          <span v-if="!loading" class="caption count">共 {{ total }} 个</span>
        </div>

        <div class="selectbar" :class="{ 'is-active': selected.size > 0 }">
          <label class="selectall">
            <input
              type="checkbox"
              :checked="allSelected"
              :indeterminate.prop="someSelected"
              :disabled="!files.length"
              @change="toggleAll"
            />
            <span>全选<template v-if="hasMore && files.length">（已加载的 {{ files.length }} 个）</template></span>
          </label>
          <Transition name="fade" mode="out-in">
            <span v-if="selected.size" key="n" class="selectbar__info">已选 {{ selected.size }} 个 · {{ formatSize(selectedBytes) }}</span>
            <span v-else key="hint" class="selectbar__info caption">勾选文件后可以批量删除</span>
          </Transition>
          <span class="spacer" />
          <button v-if="selected.size" class="btn btn-ghost" @click="selected = new Set()">取消选择</button>
          <button class="btn btn-danger" :disabled="!selected.size" @click="confirm = 'selected'">
            <Icon name="trash" :size="15" />删除所选
          </button>
        </div>

        <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>

        <div v-if="loading" class="grid" aria-hidden="true">
          <div v-for="i in 8" :key="i" class="card card--skel"><div class="skeleton card__thumb" /><div class="skeleton line" /><div class="skeleton line short" /></div>
        </div>

        <div v-else-if="!files.length && !error" class="empty">
          <span class="empty__icon"><Icon name="folder" :size="28" /></span>
          <h2>{{ kind === 'image' ? '还没有图片' : kind === 'document' ? '还没有文档' : '还没有文件' }}</h2>
          <p class="muted">在聊天里点回形针上传图片或文档，或者让{{ name }}画张图，文件就会出现在这里。</p>
        </div>

        <TransitionGroup v-else tag="div" name="list" class="grid">
          <article
            v-for="f in files"
            :key="f.id"
            class="card"
            :class="{ 'is-selected': selected.has(f.id) }"
          >
            <button
              type="button"
              class="card__thumb"
              :class="{ 'is-doc': f.kind !== 'image', 'is-loading': f.kind === 'image' && !thumbs[f.id] }"
              :aria-label="f.kind === 'image' ? `查看 ${f.filename}` : `下载 ${f.filename}`"
              @click="open(f)"
            >
              <img v-if="f.kind === 'image' && thumbs[f.id]" :src="thumbs[f.id]" :alt="f.filename" />
              <span v-else-if="f.kind === 'image'" :ref="(el) => observeThumb(el, f.id)" class="thumb-sentinel" />
              <span v-else class="doc"><Icon name="file" :size="26" /><b>{{ ext(f) }}</b></span>
            </button>
            <label class="card__check" :aria-label="`选择 ${f.filename}`" @click.stop>
              <input type="checkbox" :checked="selected.has(f.id)" @change="toggle(f.id)" />
              <span class="box"><Icon name="check" :size="13" /></span>
            </label>
            <div class="card__body" @click="toggle(f.id)">
              <p class="card__name" :title="f.filename">{{ f.filename }}</p>
              <p class="card__meta">
                <span>{{ formatSize(f.size) }}</span><span>·</span><span class="card__date" :title="dateTime(f.created_at)">{{ shortDate(f.created_at) }}</span>
              </p>
              <p class="card__meta">
                <span class="tag" :class="{ 'tag--gen': f.source === 'generated' }">{{ sourceLabel(f) }}</span>
                <button
                  v-if="f.conversation_id"
                  type="button"
                  class="conv-link"
                  :title="`打开对话：${f.conversation_title || '新对话'}`"
                  @click.stop="goConversation(f)"
                >
                  {{ f.conversation_title || '新对话' }}
                </button>
              </p>
            </div>
          </article>
        </TransitionGroup>

        <div v-if="hasMore && !loading" class="more">
          <button class="btn" :disabled="loadingMore" @click="load(false)">{{ loadingMore ? '正在加载' : '加载更多' }}</button>
        </div>
      </div>
    </div>

    <Modal
      :open="confirm === 'selected'"
      :title="`删除 ${selected.size} 个文件？`"
      subtitle="删除后不能恢复，会腾出相应的空间。聊天记录会保留，原来的位置显示「文件已删除」。"
      @close="confirm = null"
    >
      <div class="modal-actions">
        <button class="btn" @click="confirm = null">取消</button>
        <button class="btn btn-danger" :disabled="deleting" @click="doDelete">{{ deleting ? '正在删除' : '删除' }}</button>
      </div>
    </Modal>
    <Modal
      :open="confirm === 'old'"
      :title="`删除 ${OLD_DAYS} 天前的所有文件？`"
      :subtitle="`${OLD_DAYS} 天之前上传的图片、文档和代码生成的图片都会被删除，不能恢复。最近 ${OLD_DAYS} 天的文件不受影响。`"
      @close="confirm = null"
    >
      <div class="modal-actions">
        <button class="btn" @click="confirm = null">取消</button>
        <button class="btn btn-danger" :disabled="deleting" @click="doDelete">{{ deleting ? '正在删除' : '全部删除' }}</button>
      </div>
    </Modal>
  </main>
</template>

<style scoped>
.wide { max-width: 1040px; }
.usage { margin-bottom: 18px; }
.usage__head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.usage__label { margin: 0 0 2px; color: var(--color-muted); font-size: 12px; font-weight: 600; letter-spacing: .06em; }
.usage__value { margin: 0; font-size: 22px; font-family: var(--font-display); font-variant-numeric: tabular-nums; }
.usage__value .muted { font-size: 15px; font-family: var(--font-sans); }
.usage__bar { margin: 14px 0 10px; height: 6px; }
.usage__bar i { transition: width 600ms var(--ease-editorial), background-color 300ms ease; }
.is-warn .usage__bar i { background: var(--color-warning); }
.is-full .usage__bar i { background: var(--color-danger); }
.is-full { border-color: rgba(181, 51, 51, .3); }
.usage__ws { margin: -4px 0 0; }
.usage__note { margin: 10px 0 0; font-size: 13px; line-height: 1.6; }
.is-warn .usage__note { color: var(--color-warning); }
.usage__note--full { color: var(--color-danger); }

.toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; }
.filter { width: 240px; flex-shrink: 0; }
.filter button { white-space: nowrap; }
.spacer { flex: 1; }
.count { white-space: nowrap; }

.selectbar {
  position: sticky; top: -28px; z-index: 5; display: flex; align-items: center; flex-wrap: wrap; gap: 8px 12px; min-height: 52px;
  margin: 0 0 14px; padding: 8px 10px 8px 14px; border: 1px solid var(--color-divider); border-radius: var(--radius-lg);
  background: var(--color-canvas); transition: background-color var(--motion-fast) ease, border-color var(--motion-fast) ease, box-shadow var(--motion-fast) ease;
}
.selectbar.is-active { border-color: var(--color-hairline-strong); background: var(--color-surface); box-shadow: var(--shadow-float); }
.selectall { display: inline-flex; align-items: center; gap: 8px; color: var(--color-ink); font-size: 14px; cursor: pointer; user-select: none; }
.selectall input { width: 16px; height: 16px; }
.selectbar__info { color: var(--color-text-secondary); font-size: 13px; font-variant-numeric: tabular-nums; }

.grid { position: relative; display: grid; grid-template-columns: repeat(auto-fill, minmax(196px, 1fr)); gap: 12px; }
.card {
  position: relative; display: flex; flex-direction: column; overflow: hidden; border: 1px solid var(--color-divider);
  border-radius: var(--radius-lg); background: var(--color-surface);
  transition: border-color var(--motion-fast) ease, box-shadow var(--motion-fast) ease, transform var(--motion-fast) var(--ease-editorial);
}
.card:hover { border-color: var(--color-hairline-strong); box-shadow: var(--shadow-float); }
.card.is-selected { border-color: var(--color-accent); box-shadow: 0 0 0 3px rgba(91, 122, 74, .14); }
.card--skel { padding-bottom: 12px; }
.card--skel .line { height: 12px; margin: 12px 12px 0; }
.card--skel .line.short { width: 50%; }
.card__thumb {
  display: flex; aspect-ratio: 4 / 3; align-items: center; justify-content: center; overflow: hidden; padding: 0;
  background: var(--color-canvas-soft); cursor: zoom-in;
}
.card__thumb.is-doc { cursor: pointer; }
.card__thumb.is-loading { background: linear-gradient(90deg, var(--color-canvas-soft) 0%, var(--color-surface-hover) 40%, var(--color-canvas-soft) 80%); background-size: 200% 100%; animation: shimmer 1.3s infinite linear; }
.card__thumb img { width: 100%; height: 100%; object-fit: cover; animation: img-in 280ms ease; transition: transform 400ms var(--ease-editorial); }
.card:hover .card__thumb img { transform: scale(1.03); }
@keyframes img-in { from { opacity: 0; } }
.thumb-sentinel { width: 1px; height: 1px; }
.doc { display: grid; justify-items: center; gap: 6px; color: var(--color-text-secondary); }
.doc b { font-size: 12px; letter-spacing: .08em; color: var(--color-muted); }
.card__check { position: absolute; top: 8px; left: 8px; display: inline-flex; cursor: pointer; }
.card__check input { position: absolute; opacity: 0; width: 1px; height: 1px; }
.card__check .box {
  display: inline-flex; width: 24px; height: 24px; align-items: center; justify-content: center; border: 1.5px solid rgba(255, 255, 255, .95);
  border-radius: 7px; background: rgba(20, 20, 19, .28); color: transparent; box-shadow: 0 1px 3px rgba(20, 20, 19, .2);
  opacity: 0; transform: scale(.9);
  transition: opacity var(--motion-fast) ease, transform 220ms var(--ease-editorial), background-color var(--motion-fast) ease, color var(--motion-fast) ease;
}
.card:hover .box, .card__check input:focus-visible + .box, .is-selected .box, .selectbar.is-active ~ .grid .box { opacity: 1; transform: none; }
.card__check input:focus-visible + .box { outline: 2px solid var(--color-ink); outline-offset: 2px; }
.is-selected .box { border-color: var(--color-accent); background: var(--color-accent); color: #fff; }
.card__body { display: grid; gap: 4px; padding: 10px 12px 12px; cursor: pointer; }
.card__name { margin: 0; overflow: hidden; color: var(--color-ink); font-size: 14px; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.card__meta { display: flex; align-items: center; gap: 6px; min-width: 0; margin: 0; color: var(--color-muted); font-size: 12px; white-space: nowrap; }
.card__date { min-width: 0; overflow: hidden; text-overflow: ellipsis; }
.tag { flex-shrink: 0; padding: 1px 6px; border-radius: var(--radius-xs); background: var(--color-canvas-soft); color: var(--color-text-secondary); font-size: 11px; }
.tag--gen { background: var(--color-accent-tint); color: var(--color-accent-active); }
.conv-link { min-width: 0; overflow: hidden; padding: 0; color: var(--color-muted); font-size: 12px; text-align: left; text-overflow: ellipsis; white-space: nowrap; }
.conv-link:hover { color: var(--color-accent-active); text-decoration: underline; text-underline-offset: 2px; }
.more { margin-top: 20px; text-align: center; }
.empty { padding: 56px 16px; text-align: center; }
.empty__icon {
  display: inline-flex; width: 56px; height: 56px; align-items: center; justify-content: center; margin-bottom: 14px;
  border-radius: 50%; background: var(--color-canvas-soft); color: var(--color-muted);
}
.empty h2 { margin-bottom: 8px; font-size: 24px; }
.empty p { max-width: 380px; margin: 0 auto; font-size: 14px; line-height: 1.65; }
@media (hover: none) { .box { opacity: 1 !important; transform: none !important; } }
@media (max-width: 767px) {
  .selectbar { top: -20px; }
  .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .filter { width: 210px; }
  .usage__head .btn { width: 100%; }
}
</style>
