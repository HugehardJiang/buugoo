<script setup lang="ts">
// 备忘录：很多页，每页一个标题和 Markdown 正文。和布谷聊天时说「记到备忘录里」，它会挑一页（或新建一页）写进去；
// 这里也可以自己新建、修改、删除。内容不主动删就一直留着，平时不会进入和布谷的对话。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, ApiError, type MemoPage, type MemoPageInfo } from '../api'
import Modal from '../components/Modal.vue'
import TopBlur from '../components/TopBlur.vue'
import { shortWhen } from '../format'
import { renderMarkdown } from '../markdown'
import Icon from '../Icon.vue'
import { session } from '../session'
import { toast } from '../toast'

const name = computed(() => session.meta?.name ?? '布谷')
const pages = ref<MemoPageInfo[] | null>(null)
const loadError = ref('')
const query = ref('')
const current = ref<MemoPage | null>(null)
const editing = ref(false)
const title = ref('')
const content = ref('')
const saving = ref(false)
const savedAt = ref('')
const confirmDelete = ref(false)
const creating = ref(false)
const newTitle = ref('')
let timer: ReturnType<typeof setTimeout> | undefined

const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  return (pages.value ?? []).filter((p) => !q || p.title.toLowerCase().includes(q) || p.preview.toLowerCase().includes(q))
})
const dirty = computed(() => !!current.value && (title.value !== current.value.title || content.value !== current.value.content))

onMounted(async () => {
  await load()
  const id = Number(new URLSearchParams(location.search).get('p'))
  if (id) open(id)
  window.addEventListener('focus', onFocus)
})
onBeforeUnmount(() => {
  window.removeEventListener('focus', onFocus)
  if (dirty.value) save()
})

// 回到这个页面时刷新：可能刚在聊天里让布谷写了东西。
function onFocus() {
  if (!dirty.value) {
    load()
    if (current.value && !editing.value) open(current.value.id, false)
  }
}

async function load() {
  try {
    pages.value = (await api.memos()).pages
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function open(id: number, reset = true) {
  if (dirty.value) await save()
  try {
    const p = await api.memo(id)
    current.value = p
    title.value = p.title
    content.value = p.content
    if (reset) {
      editing.value = !p.content.trim()
      savedAt.value = ''
    }
    history.replaceState(null, '', `?p=${id}`)
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '打开失败')
  }
}

function back() {
  if (dirty.value) save()
  current.value = null
  history.replaceState(null, '', location.pathname)
}

// 编辑时停下来一秒自动保存。
watch([title, content], () => {
  clearTimeout(timer)
  if (dirty.value) timer = setTimeout(save, 1000)
})

async function save() {
  clearTimeout(timer)
  const p = current.value
  if (!p || !dirty.value || saving.value) return
  if (!title.value.trim()) {
    toast('标题不能为空')
    return
  }
  saving.value = true
  try {
    const patch: { title?: string; content?: string } = {}
    if (title.value !== p.title) patch.title = title.value
    if (content.value !== p.content) patch.content = content.value
    const np = await api.updateMemo(p.id, patch)
    if (current.value?.id === np.id) {
      current.value = np
      if (title.value.trim() === np.title) title.value = np.title
    }
    savedAt.value = '已保存'
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

async function create() {
  const t = newTitle.value.trim()
  if (!t) return
  try {
    const p = await api.createMemo(t)
    creating.value = false
    newTitle.value = ''
    await load()
    await open(p.id)
    editing.value = true
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '新建失败')
  }
}

async function remove() {
  confirmDelete.value = false
  const p = current.value
  if (!p) return
  try {
    await api.deleteMemo(p.id)
    current.value = null
    history.replaceState(null, '', location.pathname)
    toast(`已删除「${p.title}」`)
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '删除失败')
  }
}

function finishEdit() {
  save()
  editing.value = false
}
</script>

<template>
  <main class="main">
    <header class="topbar">
      <TopBlur />
      <div class="topbar__titles">
        <h1>备忘录</h1>
      </div>
    </header>
    <div class="page">
      <div class="memo" :class="{ 'has-page': !!current }">
        <aside class="memo__list">
          <p class="caption intro">和{{ name }}说「记到备忘录里」，它会挑一页或新建一页写进去。不删就一直留着，平时不会进入聊天。</p>
          <div class="list-tools">
            <input v-model="query" class="text-input" type="search" placeholder="搜索" aria-label="搜索备忘录" />
            <button type="button" class="btn btn-sm" @click="creating = true"><Icon name="plus" :size="15" /> 新建</button>
          </div>
          <form v-if="creating" class="new" @submit.prevent="create">
            <input v-model="newTitle" class="text-input" maxlength="40" placeholder="页面标题，例如：购物清单" aria-label="新页面标题" autofocus />
            <button class="btn btn-sm btn-primary" :disabled="!newTitle.trim()">建好</button>
            <button type="button" class="btn btn-sm btn-ghost" @click="creating = false">取消</button>
          </form>
          <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
          <div v-else-if="pages === null" class="skeleton skel" aria-hidden="true" />
          <p v-else-if="!pages.length" class="caption empty">还没有备忘录。可以点「新建」，或者直接在聊天里让{{ name }}记。</p>
          <ul v-else class="pages">
            <li v-for="p in shown" :key="p.id">
              <button type="button" class="page-item" :class="{ 'is-active': current?.id === p.id }" @click="open(p.id)">
                <span class="page-item__title">{{ p.title }}</span>
                <span class="page-item__preview">{{ p.preview || '（空）' }}</span>
                <span class="page-item__meta">{{ shortWhen(p.updated_at) }} · {{ p.size }} 字</span>
              </button>
            </li>
          </ul>
        </aside>

        <section v-if="current" class="memo__body panel panel--inset">
          <div class="body-head">
            <button type="button" class="btn btn-sm btn-ghost back" @click="back">‹ 全部</button>
            <input v-if="editing" v-model="title" class="text-input title-input" maxlength="40" aria-label="标题" />
            <h2 v-else class="title">{{ current.title }}</h2>
            <span class="caption status">{{ saving ? '正在保存' : dirty ? '未保存' : savedAt }}</span>
            <button v-if="editing" type="button" class="btn btn-sm btn-primary" @click="finishEdit">完成</button>
            <button v-else type="button" class="btn btn-sm" @click="editing = true"><Icon name="edit" :size="14" /> 编辑</button>
            <button type="button" class="icon-btn" aria-label="删除这一页" title="删除这一页" @click="confirmDelete = true"><Icon name="trash" :size="16" /></button>
          </div>
          <textarea
            v-if="editing"
            v-model="content"
            class="textarea editor"
            spellcheck="false"
            placeholder="写点什么……支持 Markdown，清单用「- 」开头"
            aria-label="内容"
          />
          <div v-else-if="current.content.trim()" class="md" @dblclick="editing = true" v-html="renderMarkdown(current.content)" />
          <p v-else class="caption">这一页还是空的，点「编辑」写点什么。</p>
          <p class="caption foot">最后修改：{{ shortWhen(current.updated_at) }}</p>
        </section>
        <section v-else class="memo__placeholder">
          <p class="caption">选一页查看，或者新建一页。</p>
        </section>
      </div>
    </div>

    <Modal :open="confirmDelete" :title="`删除「${current?.title}」？`" subtitle="这一页的内容会全部删除，不能恢复。" @close="confirmDelete = false">
      <div class="modal-actions">
        <button class="btn" @click="confirmDelete = false">取消</button>
        <button class="btn btn-danger" @click="remove">删除</button>
      </div>
    </Modal>
  </main>
</template>

<style scoped>
.memo { display: grid; grid-template-columns: 300px minmax(0, 1fr); gap: 28px; max-width: 1240px; margin: 0 auto; align-items: start; }
.memo__list { position: sticky; top: 72px; display: grid; gap: 12px; min-width: 0; }
.intro { margin: 0; line-height: 1.65; }
.list-tools, .new { display: flex; gap: 8px; align-items: center; }
.list-tools .text-input, .new .text-input {
  flex: 1; min-width: 0; min-height: 40px; border: 0; border-radius: 9999px; padding: 6px 16px;
  background: var(--md-surface-container-high);
  transition: background-color var(--motion-fast) var(--md-ease), box-shadow var(--motion-fast) var(--md-ease);
}
.list-tools .text-input:focus, .new .text-input:focus {
  background: var(--md-surface-container-lowest); box-shadow: var(--md-elev-1);
}
.skel { height: 120px; border-radius: var(--radius-xl); }
.empty { margin: 8px 0 0; }
.pages { display: grid; gap: 6px; margin: 0; padding: 0; list-style: none; }
.page-item {
  display: grid; gap: 3px; width: 100%; padding: 12px 16px; border: 0; border-radius: 18px; text-align: left;
  transition: background-color var(--motion-fast) var(--md-ease), box-shadow var(--motion-fast) var(--md-ease);
}
.page-item:hover { background: var(--md-surface-container-low); }
.page-item.is-active {
  background: var(--md-primary-container); color: var(--md-on-primary-container); box-shadow: none;
}
.page-item__title { color: var(--color-ink); font-size: 14px; font-weight: 500; overflow-wrap: anywhere; }
.page-item__preview { color: var(--color-text-secondary); font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.page-item__meta { color: var(--color-muted); font-size: 12px; }
.page-item.is-active .page-item__title,
.page-item.is-active .page-item__preview,
.page-item.is-active .page-item__meta { color: var(--md-on-primary-container); }
.page-item.is-active .page-item__title { font-weight: 600; }
.page-item.is-active .page-item__preview,
.page-item.is-active .page-item__meta { opacity: .82; }
.memo__body {
  min-width: 0; border: 0; border-radius: var(--radius-xxl);
}
.memo__placeholder {
  display: grid; place-items: center; min-height: 300px;
  border: 0; border-radius: var(--radius-xxl); background: var(--md-surface-container-low);
}
.body-head {
  display: flex; align-items: center; gap: 10px; margin-bottom: 18px; padding-bottom: 14px;
  border-bottom: 1px solid var(--color-hairline-subtle, rgba(20, 20, 19, .08));
}
.back { display: none; }
.title { flex: 1; min-width: 0; margin: 0; font-size: 21px; font-weight: 500; letter-spacing: -.02em; overflow-wrap: anywhere; word-break: normal; }
.title-input { flex: 1; min-width: 0; font-size: 18px; font-weight: 500; }
.status { white-space: nowrap; }
.editor { width: 100%; min-height: 58vh; border-radius: 16px; font-family: var(--font-mono); font-size: 14px; line-height: 1.75; }
.md { font-size: 15px; line-height: 1.8; overflow-wrap: anywhere; }
.md :deep(h1), .md :deep(h2), .md :deep(h3) { margin: 18px 0 8px; font-size: 17px; font-weight: 500; letter-spacing: -.015em; }
.md :deep(p) { margin: 0 0 12px; }
.md :deep(ul), .md :deep(ol) { margin: 0 0 12px; padding-left: 22px; }
.md :deep(pre) { overflow-x: auto; padding: 12px 14px; border-radius: var(--radius-lg); background: var(--md-surface-container-lowest); border: 0; }
.foot { margin: 20px 0 0; padding-top: 12px; border-top: 1px solid var(--color-hairline-subtle, rgba(20, 20, 19, .08)); }
@media (max-width: 820px) {
  .memo { grid-template-columns: minmax(0, 1fr); }
  .memo__list { position: static; }
  .memo.has-page .memo__list, .memo:not(.has-page) .memo__body, .memo__placeholder { display: none; }
  .back { display: inline-flex; margin-left: -8px; }
  /* 手机上：第一行是「全部 / 保存状态 / 按钮」，标题单独占第二行，不和按钮挤在一起被拆得一字一行。 */
  .body-head { flex-wrap: wrap; row-gap: 8px; }
  .body-head .status { margin-left: auto; }
  .body-head .btn { white-space: nowrap; }
  .title, .title-input { order: 10; flex: 1 0 100%; }
  .title { font-size: 20px; line-height: 1.35; overflow-wrap: break-word; word-break: normal; }
  .editor { min-height: 50vh; }
}
</style>
