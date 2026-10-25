<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { api, ApiError, sendChat, type Message, type UsedMemory } from '../api'
import { dayKey, dayLabel, renderMarkdown, timeOf } from '../format'
import { session } from '../session'

interface Item extends Message {
  memories?: UsedMemory[]
  streaming?: boolean
  error?: string
}

const items = ref<Item[]>([])
const hasMore = ref(false)
const loadingMore = ref(false)
const loadError = ref('')
const input = ref('')
const sending = ref(false)
const scroller = ref<HTMLElement | null>(null)
const textarea = ref<HTMLTextAreaElement | null>(null)
const openMemories = ref<Set<number>>(new Set())
let controller: AbortController | null = null

const name = computed(() => session.meta?.name ?? '布谷')
const notReady = computed(() => session.meta && !session.meta.llm_ready)
const embeddingMissing = computed(() => session.meta && session.meta.llm_ready && !session.meta.embedding_ready)

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

function nearBottom(): boolean {
  const el = scroller.value
  return !el || el.scrollHeight - el.scrollTop - el.clientHeight < 120
}

async function scrollToBottom(force = false) {
  if (!force && !nearBottom()) return
  await nextTick()
  const el = scroller.value
  if (el) el.scrollTop = el.scrollHeight
}

async function loadInitial() {
  try {
    const r = await api.messages(0, 50)
    items.value = r.messages
    hasMore.value = r.has_more
    await scrollToBottom(true)
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function loadMore() {
  if (!items.value.length || loadingMore.value) return
  loadingMore.value = true
  const el = scroller.value
  const prevHeight = el?.scrollHeight ?? 0
  try {
    const r = await api.messages(items.value[0].id, 50)
    items.value = [...r.messages, ...items.value]
    hasMore.value = r.has_more
    await nextTick()
    if (el) el.scrollTop = el.scrollHeight - prevHeight
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  } finally {
    loadingMore.value = false
  }
}

function autoGrow() {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${Math.min(el.scrollHeight, 180)}px`
}

async function send() {
  const content = input.value.trim()
  if (!content || sending.value) return
  sending.value = true
  input.value = ''
  await nextTick()
  autoGrow()

  const now = new Date().toISOString()
  const userItem: Item = { id: -Date.now(), role: 'user', content, status: 'complete', created_at: now }
  const reply: Item = { id: -Date.now() - 1, role: 'assistant', content: '', status: 'complete', created_at: now, streaming: true }
  items.value.push(userItem, reply)
  const r = items.value[items.value.length - 1]
  await scrollToBottom(true)

  controller = new AbortController()
  try {
    const final = await sendChat(
      content,
      {
        onStart: (d) => {
          Object.assign(items.value[items.value.length - 2], d.user_message)
          r.id = d.assistant_message_id
        },
        onMemories: (m) => (r.memories = m),
        onDelta: (t) => {
          r.content += t
          scrollToBottom()
        },
      },
      controller.signal,
    )
    Object.assign(r, { ...final, memories: r.memories })
  } catch (e) {
    if ((e as Error).name === 'AbortError') {
      r.status = 'interrupted'
    } else {
      r.status = 'error'
      r.error = e instanceof ApiError ? e.message : '这次没有发送成功'
      // 请求在开始推送前就失败（例如超出限额），用户消息并未保存，放回输入框。
      if (r.id < 0) {
        items.value.splice(items.value.length - 2, 1)
        input.value = content
      }
    }
  } finally {
    r.streaming = false
    sending.value = false
    controller = null
    scrollToBottom()
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

function toggleMemories(id: number) {
  const wasAtBottom = nearBottom()
  const s = new Set(openMemories.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  openMemories.value = s
  if (wasAtBottom) scrollToBottom(true)
}

onMounted(loadInitial)
</script>

<template>
  <main class="main chat">
    <header class="topbar">
      <div class="topbar__titles">
        <span class="eyebrow">CHAT · 聊天</span>
        <h1>{{ name }}</h1>
      </div>
    </header>

    <div ref="scroller" class="scroller">
      <div class="thread">
        <p v-if="notReady" class="form-flash">
          还没有配置聊天模型。在配置文件中填写 <code>llm.chat.api_key</code>，然后重启服务。
        </p>
        <p v-else-if="embeddingMissing" class="form-flash">
          还没有配置向量模型（<code>llm.embedding.api_key</code>），{{ name }}暂时记不住聊过的事。
        </p>
        <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>

        <div v-if="hasMore" class="more">
          <button class="btn btn-ghost" :disabled="loadingMore" @click="loadMore">
            {{ loadingMore ? '正在加载' : '加载更早的消息' }}
          </button>
        </div>

        <div v-if="!items.length && !loadError" class="empty">
          <h2>最近过得怎么样。</h2>
          <p class="muted">随便说点什么。{{ name }}会记住你提到的事，下次自然地接上。</p>
        </div>

        <template v-for="row in rows" :key="row.kind === 'day' ? row.key : row.item.id">
          <div v-if="row.kind === 'day'" class="day"><span>{{ row.label }}</span></div>
          <article v-else class="msg" :class="row.item.role">
            <div v-if="row.item.role === 'assistant'" class="md body" v-html="renderMarkdown(row.item.content)" />
            <div v-else class="bubble">{{ row.item.content }}</div>
            <span v-if="row.item.streaming && !row.item.content" class="typing" aria-label="正在回复"><i /><i /><i /></span>
            <div class="meta">
              <span>{{ timeOf(row.item.created_at) }}</span>
              <span v-if="row.item.status === 'interrupted'">· 已停止</span>
              <span v-if="row.item.status === 'error'" class="err">· {{ row.item.error || '这次没有回复成功' }}</span>
              <button
                v-if="row.item.memories?.length"
                class="chip-note recall-toggle"
                :aria-expanded="openMemories.has(row.item.id)"
                @click="toggleMemories(row.item.id)"
              >
                想起了 {{ row.item.memories.length }} 件事
              </button>
            </div>
            <ul v-if="row.item.memories?.length && openMemories.has(row.item.id)" class="recalled">
              <li v-for="m in row.item.memories" :key="m.id">
                <span class="badge-pill">{{ m.source === 'stated' ? '你说过' : '推测' }}</span>
                <span>{{ m.content }}</span>
              </li>
            </ul>
          </article>
        </template>
      </div>
    </div>

    <form class="composer" @submit.prevent="send">
      <div class="composer__box">
        <textarea
          ref="textarea"
          v-model="input"
          class="textarea"
          rows="1"
          :placeholder="`和${name}说点什么`"
          aria-label="消息"
          maxlength="4000"
          @input="autoGrow"
          @keydown="onKeydown"
        />
        <button v-if="sending" type="button" class="btn" @click="stop">停止</button>
        <button v-else class="btn btn-primary" :disabled="!input.trim()">发送</button>
      </div>
      <p class="caption hint">Enter 发送，Shift+Enter 换行</p>
    </form>
  </main>
</template>

<style scoped>
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
.msg.user { align-items: flex-end; }
.bubble {
  max-width: min(560px, 86%); padding: 10px 14px; border-radius: var(--radius-lg); background: var(--color-canvas-soft);
  color: var(--color-ink); font-size: 16px; line-height: 1.65; white-space: pre-wrap; word-break: break-word;
}
.body { max-width: 640px; color: var(--color-ink); font-size: 16px; line-height: 1.75; word-break: break-word; }
.meta { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin-top: 6px; color: var(--color-muted); font-size: 12px; }
.meta .err { color: var(--color-danger); }
.recall-toggle { cursor: pointer; height: 24px; font-size: 12px; margin-left: 4px; }
.recalled {
  display: grid; gap: 8px; max-width: 640px; margin: 10px 0 0; padding: 12px 14px; list-style: none;
  border-radius: var(--radius-md); background: var(--color-canvas-soft); color: var(--color-text-secondary); font-size: 14px; line-height: 1.45;
}
.recalled li { display: flex; align-items: baseline; gap: 8px; }
.recalled .badge-pill { flex-shrink: 0; background: var(--color-surface); }
.typing { display: inline-flex; gap: 4px; padding: 8px 0 2px; }
.typing i { width: 6px; height: 6px; border-radius: 50%; background: var(--color-muted); animation: blink 1.2s infinite ease-in-out; }
.typing i:nth-child(2) { animation-delay: .2s; }
.typing i:nth-child(3) { animation-delay: .4s; }
@keyframes blink { 0%, 80%, 100% { opacity: .25; } 40% { opacity: 1; } }
.composer { padding: 12px 32px calc(14px + env(safe-area-inset-bottom)); border-top: 1px solid var(--color-divider); }
.composer__box { display: flex; align-items: flex-end; gap: 8px; max-width: 760px; margin: 0 auto; }
.composer .textarea { min-height: 42px; max-height: 180px; resize: none; font-size: 15px; }
.composer .btn { flex-shrink: 0; min-height: 42px; }
.hint { max-width: 760px; margin: 6px auto 0; }
@media (max-width: 767px) {
  .thread { padding: 16px var(--mobile-gutter) 8px; }
  .composer { padding: 10px var(--mobile-gutter) 10px; }
  .hint { display: none; }
}
</style>
