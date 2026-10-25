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
      r.error = e instanceof ApiError ? e.message : '发送失败'
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
  const s = new Set(openMemories.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  openMemories.value = s
}

onMounted(loadInitial)
</script>

<template>
  <main class="chat">
    <div ref="scroller" class="scroller">
      <div class="inner">
        <div v-if="notReady" class="notice">
          还没有配置聊天模型。请在配置文件中填写 <code>llm.chat.api_key</code> 后重启服务。
        </div>
        <div v-else-if="embeddingMissing" class="notice">
          还没有配置向量模型（<code>llm.embedding.api_key</code>），{{ name }}暂时记不住事情。
        </div>
        <p v-if="loadError" class="error">{{ loadError }}</p>

        <div v-if="hasMore" class="more">
          <button class="btn ghost small" :disabled="loadingMore" @click="loadMore">
            {{ loadingMore ? '加载中…' : '加载更早的消息' }}
          </button>
        </div>

        <div v-if="!items.length && !loadError" class="empty">
          <img src="/favicon.svg" alt="" width="56" height="56" />
          <p>嗨，我是{{ name }}。最近过得怎么样？</p>
        </div>

        <template v-for="row in rows" :key="row.kind === 'day' ? row.key : row.item.id">
          <div v-if="row.kind === 'day'" class="day"><span>{{ row.label }}</span></div>
          <div v-else class="msg" :class="row.item.role">
            <div class="bubble">
              <div v-if="row.item.role === 'assistant'" class="md" v-html="renderMarkdown(row.item.content)" />
              <div v-else class="plain">{{ row.item.content }}</div>
              <span v-if="row.item.streaming && !row.item.content" class="typing"><i /><i /><i /></span>
            </div>
            <div class="meta">
              <span>{{ timeOf(row.item.created_at) }}</span>
              <span v-if="row.item.status === 'interrupted'" class="warn">· 已中断</span>
              <span v-if="row.item.status === 'error'" class="err">· {{ row.item.error || '生成失败' }}</span>
              <button
                v-if="row.item.memories?.length"
                class="linklike"
                @click="toggleMemories(row.item.id)"
              >
                · 想起了 {{ row.item.memories.length }} 件事
              </button>
            </div>
            <ul v-if="row.item.memories?.length && openMemories.has(row.item.id)" class="recalled">
              <li v-for="m in row.item.memories" :key="m.id">
                <span class="tag" :class="m.source">{{ m.source === 'stated' ? '你说过' : '推测' }}</span>
                {{ m.content }}
                <span class="muted score">{{ m.score.total.toFixed(2) }}</span>
              </li>
            </ul>
          </div>
        </template>
      </div>
    </div>

    <form class="composer" @submit.prevent="send">
      <textarea
        ref="textarea"
        v-model="input"
        class="input"
        rows="1"
        :placeholder="`和${name}说点什么…`"
        title="Enter 发送，Shift+Enter 换行"
        maxlength="4000"
        @input="autoGrow"
        @keydown="onKeydown"
      />
      <button v-if="sending" type="button" class="btn ghost" @click="stop">停止</button>
      <button v-else class="btn" :disabled="!input.trim()">发送</button>
    </form>
  </main>
</template>

<style scoped>
.chat { flex: 1; display: flex; flex-direction: column; min-height: 0; }
.scroller { flex: 1; overflow-y: auto; }
.inner { padding: 16px 16px 8px; }
.more { text-align: center; margin-bottom: 12px; }
.empty { text-align: center; color: var(--muted); margin-top: 18vh; }
.day { display: flex; align-items: center; gap: 10px; margin: 18px 0 10px; color: var(--muted); font-size: 12px; }
.day::before, .day::after { content: ''; flex: 1; height: 1px; background: var(--border); }
.msg { display: flex; flex-direction: column; margin: 6px 0 10px; max-width: 86%; }
.msg.user { margin-left: auto; align-items: flex-end; }
.msg.assistant { align-items: flex-start; }
.bubble { padding: 9px 14px; border-radius: 18px; word-break: break-word; }
.user .bubble { background: var(--user-bubble); color: var(--user-text); border-bottom-right-radius: 6px; }
.assistant .bubble { background: var(--bot-bubble); border: 1px solid var(--border); border-bottom-left-radius: 6px; box-shadow: var(--shadow); }
.plain { white-space: pre-wrap; }
.meta { font-size: 12px; color: var(--muted); margin-top: 3px; padding: 0 6px; display: flex; gap: 4px; flex-wrap: wrap; }
.meta .warn { color: #b08a2e; }
.meta .err { color: var(--danger); }
.linklike { border: 0; background: none; padding: 0; color: var(--accent); cursor: pointer; font-size: 12px; }
.recalled { list-style: none; margin: 6px 0 0; padding: 8px 10px; background: var(--surface-2); border-radius: 10px; font-size: 13px; }
.recalled li + li { margin-top: 4px; }
.score { font-size: 11px; margin-left: 4px; }
.typing { display: inline-flex; gap: 4px; padding: 4px 0; }
.typing i { width: 6px; height: 6px; border-radius: 50%; background: var(--muted); animation: blink 1.2s infinite ease-in-out; }
.typing i:nth-child(2) { animation-delay: .2s; }
.typing i:nth-child(3) { animation-delay: .4s; }
@keyframes blink { 0%, 80%, 100% { opacity: .25; } 40% { opacity: 1; } }
.composer {
  display: flex; gap: 8px; align-items: flex-end; padding: 10px 16px calc(12px + env(safe-area-inset-bottom));
  border-top: 1px solid var(--border); background: var(--bg);
}
.composer textarea { resize: none; max-height: 180px; line-height: 1.5; }
.composer .btn { flex-shrink: 0; height: 40px; }
</style>
