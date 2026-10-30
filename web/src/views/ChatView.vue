<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  api,
  ApiError,
  regenerateChat,
  sendChat,
  followChat,
  isLostStream,
  type ChatHandlers,
  type ModelPrice,
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
import ThinkingSlider from '../components/ThinkingSlider.vue'
import ModelIcon from '../components/ModelIcon.vue'
import { shortName, sourceOf } from '../vendors'
import { splitBubbles } from '../bubbles'
import { dayKey, dayLabel, formatSize, renderMarkdown, shortWhen, timeOf } from '../format'
import { memoriesUnlocked } from '../memoryLock'
import Collapse from '../components/Collapse.vue'
import { session } from '../session'
import { recentQuery, wideScreen } from '../sidebarState'
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
  // reconnecting 表示连接断了、回复还在服务器上生成，正在重新连接。
  reconnecting?: boolean
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
// 回复在服务器上后台生成：切到别的对话时只是不再看它（detaching），回来再接上；只有点「停止」才会中断。
let detaching = false
// streamConv 是正在接收回复的对话。
let streamConv = 0
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
// 临时对话不在对话列表里，单独放着；离开它（打开别的对话、关掉页面）时删除。
const tempConv = ref<Conversation | null>(null)
const current = computed(
  () => convs.value.find((c) => c.id === currentId.value) ?? (tempConv.value?.id === currentId.value ? tempConv.value : null),
)
const isTemp = computed(() => !!current.value?.temporary)
const title = computed(() => (isTemp.value ? '临时对话' : current.value?.title || '新对话'))
const defaultModel = computed(() => models.value.find((m) => m.is_default) ?? models.value[0] ?? null)
// 对话实际使用的模型：未选择或已不可用时使用默认模型。
const activeModel = computed(() => {
  const id = current.value?.model_id
  return (id != null && models.value.find((m) => m.id === id)) || defaultModel.value
})
const thinkingOptions = computed<{ v: Thinking; l: string; d?: string }[]>(() => {
  switch (activeModel.value?.thinking_style) {
    case 'deepseek':
      // DeepSeek 只有 low / high / max 三档强度。
      return [
        { v: 'off', l: '关', d: '直接回答，最快' },
        { v: 'low', l: '低', d: '稍微想一想，速度和质量兼顾' },
        { v: 'medium', l: '高', d: '认真推敲，适合难一点的问题' },
        { v: 'high', l: '最高', d: '想得最久最细，回得最慢、用的 token 最多' },
      ]
    case 'model_switch':
      return [
        { v: 'off', l: '不思考', d: '直接回答，最快' },
        { v: 'medium', l: '深度思考', d: '换用推理模型，先想再答' },
      ]
    case 'reasoning_effort':
    case 'enable_thinking':
      return [
        { v: 'off', l: '关', d: '直接回答，最快' },
        { v: 'low', l: '低', d: '稍微想一想，速度和质量兼顾' },
        { v: 'medium', l: '中', d: '认真推敲，适合多数问题' },
        { v: 'high', l: '高', d: '想得最久最细，回得最慢、用的 token 最多' },
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
  modelPrices.value = m.prices ?? null
  pricePeak.value = !!m.peak
  hasModel.value = me.has_model
  embeddingReady.value = me.embedding_ready
}

async function openConversation(id: number) {
  if (tempConv.value && tempConv.value.id !== id) discardTemp()
  detach()
  currentId.value = id
  drawer.value = false
  items.value = []
  hasMore.value = false
  loadError.value = ''
  openMemories.value = new Set()
  openThinking.value = new Set()
  // 临时对话不写进地址，刷新页面后不会再打开它。
  if (tempConv.value?.id === id) router.replace({ query: {} })
  else if (String(route.query.c ?? '') !== String(id)) router.replace({ query: { c: String(id) } })
  try {
    const r = await api.messages(id, 0, 50)
    if (currentId.value !== id) return
    items.value = r.messages
    hasMore.value = r.has_more
    // 先标成「正在生成」再渲染，免得闪一下「已中断」。
    if (r.generating) follow(id)
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
  if (route.query.new) await startNewFromSidebar()
}

// 侧栏的「新对话」：跳到 /?new=… ，这里新建（或复用空的）对话。
async function startNewFromSidebar() {
  await newConversation()
  textarea.value?.focus()
}
watch(
  () => route.query.new,
  (v, old) => {
    if (v && v !== old && convs.value.length) startNewFromSidebar()
  },
)

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
  // 当前对话还是空的就直接用它，避免堆出一串空对话。
  if (current.value && !isTemp.value && !items.value.length) {
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
  if (tempConv.value?.id === c.id) tempConv.value = c
  const i = convs.value.findIndex((x) => x.id === c.id)
  if (i >= 0) convs.value[i] = c
}

// startTemporary 开一段临时对话：不保存、不写记忆、不影响心情；默认也不参考记忆和心情。
async function startTemporary() {
  if (isTemp.value) {
    textarea.value?.focus()
    return
  }
  try {
    const c = await api.createConversation({ temporary: true, use_context: false })
    discardTemp()
    tempConv.value = c
    await openConversation(c.id)
    textarea.value?.focus()
  } catch (e) {
    toast(errText(e, '开临时对话失败'))
  }
}

// discardTemp 删掉临时对话（不等结果，失败了服务器也会定时清理）。
function discardTemp() {
  const t = tempConv.value
  if (!t) return
  tempConv.value = null
  api.discardConversation(t.id)
}

async function endTemporary() {
  if (!isTemp.value || sending.value) return
  if (items.value.length && !confirm('结束后这段临时对话会被删掉，确定吗？')) return
  discardTemp()
  if (convs.value.length) {
    await openConversation(convs.value[0].id)
    return
  }
  try {
    const c = await api.createConversation()
    convs.value = [c]
    await openConversation(c.id)
  } catch (e) {
    toast(errText(e, '新建对话失败'))
  }
}

function setUseContext(on: boolean) {
  patchCurrent({ use_context: on })
}

function onUnload() {
  if (tempConv.value) api.discardConversation(tempConv.value.id)
}

async function patchCurrent(patch: { title?: string; model_id?: number | null; thinking?: Thinking; use_context?: boolean }) {
  if (!current.value) return
  try {
    replaceConv(await api.updateConversation(current.value.id, patch))
  } catch (e) {
    toast(errText(e, '修改失败'))
  }
}

const pickerOpen = ref(false)
const pickerEl = ref<HTMLElement | null>(null)
const pickerSearch = ref<HTMLInputElement | null>(null)
const modelQuery = ref('')
const isMulti = computed(() => session.meta?.mode === 'multi')
// 计费开着时各公共模型现在的价格（元 / 百万 token）；没开计费时为 null。
const modelPrices = ref<Record<string, ModelPrice> | null>(null)
const pricePeak = ref(false)

watch(pickerOpen, async (v) => {
  if (!v) return
  modelQuery.value = ''
  await nextTick()
  // 手机上不自动弹出键盘。
  if (window.matchMedia('(hover: hover)').matches) pickerSearch.value?.focus()
})

// 按搜索词过滤，再按「公共 / 我的」分组。
const modelGroups = computed(() => {
  const q = modelQuery.value.trim().toLowerCase()
  const hit = models.value.filter((m) => !q || m.name.toLowerCase().includes(q) || m.model.toLowerCase().includes(q))
  if (!isMulti.value) return [{ label: '', items: hit }]
  return [
    { label: '公共模型', items: hit.filter((m) => m.scope !== 'personal') },
    { label: '我的模型', items: hit.filter((m) => m.scope === 'personal') },
  ].filter((g) => g.items.length)
})
const modelCount = computed(() => modelGroups.value.reduce((n, g) => n + g.items.length, 0))

function yuanPrice(x?: number) {
  if (!x) return '0'
  return x >= 100 ? x.toFixed(0) : x >= 10 ? x.toFixed(1).replace(/\.0$/, '') : x.toFixed(2).replace(/\.?0+$/, '')
}

// modelPrice 返回模型旁边显示的价格（现在这个时段的）、时段标签和悬停说明。
function modelPrice(m: ModelView): { text: string; title: string; tag?: string } | null {
  if (m.scope === 'personal' && isMulti.value) return { text: '自己的 Key', title: '用你自己的 API Key，不扣余额' }
  if (!modelPrices.value) return null
  const p = modelPrices.value[String(m.id)]
  if (!p || (!p.input && !p.output)) return { text: '免费', title: '这个模型不扣余额' }
  const line = (x: { input?: number; cache_input?: number; output?: number }) =>
    `输入 ¥${yuanPrice(x.input)}，输出 ¥${yuanPrice(x.output)}${x.cache_input ? `，命中缓存 ¥${yuanPrice(x.cache_input)}` : ''}`
  if (p.off_peak) {
    const now = pricePeak.value ? p : p.off_peak
    return {
      text: `¥${yuanPrice(now.input)} / ${yuanPrice(now.output)}`,
      tag: pricePeak.value ? '高峰' : '空闲',
      title: `分时段计价，每百万 token\n高峰：${line(p)}\n空闲：${line(p.off_peak)}\n现在是${pricePeak.value ? '高峰' : '空闲'}时段`,
    }
  }
  return { text: `¥${yuanPrice(p.input)} / ${yuanPrice(p.output)}`, title: `每百万 token：${line(p)}` }
}

function setThinking(v: string) {
  if (v !== thinkingValue.value) patchCurrent({ thinking: v as Thinking })
}

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
  streamConv = convId
  try {
    const final = await sendChat(
      convId,
      content,
      attachments.map((a) => a.id),
      replyHandlers(r, (d) => {
        Object.assign(items.value[items.value.length - 2], d.user_message, { attachments })
        replaceConv(d.conversation)
        for (const p of sentPending) if (p.preview) URL.revokeObjectURL(p.preview)
      }),
      controller.signal,
    )
    Object.assign(r, { ...final, memories: r.memories, tools: final.tools ?? r.tools })
  } catch (e) {
    if (lostOrDetached(e, r, convId)) {
      // 回复还在服务器上生成，回来时接上。
    } else if ((e as Error).name === 'AbortError') {
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
    streamConv = 0
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

// replyHandlers 把流式事件写进回复 r。
function replyHandlers(r: Item, onStart?: NonNullable<ChatHandlers['onStart']>): ChatHandlers {
  return {
    onStart: (d) => {
      r.id = d.assistant_message_id
      onStart?.(d)
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
  }
}

// 最后一条是保存过的回复时可以重新生成。
const lastReply = computed(() => {
  const last = items.value[items.value.length - 1]
  return last && last.role === 'assistant' && last.id > 0 && !last.streaming ? last : null
})

// regenerate 重新生成最后一条回复：原来的回复换成新的。
async function regenerate() {
  const old = lastReply.value
  if (!old || sending.value || !current.value) return
  const convId = current.value.id
  sending.value = true
  const tmp = -Date.now()
  items.value.splice(items.value.length - 1, 1, {
    id: tmp, key: tmp, fresh: true, role: 'assistant', content: '', reasoning: '', tools: [], status: 'complete',
    created_at: new Date().toISOString(), streaming: true,
  })
  const r = items.value[items.value.length - 1]
  await scrollToBottom(true, true)
  controller = new AbortController()
  streamConv = convId
  try {
    const final = await regenerateChat(convId, replyHandlers(r), controller.signal)
    Object.assign(r, { ...final, memories: r.memories, tools: final.tools ?? r.tools })
  } catch (e) {
    if (lostOrDetached(e, r, convId)) {
      // 回复还在服务器上生成，回来时接上。
    } else if ((e as Error).name === 'AbortError') {
      r.status = 'interrupted'
    } else if (r.id < 0) {
      // 还没开始就失败了（例如超出限额），原来的回复还在。
      items.value.splice(items.value.indexOf(r), 1, old)
      toast(errText(e, '重新生成失败'))
    } else {
      r.status = 'error'
      r.error = errText(e, '这次没有回复成功')
    }
  } finally {
    r.streaming = false
    sending.value = false
    controller = null
    streamConv = 0
    scrollToBottom()
    meter.value?.refresh()
  }
}

// removeMessage 删除一条消息。
async function removeMessage(m: Item) {
  if (m.id < 0 || m.streaming || !current.value) return
  if (!confirm(`删除这条${m.role === 'user' ? '消息' : '回复'}？删除后${name.value}之后也看不到它了（已经记住的事不受影响）。`)) return
  try {
    await api.deleteMessage(current.value.id, m.id)
    items.value = items.value.filter((x) => x !== m)
    meter.value?.refresh()
  } catch (e) {
    toast(errText(e, '删除失败'))
  }
}

// stop 停止生成：让服务器中断，已经生成的部分会保存下来并通过流发回来；服务器没响应就直接断开。
async function stop() {
  const id = streamConv
  if (!id) return
  try {
    await api.stopReply(id)
  } catch {
    controller?.abort()
  }
}

// detach 不再看正在生成的回复（切到别的对话时），回复在服务器上照样生成完。
function detach() {
  if (!controller) return
  detaching = true
  controller.abort()
}

// lostOrDetached 处理「连接断了」和「切走了」：回复还在服务器上生成，不算失败。
function lostOrDetached(e: unknown, r: Item, convId: number) {
  if (detaching && (e as Error).name === 'AbortError') {
    detaching = false
    return true
  }
  if (r.id > 0 && isLostStream(e)) {
    r.reconnecting = true
    whenVisible(() => resume(convId))
    return true
  }
  return false
}

// resume 连接断了以后重新读这段对话、接上还在生成的回复；网络没恢复时每隔几秒再试，先显示的内容不清掉。
async function resume(convId: number) {
  for (let i = 0; i < 60; i++) {
    if (currentId.value !== convId || sending.value) return
    try {
      const r = await api.messages(convId, 0, 50)
      if (currentId.value !== convId || sending.value) return
      items.value = r.messages
      hasMore.value = r.has_more
      if (r.generating) follow(convId)
      await scrollToBottom(true)
      return
    } catch {
      await new Promise((ok) => setTimeout(ok, document.visibilityState === 'visible' ? 2000 : 5000))
    }
  }
}

// whenVisible 在页面回到前台后执行（手机切回来时网络可能还没恢复，稍等一下）。
function whenVisible(fn: () => void) {
  if (document.visibilityState === 'visible') {
    setTimeout(fn, 800)
    return
  }
  const on = () => {
    if (document.visibilityState !== 'visible') return
    document.removeEventListener('visibilitychange', on)
    setTimeout(fn, 800)
  }
  document.addEventListener('visibilitychange', on)
}

// follow 接上这段对话正在后台生成的回复：先显示已经生成的部分，再接着显示。
async function follow(convId: number) {
  if (sending.value || currentId.value !== convId) return
  const r = [...items.value].reverse().find((m) => m.role === 'assistant')
  if (!r) return
  Object.assign(r, { content: '', reasoning: '', tools: [], status: 'complete', error: undefined, streaming: true })
  sending.value = true
  controller = new AbortController()
  streamConv = convId
  try {
    const final = await followChat(convId, replyHandlers(r), controller.signal)
    if (final) Object.assign(r, { ...final, memories: r.memories, tools: final.tools ?? r.tools })
    else {
      // 刚好已经生成完了：重新读一遍。
      r.streaming = false
      sending.value = false
      controller = null
      streamConv = 0
      if (currentId.value === convId) await openConversation(convId)
      return
    }
  } catch (e) {
    if (!lostOrDetached(e, r, convId)) {
      if ((e as Error).name === 'AbortError') r.status = 'interrupted'
      else {
        r.status = 'error'
        r.error = errText(e, '这次没有回复成功')
      }
    }
  } finally {
    r.streaming = false
    sending.value = false
    controller = null
    streamConv = 0
    scrollToBottom()
    meter.value?.refresh()
  }
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
// 侧栏「搜索」里输入的词筛选对话。
const shownConvs = computed(() => {
  const q = recentQuery.value.trim().toLowerCase()
  return q ? convs.value.filter((c) => (c.title || '新对话').toLowerCase().includes(q)) : convs.value
})
const convSections = computed(() => [
  { key: 'web', label: '普通聊天', items: shownConvs.value.filter((c) => !c.channel) },
  { key: 'ext', label: '外部聊天', items: shownConvs.value.filter((c) => !!c.channel) },
])

// 空对话时的问候（参考 claude.ai：问候和输入框一起放在页面中间）。
const greeting = computed(() => {
  const h = new Date().getHours()
  const part = h < 5 ? '夜深了' : h < 11 ? '早上好' : h < 13 ? '中午好' : h < 18 ? '下午好' : '晚上好'
  const who = session.user?.display_name || session.user?.username || ''
  return who ? `${part}，${who}` : part
})
const isEmpty = computed(() => !items.value.length && !loadError.value && !!current.value)

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
  window.addEventListener('pagehide', onUnload)
})
onBeforeUnmount(() => {
  clearInterval(pollTimer)
  document.removeEventListener('visibilitychange', onVisible)
  window.removeEventListener('focus', checkNew)
  window.removeEventListener('pagehide', onUnload)
  // 去了别的页面：临时对话也就结束了。
  discardTemp()
})
</script>

<template>
  <div class="chat-shell">
    <Transition name="fade">
      <div v-if="drawer" class="drawer-backdrop" @click="drawer = false" />
    </Transition>
    <aside class="sidebar" :class="{ 'is-open': drawer, 'is-merged': wideScreen }" aria-label="对话列表">
      <div class="sidebar__head">
        <span class="eyebrow">对话</span>
        <button class="btn" @click="newConversation">
          <Icon name="plus" :size="16" />新对话
        </button>
      </div>
      <!-- 宽屏时对话列表放进左侧合并的侧栏（App.vue 的 #app-recents），和 claude.ai 一样 -->
      <Teleport to="#app-recents" :disabled="!wideScreen" defer>
      <nav class="sidebar__list" :class="{ 'is-side': wideScreen }" aria-label="对话">
        <template v-for="sec in convSections" :key="sec.key">
          <p v-if="sec.items.length || sec.key === 'web'" class="conv-sec">{{ sec.label }}</p>
          <TransitionGroup tag="div" name="list" class="category-filter-list">
            <button
              v-for="c in sec.items"
              :key="c.id"
              class="category-filter"
              :class="{ 'is-active': c.id === currentId }"
              @click="openConversation(c.id)"
            >
              <span><i v-if="c.channel" class="src-tag" :class="`src-tag--${c.channel}`">{{ channelLabels[c.channel] }}</i>{{ c.title || (c.channel ? `${channelLabels[c.channel]}里的聊天` : '新对话') }}</span>
              <small>{{ shortWhen(c.last_message_at ?? c.created_at) }}</small>
            </button>
          </TransitionGroup>
          <p v-if="sec.key === 'web' && !sec.items.length" class="caption conv-empty">{{ recentQuery ? '没有找到' : '还没有网页上的对话' }}</p>
        </template>
      </nav>
      </Teleport>
      <p class="caption sidebar__foot">每段对话各自独立，但{{ name }}记得的事在所有对话里都通用。</p>
    </aside>

    <main class="main chat" :class="{ 'is-empty': isEmpty }">
      <header class="topbar">
        <button class="icon-btn menu-btn" aria-label="对话列表" @click="drawer = true"><Icon name="menu" /></button>
        <div class="topbar__titles">
          <h1 v-if="!isEmpty || isTemp" class="conv-title">{{ title }}</h1>
        </div>
        <div v-if="current" class="topbar__actions">
          <span v-show="!isEmpty" class="meter-wrap"><ContextMeter ref="meter" :conversation-id="currentId" :busy="sending" @status="(s) => (compactPending = !!s?.pending)" /></span>
          <template v-if="isTemp">
            <button class="btn btn-sm temp-end" :disabled="sending" title="结束并删除这段临时对话" @click="endTemporary">结束<span class="hide-sm">临时对话</span></button>
          </template>
          <template v-else>
            <button
              class="icon-btn"
              aria-label="开临时对话"
              title="临时对话：不保存聊天记录，不写进记忆、不影响心情"
              @click="startTemporary"
            >
              <Icon name="ghost" :size="18" />
            </button>
            <template v-if="!isEmpty">
              <button class="icon-btn" aria-label="重命名对话" title="重命名" @click="startRename"><Icon name="edit" :size="18" /></button>
              <button class="icon-btn" aria-label="删除对话" title="删除" :disabled="sending" @click="deleting = true"><Icon name="trash" :size="18" /></button>
            </template>
          </template>
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
            <div v-if="!items.length && !loadError && isTemp" key="temp" class="empty empty--temp">
              <span class="empty__icon"><Icon name="ghost" :size="26" /></span>
              <h2>临时对话</h2>
              <p class="muted">这里聊的不会保存，不会写进{{ name }}的记忆，也不会影响它的心情。点「结束」或离开这个页面就删掉。</p>
            </div>
            <div v-else-if="!items.length && !loadError && current" key="normal" class="empty">
              <h2 class="greet"><img src="/logo.png" alt="" class="greet__logo" />{{ greeting }}</h2>
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
                <span v-if="row.item.reconnecting" class="reconnecting">· 连接断了，回复还在生成，正在重新连接…</span>
                <span v-else-if="row.item.status === 'interrupted'">· 已停止</span>
                <span v-if="row.item.status === 'error'" class="err">· {{ row.item.error || '这次没有回复成功' }}</span>
                <button v-if="row.item.status === 'error'" type="button" class="chip-note diag" @click="copyDiag(row.item)">复制诊断信息</button>
                <span v-if="row.item.id > 0 && !row.item.streaming" class="msg-actions">
                  <button
                    v-if="row.item === lastReply"
                    type="button"
                    class="msg-action"
                    :disabled="sending"
                    title="重新生成这条回复"
                    aria-label="重新生成这条回复"
                    @click="regenerate"
                  >
                    <Icon name="refresh" :size="14" />
                  </button>
                  <button type="button" class="msg-action" :disabled="sending" title="删除" :aria-label="row.item.role === 'user' ? '删除这条消息' : '删除这条回复'" @click="removeMessage(row.item)">
                    <Icon name="trash" :size="14" />
                  </button>
                </span>
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
        <Transition name="fade">
          <div v-if="isTemp" class="temp-bar">
            <span class="temp-bar__label"><Icon name="ghost" :size="15" />临时对话 · 不保存、不写记忆</span>
            <label class="temp-switch" :title="`打开后${name}会参考已有的记忆和现在的心情来聊，但不会改变它们`">
              <input type="checkbox" :checked="!!current?.use_context" :disabled="sending" @change="setUseContext(($event.target as HTMLInputElement).checked)" />
              <span class="temp-switch__track" aria-hidden="true"><span /></span>
              <span>参考记忆和心情</span>
            </label>
          </div>
        </Transition>
        <div class="composer__box" :class="{ 'is-temp': isTemp }">
          <textarea
            ref="textarea"
            v-model="input"
            class="composer__input"
            rows="1"
            :placeholder="isTemp ? '临时对话，聊完就删' : `和${name}说点什么`"
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
                <ModelIcon v-if="activeModel" :model="activeModel.model" :name="activeModel.name" :size="15" />
                <span class="picker__model">{{ activeModel ? shortName(activeModel.name) : '默认模型' }}</span>
                <span v-if="thinkingLabel" class="picker__think">· {{ thinkingLabel }}</span>
                <Icon name="chevron" :size="12" class="picker__chev" />
              </button>
              <Transition name="pop">
              <div v-if="pickerOpen" class="picker__panel" role="dialog" aria-label="模型与思考等级">
                <div class="picker__search">
                  <Icon name="search" :size="15" />
                  <input ref="pickerSearch" v-model="modelQuery" type="search" placeholder="搜索模型" aria-label="搜索模型" />
                </div>
                <div class="picker__list">
                  <button
                    v-if="!modelQuery.trim()"
                    type="button"
                    class="picker__opt"
                    :class="{ 'is-active': current?.model_id == null }"
                    @click="chooseModel(null)"
                  >
                    <span class="picker__opt-icon picker__opt-auto"><Icon name="sparkle" :size="14" /></span>
                    <span class="picker__opt-main">
                      <span class="picker__opt-name">默认模型</span>
                      <Icon v-if="current?.model_id == null" name="check" :size="15" class="picker__check" />
                    </span>
                    <small v-if="defaultModel">跟随设置里的默认：{{ shortName(defaultModel.name) }}</small>
                  </button>
                  <template v-for="g in modelGroups" :key="g.label">
                    <p v-if="g.label" class="picker__group">{{ g.label }}</p>
                    <button
                      v-for="m in g.items"
                      :key="m.id"
                      type="button"
                      class="picker__opt"
                      :class="{ 'is-active': current?.model_id === m.id }"
                      @click="chooseModel(m.id)"
                    >
                      <ModelIcon :model="m.model" :name="m.name" :size="20" class="picker__opt-icon" />
                      <span class="picker__opt-main">
                        <span class="picker__opt-name" :title="m.name">{{ shortName(m.name) }}</span>
                        <span v-if="sourceOf(m.base_url, m.name)" class="picker__src">{{ sourceOf(m.base_url, m.name) }}</span>
                        <Icon v-if="current?.model_id === m.id" name="check" :size="15" class="picker__check" />
                      </span>
                      <small class="picker__opt-meta">
                        <span>{{ modelTraits(m) }}</span>
                        <span v-if="modelPrice(m)" class="picker__price" :title="modelPrice(m)!.title">
                          <span v-if="modelPrice(m)!.tag" class="picker__tag" :class="{ 'is-peak': modelPrice(m)!.tag === '高峰' }">{{ modelPrice(m)!.tag }}</span>
                          {{ modelPrice(m)!.text }}
                        </span>
                      </small>
                    </button>
                  </template>
                  <p v-if="!modelCount && modelQuery.trim()" class="picker__empty">没有叫「{{ modelQuery.trim() }}」的模型</p>
                </div>
                <p v-if="modelPrices" class="picker__note">价格是每百万 token 的输入 / 输出（有免费或套餐额度时先用额度）</p>
                <div v-if="thinkingOptions.length" class="picker__think-box">
                  <div class="picker__think-head">
                    <span>思考</span>
                    <strong>{{ thinkingLabel }}</strong>
                  </div>
                  <ThinkingSlider :stops="thinkingOptions" :model-value="thinkingValue" @update:model-value="setThinking" />
                </div>
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
/* 空对话：问候和输入框一起放在页面中间（参考 claude.ai） */
.chat.is-empty .scroller { display: flex; flex: 1 1 0; flex-direction: column; justify-content: flex-end; }
.chat.is-empty .thread { width: 100%; padding-bottom: 20px; }
.chat.is-empty .composer { flex: 1 1 0; }
.chat.is-empty .empty { margin-top: 0; }
.meter-wrap { display: inline-flex; align-items: center; }
.chat.is-empty .composer__input { min-height: 72px; }
.greet {
  display: flex; align-items: center; justify-content: center; gap: 12px; margin: 0 0 6px;
  font-family: var(--font-display); font-size: clamp(28px, 3.4vw, 38px); font-weight: 500; letter-spacing: -.015em; line-height: 1.2;
}
.greet__logo { width: 1.05em; height: 1.05em; object-fit: contain; }
/* 放进左侧合并侧栏的对话列表：和 claude.ai 的「最近」一样，只显示标题 */
.sidebar.is-merged { display: none; }
.sidebar__list.is-side { flex: none; overflow: visible; padding: 0; }
.is-side .category-filter-list { gap: 1px; }
.is-side .category-filter { min-height: 36px; padding: 0 10px; color: var(--color-ink); font-size: 14.5px; }
.is-side .category-filter::before, .is-side .category-filter small { display: none; }
.is-side .category-filter > span { padding-left: 0; }
.is-side .category-filter:hover { background: var(--color-hover-wash); }
.is-side .category-filter.is-active { background: rgba(20, 20, 19, .075); }
.is-side .conv-sec { margin: 12px 10px 4px; font-size: 12px; }
.is-side .conv-sec:first-child { display: none; }
.is-side .conv-empty { margin: 4px 10px; }
.scroller { flex: 1; overflow-y: auto; }
.thread { max-width: 760px; margin: 0 auto; padding: 24px 32px 12px; }
.more { text-align: center; margin-bottom: 12px; }
.empty { margin-top: 14vh; text-align: center; }
.empty h2 { font-size: clamp(30px, 4vw, 40px); line-height: 1.15; letter-spacing: -.015em; margin-bottom: 12px; }
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
  display: inline-flex; align-items: center; gap: 6px; height: 30px; margin-top: 10px; padding: 0 6px 0 12px;
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
  display: flex; align-items: center; gap: 6px; max-width: 760px; margin: 0 auto 8px; padding: 8px 12px;
  border: 1px solid rgba(181, 51, 51, .25); border-radius: var(--radius-md); background: var(--color-danger-tint);
  color: var(--color-danger); font-size: 13px;
}
.msg.user { align-items: flex-end; }
.msg.user :deep(.att-list) { margin-bottom: 6px; }
.bubble {
  max-width: min(560px, 86%); padding: 10px 15px; border-radius: 18px 18px 4px 18px; background: var(--color-accent-tint);
  color: var(--color-ink); font-size: 16px; line-height: 1.6; white-space: pre-wrap; word-break: break-word;
}
/* 助手的回复像微信一样拆成几条气泡，一条条冒出来 */
.bubbles { display: grid; gap: 6px; justify-items: start; max-width: min(640px, 92%); }
.bubbles:empty { display: none; }
.bubble--bot {
  max-width: 100%; min-width: 0; border-radius: 4px 18px 18px 18px; background: var(--color-card);
  white-space: normal;
}
.bubble--bot + .bubble--bot { border-top-left-radius: 18px; }
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
.msg-actions { display: inline-flex; gap: 2px; opacity: 0; transition: opacity .15s ease; }
.msg:hover .msg-actions, .msg:focus-within .msg-actions { opacity: 1; }
@media (hover: none) { .msg-actions { opacity: .7; } }
.msg-action {
  display: inline-grid; place-items: center; width: 24px; height: 24px; padding: 0; border: 0; border-radius: 6px;
  background: none; color: var(--color-muted); cursor: pointer; transition: background-color .15s ease, color .15s ease;
}
.msg-action:hover:not(:disabled) { background: var(--color-surface-soft, rgba(0, 0, 0, .05)); color: var(--color-ink); }
.msg-action:disabled { opacity: .4; cursor: default; }
.recall-toggle { cursor: pointer; height: 24px; font-size: 12px; margin-left: 4px; }
.recalled {
  display: grid; gap: 8px; max-width: 640px; margin: 10px 0 0; padding: 12px 14px; list-style: none;
  border-radius: var(--radius-md); background: var(--color-canvas-soft); color: var(--color-text-secondary); font-size: 14px; line-height: 1.45;
}
.recalled li { display: flex; align-items: baseline; gap: 8px; }
.recalled .badge-pill { flex-shrink: 0; background: var(--color-surface); }
.composer { padding: 10px 32px calc(14px + env(safe-area-inset-bottom)); }
.composer__box {
  display: flex; flex-direction: column; max-width: 760px; margin: 0 auto; padding: 14px 12px 10px 18px;
  border: 1px solid var(--color-divider); border-radius: var(--radius-card); background: var(--color-surface);
  box-shadow: 0 4px 20px rgba(20, 20, 19, .04); transition: border-color .18s ease, box-shadow .18s ease;
}
.composer__box:focus-within { border-color: var(--color-hairline-strong); box-shadow: 0 6px 28px rgba(20, 20, 19, .08); }
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
  position: absolute; right: 0; bottom: calc(100% + 8px); z-index: 20; width: 360px; max-width: calc(100vw - 24px);
  padding: 10px; border: 1px solid var(--color-divider); border-radius: var(--radius-lg); background: var(--color-surface);
  box-shadow: var(--shadow-float); animation: toast-in 180ms var(--ease-editorial);
}
.picker__search {
  display: flex; align-items: center; gap: 8px; height: 38px; margin-bottom: 6px; padding: 0 10px;
  border-radius: var(--radius-md); background: var(--color-canvas-soft); color: var(--color-muted);
}
.picker__search input { flex: 1; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--color-ink); font: inherit; font-size: 14px; }
.picker__search input::-webkit-search-cancel-button { cursor: pointer; }
.picker__list { display: grid; gap: 1px; max-height: min(340px, 48vh); overflow-y: auto; margin: 0 -2px; padding: 0 2px; }
.picker__group { margin: 8px 8px 2px; color: var(--color-muted); font-size: 11px; font-weight: 600; letter-spacing: .06em; }
.picker__opt {
  display: grid; gap: 2px; width: 100%; padding: 7px 10px; border-radius: var(--radius-md); color: var(--color-ink); text-align: left;
  transition: background-color .12s ease;
}
.picker__opt:hover { background: var(--color-surface-hover); }
.picker__opt.is-active { background: var(--color-canvas-soft); }
.picker__opt { grid-template-columns: 20px minmax(0, 1fr); column-gap: 10px; }
.picker__opt > :not(.picker__opt-icon) { grid-column: 2; }
.picker__opt-icon { grid-row: 1 / span 2; grid-column: 1; align-self: center; }
.picker__opt-auto { display: grid; place-items: center; width: 20px; height: 20px; border-radius: 6px; background: var(--color-canvas-soft); color: var(--color-text-secondary); }
.picker__src { flex-shrink: 0; padding: 0 5px; border: 1px solid var(--color-hairline-strong); border-radius: 4px; color: var(--color-muted); font-size: 10px; line-height: 15px; }
.picker__opt-main { display: flex; align-items: center; gap: 8px; min-width: 0; }
.picker__opt-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; }
.picker__check { flex-shrink: 0; color: var(--color-accent); }
.picker__opt small { display: block; color: var(--color-muted); font-size: 12px; line-height: 1.4; }
.picker__opt-meta { display: flex !important; justify-content: space-between; gap: 10px; }
.picker__price { flex-shrink: 0; display: inline-flex; align-items: center; gap: 4px; font-variant-numeric: tabular-nums; color: var(--color-text-secondary); }
.picker__tag { padding: 0 5px; border-radius: 4px; background: var(--color-accent-tint, rgba(91, 122, 74, .12)); color: var(--color-accent-active); font-size: 10px; line-height: 16px; }
.picker__tag.is-peak { background: rgba(181, 120, 40, .12); color: #9a5b12; }
.picker__empty { margin: 12px 8px; color: var(--color-muted); font-size: 13px; }
.picker__note { margin: 6px 8px 0; color: var(--color-muted); font-size: 11px; line-height: 1.5; }
.picker__think-box { margin-top: 8px; padding: 10px 10px 4px; border-top: 1px solid var(--color-divider); }
.picker__think-head { display: flex; justify-content: space-between; align-items: baseline; margin-bottom: 8px; color: var(--color-muted); font-size: 12px; font-weight: 600; letter-spacing: .04em; }
.picker__think-head strong { color: var(--color-ink); font-size: 13px; letter-spacing: 0; }
.reconnecting { animation: breathe 1.6s ease-in-out infinite; }
.temp-end { color: var(--color-text-secondary); }
.empty__icon { display: inline-grid; place-items: center; width: 52px; height: 52px; margin-bottom: 14px; border-radius: 16px; background: var(--color-canvas-soft); color: var(--color-text-secondary); }
.temp-bar {
  display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 6px 12px;
  max-width: 760px; margin: 0 auto 8px; padding: 0 6px; color: var(--color-muted); font-size: 12.5px;
}
.temp-bar__label { display: inline-flex; align-items: center; gap: 6px; }
.temp-switch { display: inline-flex; align-items: center; gap: 8px; color: var(--color-text-secondary); cursor: pointer; user-select: none; }
.temp-switch input { position: absolute; opacity: 0; pointer-events: none; }
.temp-switch__track {
  position: relative; width: 30px; height: 18px; border-radius: 999px; background: var(--color-hairline-strong);
  transition: background-color .2s ease;
}
.temp-switch__track span {
  position: absolute; top: 2px; left: 2px; width: 14px; height: 14px; border-radius: 50%; background: #fff;
  box-shadow: 0 1px 2px rgba(20, 20, 19, .25); transition: transform .28s cubic-bezier(.34, 1.56, .64, 1);
}
.temp-switch input:checked + .temp-switch__track { background: var(--color-accent); }
.temp-switch input:checked + .temp-switch__track span { transform: translateX(12px); }
.temp-switch input:focus-visible + .temp-switch__track { outline: 2px solid var(--color-accent); outline-offset: 2px; }
.temp-switch input:disabled + .temp-switch__track { opacity: .5; }
.composer__box.is-temp { border-style: dashed; background: var(--color-canvas-soft); }
.composer.is-dragging .composer__box { border-style: dashed; border-color: var(--color-accent); }
.pending { display: flex; flex-wrap: wrap; gap: 8px; max-width: 760px; margin: 0 auto 8px; }
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
  /* 手机上面板铺满输入框的宽度，不跟着按钮的位置走。 */
  .composer__box { position: relative; }
  .picker { position: static; }
  .hide-sm { display: none; }
  .picker__panel { left: 0; right: 0; width: auto; max-width: none; }
}
</style>
