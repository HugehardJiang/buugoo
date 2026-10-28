<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  api,
  ApiError,
  sendChat,
  type Attachment,
  type PersonaPreset,
  type Usage,
  type Conversation,
  type Message,
  type ModelView,
  type Thinking,
  type UsedMemory,
} from '../api'
import Icon from '../Icon.vue'
import AttachmentList from '../components/AttachmentList.vue'
import Modal from '../components/Modal.vue'
import ContextMeter from '../components/ContextMeter.vue'
import FatBird from '../components/FatBird.vue'
import ToolSteps from '../components/ToolSteps.vue'
import { splitBubbles } from '../bubbles'
import { dayKey, dayLabel, formatSize, renderMarkdown, shortWhen, timeOf } from '../format'
import { memoriesUnlocked } from '../memoryLock'
import Collapse from '../components/Collapse.vue'
import { session } from '../session'
import { toast } from '../toast'
import { copyDiagnostics } from '../diag'

interface Item extends Message {
  memories?: UsedMemory[]
  streaming?: boolean
  error?: string
  // key 是本页面内稳定的标识：新消息先用临时 ID，保存后 id 会变，key 不变，避免重新渲染造成闪动。
  key?: number
  // fresh 表示本次新发出的消息，出现时带入场动画。
  fresh?: boolean
}

// Pending 是输入框中待发送的文件：选择后立即上传，发送消息时带上已上传的 ID。
interface Pending {
  key: number
  name: string
  size: number
  isImage: boolean
  preview?: string
  status: 'uploading' | 'done' | 'error'
  att?: Attachment
  error?: string
}

const route = useRoute()
const router = useRouter()

const convs = ref<Conversation[]>([])
const currentId = ref(0)

// copyDiag 复制这条失败回复的诊断信息。
function copyDiag(m: { id: number; model?: string; error?: string; created_at: string }) {
  copyDiagnostics({ 对话: currentId.value, 消息: m.id > 0 ? m.id : '（没有保存）', 模型: m.model, 时间: m.created_at, 错误: m.error || '这次没有回复成功' })
}
const models = ref<ModelView[]>([])
const hasModel = ref(true)
const embeddingReady = ref(true)

const items = ref<Item[]>([])
const hasMore = ref(false)
const loadingMore = ref(false)
const loadError = ref('')
const input = ref('')
const sending = ref(false)
const scroller = ref<HTMLElement | null>(null)
const textarea = ref<HTMLTextAreaElement | null>(null)
const openMemories = ref<Set<number>>(new Set())
const openThinking = ref<Set<number>>(new Set())
const drawer = ref(false)
const renaming = ref(false)
const renameText = ref('')
const deleting = ref(false)
let controller: AbortController | null = null
const pending = ref<Pending[]>([])
const fileInput = ref<HTMLInputElement | null>(null)
const dragging = ref(false)
let pendingSeq = 0
const usage = ref<Usage | null>(null)
const persona = ref<PersonaPreset | null>(null)
const showJump = ref(false)
const docAccept = 'image/jpeg,image/png,image/gif,image/webp,.pdf,.docx,.txt,.md,.markdown,.csv,.tsv,.json,.yaml,.yml,.xml,.html,.log,.py,.js,.ts,.go,.java,.c,.cpp,.h,.rs,.sh,.sql'
// 能运行代码时什么文件都能发（表格、压缩包等交给代码处理）。
const accept = computed(() => (session.meta?.uploads?.any_file ? '' : docAccept))
const uploading = computed(() => pending.value.some((p) => p.status === 'uploading'))
const readyFiles = computed(() => pending.value.filter((p) => p.status === 'done'))
// 文件空间用满后不能再上传任何文件。
const uploadBlocked = computed(() => !!usage.value?.full)
const canSend = computed(() => !!current.value && !uploading.value && (!!input.value.trim() || readyFiles.value.length > 0))

const name = computed(() => session.meta?.name ?? '布谷')
const isAdmin = computed(() => !!session.user?.is_admin)
const current = computed(() => convs.value.find((c) => c.id === currentId.value) ?? null)
const title = computed(() => current.value?.title || '新对话')
const defaultModel = computed(() => models.value.find((m) => m.is_default) ?? models.value[0] ?? null)
// 对话实际使用的模型：未选择或已不可用时使用默认模型。
const activeModel = computed(() => {
  const id = current.value?.model_id
  return (id != null && models.value.find((m) => m.id === id)) || defaultModel.value
})
const thinkingOptions = computed<{ v: Thinking; l: string }[]>(() => {
  switch (activeModel.value?.thinking_style) {
    case 'deepseek':
      // DeepSeek 只有 low / high / max 三档强度。
      return [
        { v: 'off', l: '关' },
        { v: 'low', l: '低' },
        { v: 'medium', l: '高' },
        { v: 'high', l: '最高' },
      ]
    case 'model_switch':
      return [
        { v: 'off', l: '不思考' },
        { v: 'medium', l: '深度思考' },
      ]
    case 'reasoning_effort':
    case 'enable_thinking':
      return [
        { v: 'off', l: '关' },
        { v: 'low', l: '低' },
        { v: 'medium', l: '中' },
        { v: 'high', l: '高' },
      ]
    default:
      return []
  }
})
const thinkingValue = computed(() => {
  const t = current.value?.thinking ?? 'off'
  // 「换用推理模型」只有开和关，任何等级都视为开。
  if (activeModel.value?.thinking_style === 'model_switch' && t !== 'off') return 'medium'
  return t
})

// 在每天第一条消息前插入日期分隔条。
const rows = computed(() => {
  const out: ({ kind: 'day'; key: string; label: string } | { kind: 'msg'; item: Item })[] = []
  let last = ''
  for (const it of items.value) {
    const k = dayKey(it.created_at)
    if (k !== last) {
      out.push({ kind: 'day', key: `day-${k}`, label: dayLabel(it.created_at) })
      last = k
    }
    out.push({ kind: 'msg', item: it })
  }
  return out
})

function errText(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback
}

function nearBottom(): boolean {
  const el = scroller.value
  return !el || el.scrollHeight - el.scrollTop - el.clientHeight < 120
}

async function scrollToBottom(force = false, smooth = false) {
  if (!force && !nearBottom()) return
  await nextTick()
  const el = scroller.value
  if (!el) return
  if (smooth) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
  else el.scrollTop = el.scrollHeight
}

// 往上翻看时出现「回到最新」按钮。
function onScroll() {
  showJump.value = !nearBottom()
}

function uid(it: Item) {
  return it.key ?? it.id
}

async function refreshUsage() {
  if (!session.meta?.uploads) return
  try {
    usage.value = await api.usage()
  } catch {
    // 用量只用于提示，读取失败不影响聊天。
  }
}

async function loadSide() {
  const [c, m, me] = await Promise.all([api.conversations(), api.models(), api.me()])
  refreshUsage()
  api
    .presets()
    .then((r) => (persona.value = r.presets.find((p) => p.id === r.current) ?? null))
    .catch(() => {})
  convs.value = c.conversations
  models.value = m.models
  hasModel.value = me.has_model
  embeddingReady.value = me.embedding_ready
}

async function openConversation(id: number) {
  currentId.value = id
  drawer.value = false
  items.value = []
  hasMore.value = false
  loadError.value = ''
  openMemories.value = new Set()
  openThinking.value = new Set()
  if (String(route.query.c ?? '') !== String(id)) router.replace({ query: { c: String(id) } })
  try {
    const r = await api.messages(id, 0, 50)
    if (currentId.value !== id) return
    items.value = r.messages
    hasMore.value = r.has_more
    await scrollToBottom(true)
  } catch (e) {
    loadError.value = errText(e, '加载失败')
  }
}

async function loadInitial() {
  try {
    await loadSide()
  } catch (e) {
    loadError.value = errText(e, '加载失败')
    return
  }
  const wanted = Number(route.query.c)
  const target = convs.value.find((c) => c.id === wanted) ?? convs.value[0]
  if (target) await openConversation(target.id)
}

async function loadMore() {
  if (!items.value.length || loadingMore.value) return
  loadingMore.value = true
  const el = scroller.value
  const prevHeight = el?.scrollHeight ?? 0
  try {
    const r = await api.messages(currentId.value, items.value[0].id, 50)
    items.value = [...r.messages, ...items.value]
    hasMore.value = r.has_more
    await nextTick()
    if (el) el.scrollTop = el.scrollHeight - prevHeight
  } catch (e) {
    loadError.value = errText(e, '加载失败')
  } finally {
    loadingMore.value = false
  }
}

async function newConversation() {
  if (sending.value) return
  // 当前对话还是空的就直接用它，避免堆出一串空对话。
  if (current.value && !items.value.length) {
    drawer.value = false
    textarea.value?.focus()
    return
  }
  try {
    const c = await api.createConversation()
    convs.value = [c, ...convs.value]
    await openConversation(c.id)
    textarea.value?.focus()
  } catch (e) {
    toast(errText(e, '新建对话失败'))
  }
}

function replaceConv(c: Conversation) {
  const i = convs.value.findIndex((x) => x.id === c.id)
  if (i >= 0) convs.value[i] = c
}

async function patchCurrent(patch: { title?: string; model_id?: number | null; thinking?: Thinking }) {
  if (!current.value) return
  try {
    replaceConv(await api.updateConversation(current.value.id, patch))
  } catch (e) {
    toast(errText(e, '修改失败'))
  }
}

const pickerOpen = ref(false)
const pickerEl = ref<HTMLElement | null>(null)

function chooseModel(id: number | null) {
  patchCurrent({ model_id: id })
}

// 点击面板外部或按 Esc 时收起。
function onDocPointer(e: PointerEvent) {
  if (pickerOpen.value && pickerEl.value && !pickerEl.value.contains(e.target as Node)) pickerOpen.value = false
}
function onDocKey(e: KeyboardEvent) {
  if (e.key === 'Escape') pickerOpen.value = false
}
onMounted(() => {
  document.addEventListener('pointerdown', onDocPointer)
  document.addEventListener('keydown', onDocKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointer)
  document.removeEventListener('keydown', onDocKey)
})

const thinkingLabel = computed(() => {
  if (!thinkingOptions.value.length) return ''
  const o = thinkingOptions.value.find((x) => x.v === thinkingValue.value)
  if (!o || o.v === 'off') return '不思考'
  return o.l.length === 1 || o.l === '最高' ? `思考${o.l}` : o.l
})

function modelTraits(m: ModelView) {
  const t: string[] = []
  if (m.thinking_style !== 'none') t.push('可思考')
  if (m.vision) t.push('看图')
  if (m.tools) t.push('工具')
  return t.join(' · ')
}

function startRename() {
  renameText.value = current.value?.title ?? ''
  renaming.value = true
}

async function saveRename() {
  await patchCurrent({ title: renameText.value })
  renaming.value = false
}

async function confirmDelete() {
  const id = currentId.value
  try {
    await api.deleteConversation(id)
    deleting.value = false
    convs.value = convs.value.filter((c) => c.id !== id)
    toast('对话已删除，聊过的事布谷仍然记得')
    if (convs.value.length) await openConversation(convs.value[0].id)
    else {
      const c = await api.createConversation()
      convs.value = [c]
      await openConversation(c.id)
    }
  } catch (e) {
    toast(errText(e, '删除失败'))
  }
}

function autoGrow() {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${Math.min(el.scrollHeight, 180)}px`
}

function maxFiles() {
  return session.meta?.uploads?.max_files ?? 8
}

async function addFiles(files: FileList | File[]) {
  const limits = session.meta?.uploads
  if (!limits) {
    toast('当前服务不支持上传文件')
    return
  }
  if (uploadBlocked.value) {
    toast('文件空间已经用满，去「文件」里删除一些不需要的文件后才能上传')
    return
  }
  for (const file of Array.from(files)) {
    if (pending.value.length >= maxFiles()) {
      toast(`一条消息最多带 ${maxFiles()} 个文件`)
      break
    }
    if (file.size > limits.max_file_bytes) {
      toast(`「${file.name}」太大了，单个文件不能超过 ${formatSize(limits.max_file_bytes)}`)
      continue
    }
    const isImage = file.type.startsWith('image/')
    pending.value.push({
      key: ++pendingSeq, name: file.name || '粘贴的图片', size: file.size, isImage, status: 'uploading',
      preview: isImage ? URL.createObjectURL(file) : undefined,
    })
    // 取回数组中的响应式代理，之后的修改才会更新界面。
    const p = pending.value[pending.value.length - 1]
    try {
      p.att = await api.uploadFile(file)
      p.status = 'done'
    } catch (e) {
      p.status = 'error'
      p.error = errText(e, '上传失败')
      toast(`「${p.name}」${p.error}`)
      if (e instanceof ApiError && e.code === 'quota_exceeded') refreshUsage()
    }
  }
}

function removePending(p: Pending) {
  pending.value = pending.value.filter((x) => x.key !== p.key)
  if (p.preview) URL.revokeObjectURL(p.preview)
  // 已上传但没发送的文件顺手删掉（不删也会在 24 小时后自动清理）。
  if (p.att) api.deleteUpload(p.att.id).catch(() => {})
}

function onPickFiles(e: Event) {
  const el = e.target as HTMLInputElement
  if (el.files?.length) addFiles(el.files)
  el.value = ''
}

function onPaste(e: ClipboardEvent) {
  const files = e.clipboardData?.files
  if (files && files.length) {
    e.preventDefault()
    addFiles(files)
  }
}

function onDrop(e: DragEvent) {
  dragging.value = false
  if (e.dataTransfer?.files.length) addFiles(e.dataTransfer.files)
}

async function send() {
  const content = input.value.trim()
  if (!canSend.value || sending.value || !current.value) return
  const convId = current.value.id
  const files = readyFiles.value
  const attachments = files.map((p) => p.att!)
  const sentPending = pending.value
  sending.value = true
  input.value = ''
  pending.value = []
  await nextTick()
  autoGrow()

  const now = new Date().toISOString()
  const tmp = -Date.now()
  const userItem: Item = { id: tmp, key: tmp, fresh: true, role: 'user', content, attachments, status: 'complete', created_at: now }
  const reply: Item = {
    id: tmp - 1, key: tmp - 1, fresh: true, role: 'assistant', content: '', reasoning: '', tools: [], status: 'complete', created_at: now, streaming: true,
  }
  items.value.push(userItem, reply)
  const r = items.value[items.value.length - 1]
  await scrollToBottom(true, true)

  controller = new AbortController()
  try {
    const final = await sendChat(
      convId,
      content,
      attachments.map((a) => a.id),
      {
        onStart: (d) => {
          Object.assign(items.value[items.value.length - 2], d.user_message, { attachments })
          r.id = d.assistant_message_id
          replaceConv(d.conversation)
          for (const p of sentPending) if (p.preview) URL.revokeObjectURL(p.preview)
        },
        onTool: (ev) => {
          const list = r.tools ?? (r.tools = [])
          const i = list.findIndex((x) => x.id === ev.id)
          if (i >= 0) list[i] = ev
          else list.push(ev)
          scrollToBottom()
        },
        onMemories: (m) => (r.memories = m),
        onReasoning: (t) => {
          // 思考过程默认折叠，想看时再点开。
          r.reasoning += t
          scrollToBottom()
        },
        onDelta: (t) => {
          r.content += t
          scrollToBottom()
        },
      },
      controller.signal,
    )
    Object.assign(r, { ...final, memories: r.memories, tools: final.tools ?? r.tools })
  } catch (e) {
    if ((e as Error).name === 'AbortError') {
      r.status = 'interrupted'
    } else {
      r.status = 'error'
      r.error = errText(e, '这次没有发送成功')
      // 请求在开始推送前就失败（例如超出限额），用户消息并未保存，放回输入框。
      if (r.id < 0) {
        items.value.splice(items.value.length - 2, 1)
        input.value = content
        pending.value = sentPending
      }
    }
  } finally {
    r.streaming = false
    sending.value = false
    controller = null
    scrollToBottom()
    // 上下文用量变了（可能刚压缩过，或者这一轮超过了阈值）。
    meter.value?.refresh()
    // 上传了文件或代码画了图，更新空间用量。
    if (attachments.length || r.tools?.some((t) => t.images?.length || t.files?.length)) refreshUsage()
    // 对话的最近时间与标题变了，重新排序。
    const c = convs.value.find((x) => x.id === convId)
    if (c) {
      c.last_message_at = new Date().toISOString()
      convs.value = [c, ...convs.value.filter((x) => x.id !== convId)]
    }
  }
}

function stop() {
  controller?.abort()
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    send()
  }
}

// 上下文用量环：超过阈值后，下一条消息会先压缩，等待时提示一下。
const meter = ref<InstanceType<typeof ContextMeter> | null>(null)
const compactPending = ref(false)

// 代码生成的图片显示在回复正文下方，发给用户的文件显示为可以下载的文件。
function toolImages(m: Item): Attachment[] {
  return m.tools?.flatMap((t) => t.images ?? []) ?? []
}
function toolFiles(m: Item): Attachment[] {
  return m.tools?.flatMap((t) => t.files ?? []) ?? []
}
const channelLabels: Record<string, string> = { weixin: '微信', qq: 'QQ', telegram: 'Telegram' }
// 对话列表分两类：网页上的普通聊天，和微信、QQ、Telegram 里的外部聊天（每个聊天软件一段对话）。
const convSections = computed(() => [
  { key: 'web', label: '普通聊天', items: convs.value.filter((c) => !c.channel) },
  { key: 'ext', label: '外部聊天', items: convs.value.filter((c) => !!c.channel) },
])

function toggle(which: 'memories' | 'thinking', id: number) {
  // id 是 uid(item)
  const set = which === 'memories' ? openMemories : openThinking
  const wasAtBottom = nearBottom()
  const s = new Set(set.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  set.value = s
  if (wasAtBottom) scrollToBottom(true)
}

function modelLabel(m: ModelView) {
  return m.scope === 'personal' ? `${m.name}（我的）` : m.name
}

watch(
  () => route.query.c,
  (c) => {
    const id = Number(c)
    if (id && id !== currentId.value && convs.value.some((x) => x.id === id)) openConversation(id)
  },
)

// 在微信/QQ 里聊的会出现新的对话、对话顺序也会变：定期刷新对话列表（保留本地的改动，例如还没发消息的新对话）。
async function refreshConvs() {
  if (sending.value || document.visibilityState !== 'visible') return
  try {
    const r = await api.conversations()
    if (sending.value) return
    const known = new Set(r.conversations.map((c) => c.id))
    const local = convs.value.filter((c) => !known.has(c.id))
    const next = [...local, ...r.conversations]
    const changed = next.length !== convs.value.length || next.some((c, i) => {
      const o = convs.value[i]
      return !o || o.id !== c.id || o.title !== c.title || o.last_message_at !== c.last_message_at
    })
    if (changed) convs.value = next
  } catch {
    // 下次再试。
  }
}

// 布谷可能主动发来消息（提醒、定时任务、主动关心，或在微信/QQ 里聊的）：页面可见时定期检查当前对话有没有新消息。
let pollTimer: ReturnType<typeof setInterval> | undefined
async function checkNew() {
  refreshConvs()
  const id = currentId.value
  if (!id || sending.value || document.visibilityState !== 'visible') return
  try {
    const { latest_id } = await api.latestMessage(id)
    const known = items.value.reduce((m, it) => Math.max(m, it.id), 0)
    if (latest_id <= known || currentId.value !== id || sending.value) return
    const r = await api.messages(id, 0, 50)
    if (currentId.value !== id || sending.value) return
    const fresh = r.messages.filter((m) => m.id > known).map((m) => ({ ...m, fresh: true }))
    if (!fresh.length) return
    const wasAtBottom = nearBottom()
    items.value = [...items.value, ...fresh]
    if (wasAtBottom) scrollToBottom(true, true)
    meter.value?.refresh()
    const c = convs.value.find((x) => x.id === id)
    if (c) {
      c.last_message_at = fresh[fresh.length - 1].created_at
      convs.value = [c, ...convs.value.filter((x) => x.id !== id)]
    }
  } catch {
    // 只是检查新消息，失败了下次再试。
  }
}
function onVisible() {
  if (document.visibilityState === 'visible') checkNew()
}
onMounted(() => {
  loadInitial()
  pollTimer = setInterval(checkNew, 20000)
  document.addEventListener('visibilitychange', onVisible)
  window.addEventListener('focus', checkNew)
})
onBeforeUnmount(() => {
  clearInterval(pollTimer)
  document.removeEventListener('visibilitychange', onVisible)
  window.removeEventListener('focus', checkNew)
})
</script>

<template>
  <div class="chat-shell">
    <Transition name="fade">
      <div v-if="drawer" class="drawer-backdrop" @click="drawer = false" />
    </Transition>
    <aside class="sidebar" :class="{ 'is-open': drawer }" aria-label="对话列表">
      <div class="sidebar__head">
        <span class="eyebrow">对话</span>
        <button class="btn" :disabled="sending" @click="newConversation">
          <Icon name="plus" :size="16" />新对话
        </button>
      </div>
      <nav class="sidebar__list" aria-label="对话">
        <template v-for="sec in convSections" :key="sec.key">
          <p v-if="sec.items.length || sec.key === 'web'" class="conv-sec">{{ sec.label }}</p>
          <TransitionGroup tag="div" name="list" class="category-filter-list">
            <button
              v-for="c in sec.items"
              :key="c.id"
              class="category-filter"
              :class="{ 'is-active': c.id === currentId }"
              :disabled="sending && c.id !== currentId"
              @click="openConversation(c.id)"
            >
              <span><i v-if="c.channel" class="src-tag" :class="`src-tag--${c.channel}`">{{ channelLabels[c.channel] }}</i>{{ c.title || (c.channel ? `${channelLabels[c.channel]}里的聊天` : '新对话') }}</span>
              <small>{{ shortWhen(c.last_message_at ?? c.created_at) }}</small>
            </button>
          </TransitionGroup>
          <p v-if="sec.key === 'web' && !sec.items.length" class="caption conv-empty">还没有网页上的对话</p>
        </template>
      </nav>
      <p class="caption sidebar__foot">每段对话各自独立，但{{ name }}记得的事在所有对话里都通用。</p>
    </aside>

    <main class="main chat">
      <header class="topbar">
        <button class="icon-btn menu-btn" aria-label="对话列表" @click="drawer = true"><Icon name="menu" /></button>
        <div class="topbar__titles">
          <h1 class="conv-title">{{ title }}</h1>
        </div>
        <div v-if="current" class="topbar__actions">
          <ContextMeter ref="meter" :conversation-id="currentId" :busy="sending" @status="(s) => (compactPending = !!s?.pending)" />
          <button class="icon-btn" aria-label="重命名对话" title="重命名" @click="startRename"><Icon name="edit" :size="18" /></button>
          <button class="icon-btn" aria-label="删除对话" title="删除" :disabled="sending" @click="deleting = true"><Icon name="trash" :size="18" /></button>
        </div>
      </header>

      <div ref="scroller" class="scroller" @scroll.passive="onScroll">
        <div class="thread">
          <p v-if="!hasModel" class="form-flash">
            还没有可用的聊天模型。去 <RouterLink to="/settings#models" class="link">设置 → 模型</RouterLink> 添加一个，就可以开始聊天了。
          </p>
          <p v-else-if="!embeddingReady" class="form-flash">
            <template v-if="isAdmin">
              还没有配置向量模型，{{ name }}暂时记不住聊过的事。去 <RouterLink to="/settings#embedding" class="link">设置 → 向量模型</RouterLink> 配置。
            </template>
            <template v-else>记忆功能暂时不可用，{{ name }}这段时间记不住聊过的事。</template>
          </p>
          <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>

          <div v-if="hasMore" class="more">
            <button class="btn btn-ghost" :disabled="loadingMore" @click="loadMore">
              {{ loadingMore ? '正在加载' : '加载更早的消息' }}
            </button>
          </div>

          <Transition name="fade">
            <div v-if="!items.length && !loadError && current" class="empty">
              <h2>最近过得怎么样。</h2>
              <p class="muted">随便说点什么。{{ name }}会记住你提到的事，下次自然地接上。</p>
              <RouterLink v-if="persona" to="/settings#persona" class="persona-chip" title="换一种性格">
                <Icon name="sparkle" :size="14" />
                <span>现在的性格：{{ persona.label === '默认' ? '默认' : persona.label }}</span>
                <span class="persona-chip__go">换一个</span>
              </RouterLink>
            </div>
          </Transition>

          <template v-for="row in rows" :key="row.kind === 'day' ? row.key : uid(row.item)">
            <div v-if="row.kind === 'day'" class="day"><span>{{ row.label }}</span></div>
            <article v-else class="msg" :class="[row.item.role, { 'is-fresh': row.item.fresh }]">
              <div v-if="row.item.reasoning" class="thinking">
                <button
                  class="thinking__toggle"
                  :aria-expanded="openThinking.has(uid(row.item))"
                  @click="toggle('thinking', uid(row.item))"
                >
                  <Icon name="chevron" :size="14" class="chev" :class="{ 'is-open': openThinking.has(uid(row.item)) }" />
                  <span :class="{ 'thinking__live': row.item.streaming && !row.item.content }">{{ row.item.streaming && !row.item.content ? '正在思考…' : '思考过程' }}</span>
                </button>
                <Collapse :open="openThinking.has(uid(row.item))">
                  <div class="thinking__body">{{ row.item.reasoning }}</div>
                </Collapse>
              </div>
              <ToolSteps v-if="row.item.tools?.length" :steps="row.item.tools" />
              <AttachmentList v-if="row.item.attachments?.length" :items="row.item.attachments" align="end" />
              <div v-if="row.item.role === 'assistant'" class="bubbles">
                <div
                  v-for="(b, i) in row.item.content ? splitBubbles(row.item.content) : []"
                  :key="i"
                  class="bubble bubble--bot md"
                  v-html="renderMarkdown(b)"
                />
              </div>
              <div v-else-if="row.item.content" class="bubble">{{ row.item.content }}</div>
              <AttachmentList v-if="toolImages(row.item).length" :items="toolImages(row.item)" size="large" />
              <AttachmentList v-if="toolFiles(row.item).length" :items="toolFiles(row.item)" />
              <FatBird
                v-if="row.item.streaming && !row.item.content && !row.item.tools?.some((t) => t.status === 'running')"
                :label="row.item.reasoning ? '正在思考' : compactPending ? '先整理一下之前聊的' : '正在回复'"
              />
              <div class="meta">
                <span>{{ timeOf(row.item.created_at) }}</span>
                <span v-if="row.item.role === 'assistant' && row.item.model && !row.item.streaming">· {{ row.item.model }}</span>
                <span v-if="row.item.status === 'interrupted'">· 已停止</span>
                <span v-if="row.item.status === 'error'" class="err">· {{ row.item.error || '这次没有回复成功' }}</span>
                <button v-if="row.item.status === 'error'" type="button" class="chip-note diag" @click="copyDiag(row.item)">复制诊断信息</button>
                <button
                  v-if="memoriesUnlocked && row.item.memories?.length"
                  class="chip-note recall-toggle"
                  :aria-expanded="openMemories.has(uid(row.item))"
                  @click="toggle('memories', uid(row.item))"
                >
                  想起了 {{ row.item.memories.length }} 件事
                </button>
              </div>
              <Collapse v-if="memoriesUnlocked && row.item.memories?.length" :open="openMemories.has(uid(row.item))">
                <ul class="recalled">
                  <li v-for="m in row.item.memories" :key="m.id">
                    <span class="badge-pill">{{ m.source === 'stated' ? '你说过' : '推测' }}</span>
                    <span>{{ m.content }}</span>
                  </li>
                </ul>
              </Collapse>
            </article>
          </template>
        </div>
      </div>

      <div class="jump-wrap">
        <Transition name="pop">
          <button v-if="showJump && items.length" type="button" class="jump" aria-label="回到最新消息" @click="scrollToBottom(true, true)">
            <Icon name="chevron" :size="16" class="jump__icon" />{{ sending ? '正在回复' : '回到最新' }}
          </button>
        </Transition>
      </div>

      <form
        class="composer"
        :class="{ 'is-dragging': dragging }"
        @submit.prevent="send"
        @dragover.prevent="dragging = true"
        @dragleave.self="dragging = false"
        @drop.prevent="onDrop"
      >
        <Transition name="fade">
          <p v-if="uploadBlocked" class="quota-note">
            <Icon name="folder" :size="15" />
            <span>文件空间已经用满，暂时不能上传文件。</span>
            <RouterLink to="/files" class="link">去清理</RouterLink>
          </p>
        </Transition>
        <TransitionGroup v-if="pending.length" tag="div" name="list" class="pending">
          <div v-for="p in pending" :key="p.key" class="pending__item" :class="`is-${p.status}`" :title="p.error || p.name">
            <img v-if="p.preview" :src="p.preview" :alt="p.name" class="pending__thumb" />
            <span v-else class="pending__file"><Icon name="file" :size="16" /><span>{{ p.name }}</span></span>
            <span v-if="p.status === 'uploading'" class="pending__state">上传中</span>
            <span v-else-if="p.status === 'error'" class="pending__state">失败</span>
            <button type="button" class="pending__remove" :aria-label="`移除 ${p.name}`" @click="removePending(p)"><Icon name="x" :size="12" /></button>
          </div>
        </TransitionGroup>
        <div class="composer__box">
          <textarea
            ref="textarea"
            v-model="input"
            class="composer__input"
            rows="1"
            :placeholder="`和${name}说点什么`"
            aria-label="消息"
            maxlength="4000"
            @input="autoGrow"
            @keydown="onKeydown"
            @paste="onPaste"
          />
          <div class="composer__bar">
            <button
              v-if="session.meta?.uploads"
              type="button"
              class="icon-btn"
              aria-label="添加图片或文件"
              :title="uploadBlocked ? '文件空间已满，暂时不能上传' : '添加图片或文件（也可以粘贴或拖进来）'"
              :disabled="sending || uploadBlocked"
              @click="fileInput?.click()"
            >
              <Icon name="clip" />
            </button>
            <input ref="fileInput" type="file" multiple :accept="accept" hidden @change="onPickFiles" />
            <span class="spacer" />
            <div v-if="models.length" ref="pickerEl" class="picker">
              <button
                type="button"
                class="picker__chip"
                :aria-expanded="pickerOpen"
                aria-label="选择模型与思考等级"
                :disabled="sending"
                @click="pickerOpen = !pickerOpen"
              >
                <span class="picker__model">{{ activeModel?.name ?? '默认模型' }}</span>
                <span v-if="thinkingLabel" class="picker__think">· {{ thinkingLabel }}</span>
                <Icon name="chevron" :size="12" class="picker__chev" />
              </button>
              <Transition name="pop">
              <div v-if="pickerOpen" class="picker__panel" role="dialog" aria-label="模型与思考等级">
                <p class="picker__title">模型</p>
                <div class="picker__list">
                  <button
                    type="button"
                    class="picker__opt"
                    :class="{ 'is-active': current?.model_id == null }"
                    @click="chooseModel(null)"
                  >
                    <span>默认模型</span>
                    <small v-if="defaultModel">{{ defaultModel.name }}</small>
                  </button>
                  <button
                    v-for="m in models"
                    :key="m.id"
                    type="button"
                    class="picker__opt"
                    :class="{ 'is-active': current?.model_id === m.id }"
                    @click="chooseModel(m.id)"
                  >
                    <span>{{ modelLabel(m) }}</span>
                    <small>{{ modelTraits(m) }}</small>
                  </button>
                </div>
                <template v-if="thinkingOptions.length">
                  <p class="picker__title">思考</p>
                  <div class="type-switch picker__think-switch" role="group" aria-label="思考等级">
                    <button
                      v-for="o in thinkingOptions"
                      :key="o.v"
                      type="button"
                      :class="{ 'is-active': thinkingValue === o.v }"
                      @click="patchCurrent({ thinking: o.v })"
                    >
                      {{ o.l }}
                    </button>
                  </div>
                </template>
              </div>
              </Transition>
            </div>
            <button v-if="sending" type="button" class="btn" @click="stop">停止</button>
            <button v-else class="btn btn-primary" :disabled="!canSend" title="Enter 发送，Shift+Enter 换行">发送</button>
          </div>
        </div>
      </form>
    </main>

    <Modal :open="renaming" title="重命名对话" @close="renaming = false">
      <form @submit.prevent="saveRename">
        <div class="field">
          <label class="field__label" for="conv-title">标题</label>
          <input id="conv-title" v-model="renameText" class="text-input" maxlength="60" placeholder="新对话" />
          <p class="field__hint">留空则用第一句话自动生成。</p>
        </div>
        <div class="modal-actions">
          <button type="button" class="btn" @click="renaming = false">取消</button>
          <button class="btn btn-primary">保存</button>
        </div>
      </form>
    </Modal>

    <Modal
      :open="deleting"
      title="删除这段对话？"
      :subtitle="`聊天记录会被删除，无法恢复。${name}从中记住的事不受影响，可以在记忆本里单独删除。`"
      @close="deleting = false"
    >
      <div class="modal-actions">
        <button class="btn" @click="deleting = false">取消</button>
        <button class="btn btn-danger" @click="confirmDelete">删除对话</button>
      </div>
    </Modal>
  </div>
</template>

<style scoped>
.src-tag { display: inline-block; margin-right: 6px; padding: 0 5px; border-radius: 4px; background: var(--color-accent-tint); color: var(--color-accent-active); font-size: 11px; font-style: normal; line-height: 17px; vertical-align: 1px; }
.src-tag--qq { background: #dbeafe; color: #1d4ed8; }
.src-tag--telegram { background: #e0f2fe; color: #0369a1; }
.conv-sec { margin: 14px 8px 6px; color: var(--color-muted); font-size: 12px; letter-spacing: .04em; }
.conv-sec:first-child { margin-top: 2px; }
.conv-empty { margin: 0 8px; }
.chat-shell { display: flex; flex: 1; min-width: 0; min-height: 0; }
.sidebar {
  display: flex; width: var(--sidebar-width); flex: 0 0 var(--sidebar-width); flex-direction: column; min-height: 0;
  border-right: 1px solid var(--color-divider); background: var(--color-canvas);
}
.sidebar__head {
  display: flex; flex: 0 0 var(--topbar-height); align-items: center; justify-content: space-between; gap: 12px;
  padding: 0 var(--surface-inset-md); border-bottom: 1px solid var(--color-divider);
}
.sidebar__head .eyebrow { margin: 0; }
.sidebar__list { flex: 1; overflow-y: auto; align-content: start; padding: 12px; }
.sidebar__foot { margin: 0; padding: 12px var(--surface-inset-md) 16px; }
.drawer-backdrop { display: none; }
.menu-btn { display: none; margin-left: -8px; }
.topbar__titles { flex: 1; }
.conv-title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.topbar__actions { display: flex; gap: 4px; }
.link { color: var(--color-accent-active); text-decoration: underline; text-underline-offset: 3px; }

.chat { min-height: 0; }
.scroller { flex: 1; overflow-y: auto; }
.thread { max-width: 760px; margin: 0 auto; padding: 24px 32px 12px; }
.more { text-align: center; margin-bottom: 12px; }
.empty { margin-top: 14vh; text-align: center; }
.empty h2 { font-size: 30px; line-height: 1.2; margin-bottom: 10px; }
.empty p { font-size: 16px; line-height: 1.65; margin: 0; }
.day { display: flex; align-items: center; gap: 12px; margin: 22px 0 14px; color: var(--color-muted); font-size: 12px; }
.day::before, .day::after { content: ''; flex: 1; height: 1px; background: var(--color-divider); }
.msg { display: flex; flex-direction: column; margin: 8px 0 18px; }
.msg.is-fresh { animation: msg-in 360ms var(--ease-editorial) both; }
.msg.user.is-fresh { animation-name: msg-in-user; }
@keyframes msg-in { from { opacity: 0; transform: translateY(10px); } }
@keyframes msg-in-user { from { opacity: 0; transform: translateY(10px) scale(.98); transform-origin: right bottom; } }
.thinking__live { animation: breathe 1.6s ease-in-out infinite; }
@keyframes breathe { 50% { opacity: .45; } }
.persona-chip {
  display: inline-flex; align-items: center; gap: 6px; height: 32px; margin-top: 18px; padding: 0 6px 0 12px;
  border: 1px solid var(--color-divider); border-radius: var(--radius-pill); background: var(--color-surface);
  color: var(--color-text-secondary); font-size: 13px; transition: border-color var(--motion-fast) ease, box-shadow var(--motion-fast) ease;
}
.persona-chip:hover { border-color: var(--color-hairline-strong); box-shadow: var(--shadow-float); color: var(--color-ink); }
.persona-chip__go { padding: 2px 8px; border-radius: var(--radius-pill); background: var(--color-accent-tint); color: var(--color-accent-active); font-size: 12px; }
.jump-wrap { position: relative; height: 0; }
.jump {
  position: absolute; left: 50%; bottom: 10px; z-index: 6; display: inline-flex; align-items: center; gap: 4px; height: 32px;
  padding: 0 12px 0 8px; margin-left: -54px; border: 1px solid var(--color-divider); border-radius: var(--radius-pill);
  background: var(--color-surface); box-shadow: var(--shadow-float); color: var(--color-text-secondary); font-size: 13px;
}
.jump:hover { color: var(--color-ink); border-color: var(--color-hairline-strong); }
.jump__icon { transform: rotate(90deg); }
.quota-note {
  display: flex; align-items: center; gap: 6px; max-width: 960px; margin: 0 auto 8px; padding: 8px 12px;
  border: 1px solid rgba(181, 51, 51, .25); border-radius: var(--radius-md); background: var(--color-danger-tint);
  color: var(--color-danger); font-size: 13px;
}
.msg.user { align-items: flex-end; }
.msg.user :deep(.att-list) { margin-bottom: 6px; }
.bubble {
  max-width: min(560px, 86%); padding: 9px 14px; border-radius: 16px 16px 4px 16px; background: var(--color-accent-tint);
  color: var(--color-ink); font-size: 16px; line-height: 1.6; white-space: pre-wrap; word-break: break-word;
}
/* 助手的回复像微信一样拆成几条气泡，一条条冒出来 */
.bubbles { display: grid; gap: 6px; justify-items: start; max-width: min(640px, 92%); }
.bubbles:empty { display: none; }
.bubble--bot {
  max-width: 100%; min-width: 0; border: 1px solid var(--color-divider); border-radius: 4px 16px 16px 16px; background: var(--color-surface);
  white-space: normal;
}
.bubble--bot + .bubble--bot { border-top-left-radius: 16px; }
.bubble--bot :deep(pre) { max-width: 100%; }
.bubble--bot :deep(.math-display) { max-width: 100%; }
.is-fresh .bubble--bot { animation: bubble-in 320ms var(--ease-editorial) both; transform-origin: left top; }
@keyframes bubble-in { from { opacity: 0; transform: translateY(6px) scale(.96); } }
.thinking { max-width: 640px; margin-bottom: 8px; }
.thinking__toggle {
  display: inline-flex; align-items: center; gap: 4px; min-height: 28px; padding: 0 8px 0 4px; margin-left: -4px;
  border-radius: var(--radius-sm); color: var(--color-muted); font-size: 13px;
}
.thinking__toggle:hover { background: var(--color-hover-wash); color: var(--color-text-secondary); }
.chev { transition: transform var(--motion-fast) var(--ease-editorial); }
.chev.is-open { transform: rotate(90deg); }
.thinking__body {
  margin: 6px 0 4px 2px; padding: 2px 0 2px 14px; border-left: 2px solid var(--color-divider);
  color: var(--color-text-secondary); font-size: 14px; line-height: 1.7; white-space: pre-wrap; word-break: break-word;
}
.meta { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin-top: 6px; color: var(--color-muted); font-size: 12px; }
.meta .err { color: var(--color-danger); }
.meta .diag { margin-left: 4px; }
.recall-toggle { cursor: pointer; height: 24px; font-size: 12px; margin-left: 4px; }
.recalled {
  display: grid; gap: 8px; max-width: 640px; margin: 10px 0 0; padding: 12px 14px; list-style: none;
  border-radius: var(--radius-md); background: var(--color-canvas-soft); color: var(--color-text-secondary); font-size: 14px; line-height: 1.45;
}
.recalled li { display: flex; align-items: baseline; gap: 8px; }
.recalled .badge-pill { flex-shrink: 0; background: var(--color-surface); }
.composer { padding: 10px 32px calc(14px + env(safe-area-inset-bottom)); }
.composer__box {
  display: flex; flex-direction: column; max-width: 960px; margin: 0 auto; padding: 10px 10px 8px 14px;
  border: 1px solid var(--color-hairline-strong); border-radius: var(--radius-xl); background: var(--color-surface);
  transition: border-color .18s ease, box-shadow .18s ease;
}
.composer__box:focus-within { border-color: var(--color-accent); box-shadow: 0 0 0 3px rgba(91, 122, 74, .14); }
.composer__input {
  width: 100%; min-height: 44px; max-height: 220px; padding: 4px 4px 6px 0; border: 0; outline: 0; resize: none;
  background: transparent; color: var(--color-ink); font-size: 15px; line-height: 1.6;
}
.composer__input::placeholder { color: var(--color-tertiary); }
.composer__bar { display: flex; align-items: center; gap: 6px; margin-left: -6px; }
.composer__bar .btn { min-height: 34px; padding: 0 16px; border-radius: var(--radius-rail); }
.spacer { flex: 1; }
.picker { position: relative; min-width: 0; }
.picker__chip {
  display: inline-flex; max-width: 280px; height: 34px; align-items: center; gap: 4px; padding: 0 10px;
  border-radius: var(--radius-rail); color: var(--color-text-secondary); font-size: 13px; white-space: nowrap;
}
.picker__chip:hover { background: var(--color-hover-wash); color: var(--color-ink); }
.picker__model { min-width: 0; overflow: hidden; text-overflow: ellipsis; }
.picker__think { flex-shrink: 0; color: var(--color-muted); }
.picker__chev { flex-shrink: 0; transform: rotate(-90deg); }
.picker__panel {
  position: absolute; right: 0; bottom: calc(100% + 8px); z-index: 20; width: 300px; max-width: calc(100vw - 32px);
  padding: 12px; border: 1px solid var(--color-divider); border-radius: var(--radius-lg); background: var(--color-surface);
  box-shadow: var(--shadow-float); animation: toast-in 180ms var(--ease-editorial);
}
.picker__title { margin: 2px 4px 6px; color: var(--color-muted); font-size: 12px; font-weight: 600; letter-spacing: .06em; }
.picker__list { display: grid; gap: 2px; max-height: 260px; overflow-y: auto; margin-bottom: 10px; }
.picker__opt {
  display: flex; align-items: baseline; justify-content: space-between; gap: 10px; min-height: 38px; padding: 8px 10px;
  border-radius: var(--radius-md); color: var(--color-ink); font-size: 14px; text-align: left;
}
.picker__opt small { flex-shrink: 0; color: var(--color-muted); font-size: 12px; }
.picker__opt:hover { background: var(--color-surface-hover); }
.picker__opt.is-active { background: var(--color-canvas-soft); box-shadow: inset 3px 0 0 var(--color-accent); }
.picker__think-switch { grid-auto-columns: minmax(0, 1fr); }
.composer.is-dragging .composer__box { border-style: dashed; border-color: var(--color-accent); }
.pending { display: flex; flex-wrap: wrap; gap: 8px; max-width: 960px; margin: 0 auto 8px; }
.pending__item {
  position: relative; display: flex; align-items: center; height: 56px; min-width: 56px; max-width: 220px;
  border: 1px solid var(--color-divider); border-radius: var(--radius-md); background: var(--color-surface); overflow: hidden;
}
.pending__item.is-error { border-color: rgba(181, 51, 51, .4); }
.pending__thumb { width: 56px; height: 56px; object-fit: cover; }
.pending__file { display: flex; align-items: center; gap: 6px; min-width: 0; padding: 0 28px 0 10px; color: var(--color-text-secondary); font-size: 13px; }
.pending__file span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pending__state {
  position: absolute; left: 0; right: 0; bottom: 0; padding: 1px 4px; background: rgba(20, 20, 19, .55); color: #fff;
  font-size: 11px; text-align: center;
}
.is-error .pending__state { background: var(--color-danger); }
.pending__remove {
  position: absolute; top: 3px; right: 3px; display: inline-flex; width: 20px; height: 20px; align-items: center; justify-content: center;
  border-radius: 50%; background: rgba(20, 20, 19, .55); color: #fff;
}

@media (max-width: 960px) {
  .sidebar {
    position: fixed; top: 0; bottom: 0; left: 0; z-index: 30; width: min(var(--sidebar-width), 86vw);
    box-shadow: var(--shadow-modal); transform: translateX(-102%);
    transition: transform var(--motion-page) var(--ease-editorial);
  }
  .sidebar.is-open { transform: none; }
  .drawer-backdrop { display: block; position: fixed; inset: 0; z-index: 25; background: var(--color-backdrop); }
  .menu-btn { display: inline-flex; }
}
@media (max-width: 767px) {
  .thread { padding: 16px var(--mobile-gutter) 8px; }
  .composer { padding: 8px var(--mobile-gutter) 10px; }
  .picker__chip { max-width: 52vw; }
}
</style>
