<script setup lang="ts">
// 设置 → 牛马：用聊天的方式改设置。和布谷的对话、记忆、情绪完全分开；
// 牛马提出的修改显示成确认卡片，点了「确认」才执行。在这个浏览器登录了管理后台时还能改系统设置。
import { computed, nextTick, onMounted, ref } from 'vue'
import { api, ApiError, type AgentAction, type AgentMessage, type AgentState, type ModelView } from '../api'
import { consoleLogin } from '../consoleToken'
import { renderMarkdown } from '../format'
import Icon from '../Icon.vue'
import Modal from './Modal.vue'
import { session } from '../session'
import { toast } from '../toast'

const state = ref<AgentState | null>(null)
// 多用户版只有管理员账号才显示「登录管理后台」的入口。
const canAdmin = computed(() => session.meta?.mode !== 'multi' || !!session.user?.is_admin)
const models = ref<ModelView[]>([])
const loadError = ref('')
const input = ref('')
const sending = ref(false)
const pendingText = ref('')
const busy = ref(0)
const confirmClear = ref(false)
const scroller = ref<HTMLElement | null>(null)
const showLogin = ref(false)
const password = ref('')
const loggingIn = ref(false)

// login 在这里直接登录管理后台（同一个浏览器的其他标签页、管理后台页面也一起登录了）。
async function login() {
  loggingIn.value = true
  try {
    await consoleLogin(password.value)
    password.value = ''
    showLogin.value = false
    if (state.value) state.value.admin = (await api.agent()).admin
    toast('已登录管理后台，牛马现在可以改系统设置、装技能和插件了')
  } catch (e) {
    toast((e as Error).message)
  } finally {
    loggingIn.value = false
  }
}

const messages = computed(() => state.value?.messages ?? [])
const modelValue = computed(() => (state.value?.model_id == null ? '' : String(state.value.model_id)))
const examples = computed(() => {
  const list = ['我现在用的什么性格和模型？', '把布谷的性格换成毒舌一点的', '暂停我所有的定时任务', '别让布谷主动找我了']
  if (state.value?.admin) list.push('帮我上网找个查天气的 skill 装上', '把每天的消息上限改成 200')
  return list
})

onMounted(async () => {
  try {
    const [s, m] = await Promise.all([api.agent(), api.models()])
    state.value = s
    models.value = m.models.filter((x) => x.tools)
    scrollDown()
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  }
})

function scrollDown() {
  nextTick(() => {
    const el = scroller.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

async function send(text = input.value) {
  text = text.trim()
  if (!text || sending.value || !state.value) return
  sending.value = true
  pendingText.value = text
  input.value = ''
  scrollDown()
  try {
    const reply = await api.agentSend(text)
    state.value.messages.push({ id: -Date.now(), role: 'user', content: text, created_at: new Date().toISOString() }, reply)
  } catch (e) {
    input.value = text
    toast(e instanceof ApiError ? e.message : '发送失败')
  } finally {
    sending.value = false
    pendingText.value = ''
    scrollDown()
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    send()
  }
}

function replace(a: AgentAction) {
  for (const m of messages.value) {
    const i = m.actions?.findIndex((x) => x.id === a.id) ?? -1
    if (i >= 0) m.actions![i] = a
  }
}

// 卡片上开关的当前值（用户可以在确认前改），没动过时用牛马提议的值。
const optionValues = ref<Record<number, boolean>>({})
function optionValue(a: AgentAction) {
  return optionValues.value[a.id] ?? a.option?.value ?? false
}

// 卡片说明的第一行是标题，后面（例如改了哪些代码）用等宽字体显示。
function summaryHead(a: AgentAction) {
  return a.summary.split('\n')[0]
}
function summaryRest(a: AgentAction) {
  return a.summary.split('\n').slice(1).join('\n')
}

async function decide(a: AgentAction, ok: boolean) {
  if (busy.value) return
  busy.value = a.id
  try {
    const opts = a.option ? { [a.option.key]: optionValue(a) } : undefined
    const r = ok ? await api.agentConfirm(a.id, opts) : await api.agentCancel(a.id)
    replace(r)
    if (r.status === 'failed') toast('执行失败：' + (r.result ?? ''))
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  } finally {
    busy.value = 0
  }
}

async function setModel(e: Event) {
  const v = (e.target as HTMLSelectElement).value
  const id = v === '' ? null : Number(v)
  try {
    await api.agentModel(id)
    state.value = await api.agent()
    toast(`牛马换成了「${state.value.model?.name ?? '默认模型'}」`)
  } catch (err) {
    toast(err instanceof ApiError ? err.message : '保存失败')
  }
}

async function clear() {
  confirmClear.value = false
  try {
    await api.agentClear()
    if (state.value) state.value.messages = []
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  }
}

function statusText(a: AgentAction): string {
  switch (a.status) {
    case 'done':
      return '已执行'
    case 'failed':
      return '执行失败'
    case 'canceled':
      return '已取消'
    case 'running':
      return '正在执行'
  }
  return ''
}

function steps(m: AgentMessage): string {
  return (m.tools ?? []).map((t) => t.label).filter((v, i, all) => all.indexOf(v) === i).join(' · ')
}
</script>

<template>
  <section id="agent" class="panel panel--inset agent">
    <header class="head">
      <div class="head__text">
        <h2>牛马</h2>
        <p class="muted intro">
          你的设置小助手。直接说想改什么，它查清楚后列出修改，你点「确认」才会生效。和它的聊天不进布谷的记忆，也不影响布谷的心情。
        </p>
      </div>
      <button v-if="messages.length" type="button" class="btn btn-ghost btn-sm" @click="confirmClear = true">清空</button>
    </header>

    <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
    <template v-else-if="state">
      <div class="bar">
        <label class="bar__model">
          <span class="caption">使用模型</span>
          <select class="text-input select" :value="modelValue" @change="setModel">
            <option value="">跟随默认模型{{ state.model_id == null && state.model ? `（${state.model.name}）` : '' }}</option>
            <option v-for="m in models" :key="m.id" :value="String(m.id)">{{ m.name }}</option>
          </select>
        </label>
        <span v-if="state.admin" class="badge-pill admin" title="这个浏览器登录了管理后台">可改系统设置</span>
        <button v-else-if="!showLogin && canAdmin" type="button" class="link-btn caption" @click="showLogin = true">登录管理后台，让它也能改系统设置、装技能和插件</button>
      </div>
      <form v-if="showLogin && !state.admin" class="login" @submit.prevent="login">
        <input v-model="password" class="text-input" type="password" autocomplete="current-password" placeholder="管理员密码" aria-label="管理员密码" />
        <button class="btn btn-sm btn-primary" :disabled="!password || loggingIn">{{ loggingIn ? '正在验证' : '登录' }}</button>
        <button type="button" class="btn btn-sm btn-ghost" @click="showLogin = false">取消</button>
      </form>
      <p v-if="state.model && !state.model.tools" class="form-flash form-flash--error">
        现在的模型「{{ state.model.name }}」不支持工具调用，牛马用不了。请在上面换一个支持工具的模型。
      </p>

      <div ref="scroller" class="log" :class="{ 'is-empty': !messages.length && !sending }" aria-live="polite">
        <div v-if="!messages.length && !sending" class="empty">
          <p class="caption">可以这样说：</p>
          <div class="examples">
            <button v-for="ex in examples" :key="ex" type="button" class="example" @click="send(ex)">{{ ex }}</button>
          </div>
        </div>
        <template v-for="m in messages" :key="m.id">
          <div v-if="m.role === 'user'" class="msg msg--user">{{ m.content }}</div>
          <div v-else class="msg msg--bot">
            <p v-if="steps(m)" class="steps caption">{{ steps(m) }}</p>
            <div class="md" v-html="renderMarkdown(m.content)" />
            <div v-for="a in m.actions ?? []" :key="a.id" class="action" :class="`is-${a.status}`">
              <p class="action__summary">
                <span v-if="a.admin" class="badge-pill badge-pill--sys">系统</span>
                {{ summaryHead(a) }}
              </p>
              <pre v-if="summaryRest(a)" class="action__detail" :class="{ 'is-code': /^[-+（]/m.test(summaryRest(a)) }">{{ summaryRest(a) }}</pre>
              <label v-if="a.option && a.status === 'pending'" class="check-row action__option">
                <input type="checkbox" :checked="optionValue(a)" @change="optionValues[a.id] = ($event.target as HTMLInputElement).checked" />
                {{ a.option.label }}
              </label>
              <div v-if="a.status === 'pending'" class="action__btns">
                <button type="button" class="btn btn-sm btn-ghost" :disabled="!!busy" @click="decide(a, false)">取消</button>
                <button type="button" class="btn btn-sm btn-primary" :disabled="!!busy" @click="decide(a, true)">
                  {{ busy === a.id ? '正在执行' : '确认' }}
                </button>
              </div>
              <p v-else class="action__state caption">
                <Icon v-if="a.status === 'done'" name="check" :size="13" />
                {{ statusText(a) }}<template v-if="a.result && a.status !== 'canceled'">：{{ a.result }}</template>
              </p>
            </div>
          </div>
        </template>
        <template v-if="sending">
          <div class="msg msg--user">{{ pendingText }}</div>
          <div class="msg msg--bot typing caption">牛马正在干活<span class="dots"><i /><i /><i /></span></div>
        </template>
      </div>

      <form class="compose" @submit.prevent="send()">
        <textarea
          v-model="input"
          class="text-input compose__input"
          rows="1"
          maxlength="4000"
          placeholder="想改什么？例如：把我的昵称改成小明"
          aria-label="给牛马发消息"
          @keydown="onKey"
        />
        <button class="btn btn-primary" :disabled="sending || !input.trim()">发送</button>
      </form>
    </template>
    <div v-else class="skeleton skel" aria-hidden="true" />

    <Modal :open="confirmClear" title="清空和牛马的聊天？" subtitle="聊天记录和还没确认的修改都会删除，已经执行的修改不受影响。" @close="confirmClear = false">
      <div class="modal-actions">
        <button class="btn" @click="confirmClear = false">取消</button>
        <button class="btn btn-danger" @click="clear">清空</button>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
.head { display: flex; align-items: flex-start; gap: 12px; }
.head__text { flex: 1; min-width: 0; }
.intro { margin: 0 0 12px; }
.skel { height: 240px; border-radius: var(--radius-md); }
.bar { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 14px; margin-bottom: 12px; }
.bar__model { display: flex; align-items: center; gap: 8px; min-width: 0; }
.link-btn { padding: 0; color: var(--color-accent-active); text-align: left; text-decoration: underline; text-underline-offset: 3px; }
.login { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin: -4px 0 12px; }
.login .text-input { width: 200px; min-height: 34px; }
.select { width: auto; max-width: 260px; min-height: 34px; padding-top: 0; padding-bottom: 0; }
.admin { background: var(--color-accent-tint, rgba(204, 120, 92, .12)); color: var(--color-accent-active); }
.log {
  display: flex; flex-direction: column; gap: 10px; height: min(52vh, 460px); overflow-y: auto; padding: 14px;
  border: 1px solid var(--color-divider); border-radius: var(--radius-lg); background: var(--color-canvas-soft);
}
.empty { margin: auto 0; }
/* 还没聊的时候不占那么高，只放几个示例 */
.log.is-empty { height: auto; min-height: 0; }
.examples { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.example {
  padding: 6px 10px; border: 1px solid var(--color-divider); border-radius: 999px; background: var(--color-surface);
  color: var(--color-text-secondary); font-size: 13px; text-align: left; overflow-wrap: anywhere;
  transition: border-color var(--motion-fast) ease, color var(--motion-fast) ease;
}
.example:hover { border-color: var(--color-hairline-strong); color: var(--color-ink); }
.msg { max-width: 88%; padding: 9px 12px; border-radius: var(--radius-lg); font-size: 14px; line-height: 1.6; overflow-wrap: anywhere; }
.msg--user { align-self: flex-end; background: var(--color-ink); color: #fff; white-space: pre-wrap; }
.msg--bot { align-self: flex-start; border: 1px solid var(--color-divider); background: var(--color-surface); }
.md :deep(p) { margin: 0 0 6px; }
.md :deep(p:last-child) { margin-bottom: 0; }
.md :deep(ul), .md :deep(ol) { margin: 4px 0; padding-left: 20px; }
.steps { margin: 0 0 4px; }
.action { margin-top: 10px; padding: 10px 12px; border: 1px solid var(--color-hairline-strong); border-radius: var(--radius-md); background: var(--color-canvas-soft); }
.action.is-done { border-color: #cfdcc4; }
.action.is-failed { border-color: rgba(181, 51, 51, .3); }
.action.is-canceled { opacity: .6; }
.action__summary { margin: 0; white-space: pre-wrap; font-size: 13px; }
.action__detail { margin: 6px 0 0; white-space: pre-wrap; overflow-wrap: anywhere; font-family: inherit; font-size: 13px; color: var(--color-text-secondary); }
.action__detail.is-code { max-height: 240px; overflow: auto; padding: 8px; border-radius: var(--radius-sm, 6px); background: var(--color-surface); font-family: var(--font-mono); font-size: 12px; }
.action__option { margin-top: 8px; font-size: 13px; }
.badge-pill--sys { margin-right: 4px; background: var(--color-warning); color: #fff; }
.action__btns { display: flex; justify-content: flex-end; gap: 6px; margin-top: 8px; }
.action__state { display: flex; align-items: center; gap: 4px; margin: 6px 0 0; }
.is-done .action__state { color: var(--color-success); }
.is-failed .action__state { color: var(--color-danger); }
.typing { display: flex; align-items: center; gap: 6px; }
.dots { display: inline-flex; gap: 3px; }
.dots i { width: 4px; height: 4px; border-radius: 50%; background: currentColor; animation: blink 1.2s infinite ease-in-out; }
.dots i:nth-child(2) { animation-delay: .2s; }
.dots i:nth-child(3) { animation-delay: .4s; }
@keyframes blink { 0%, 80%, 100% { opacity: .25; } 40% { opacity: 1; } }
.compose { display: flex; align-items: flex-end; gap: 8px; margin-top: 10px; }
.compose__input { flex: 1; min-height: 40px; max-height: 160px; resize: vertical; line-height: 1.5; }
@media (max-width: 600px) {
  .msg { max-width: 94%; }
  .link-btn { padding: 0; color: var(--color-accent-active); text-align: left; text-decoration: underline; text-underline-offset: 3px; }
.login { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin: -4px 0 12px; }
.login .text-input { width: 200px; min-height: 34px; }
.select { max-width: 180px; }
}
</style>
