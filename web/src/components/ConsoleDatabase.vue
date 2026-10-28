<script setup lang="ts">
// 管理后台 → 数据库：把 SQLite 一键迁移到 MySQL。填好 MySQL 的地址和账号 → 测试连接 → 开始迁移（网站暂停写入，
// 逐表复制并核对行数）→ 自动切换配置并重启。失败时自动恢复，原来的 SQLite 不受影响。
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ApiError } from '../api'
import { formatSize } from '../format'
import Modal from './Modal.vue'

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

interface Progress { name: string; rows: number; copied: number }
interface MoveStatus {
  stage: 'idle' | 'running' | 'done' | 'failed'
  step: string
  tables: Progress[]
  rows: number
  copied: number
  error?: string
  target?: string
}
interface Info {
  driver: 'sqlite' | 'mysql'
  status: MoveStatus
  supervised: boolean
  restart: string
  blocked?: string
  sqlite_path?: string
  sqlite_bytes?: number
}
interface TestResult { version: string; existing: number; source_rows: number; tables: number }

const info = ref<Info | null>(null)
const loadError = ref('')
const form = reactive({ host: '127.0.0.1', port: 3306, user: 'buugoo', password: '', database: 'buugoo' })
const testing = ref(false)
const tested = ref<TestResult | null>(null)
const testedKey = ref('')
const testError = ref('')
const confirming = ref(false)
const starting = ref(false)
const startError = ref('')
const showTables = ref(false)
// 重启阶段：waiting 等服务停下，back 已经重新启动。
const restart = ref<'' | 'waiting' | 'back' | 'manual'>('')
let timer: ReturnType<typeof setInterval> | undefined

const status = computed(() => info.value?.status)
const pct = computed(() => {
  const s = status.value
  if (!s || !s.rows) return s?.stage === 'done' ? 100 : 0
  return Math.min(100, Math.floor((s.copied / s.rows) * 100))
})
const key = computed(() => JSON.stringify(form))
// 改了填写的内容要重新测试。
watch(key, (k) => {
  if (k !== testedKey.value) tested.value = null
})

onMounted(load)
onBeforeUnmount(() => clearInterval(timer))

async function load() {
  try {
    info.value = await props.call<Info>('GET', '/database')
    loadError.value = ''
    if (info.value.status.stage === 'running') poll()
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

function target() {
  return { ...form, port: Number(form.port) || 3306 }
}

async function test() {
  testing.value = true
  testError.value = ''
  tested.value = null
  try {
    tested.value = await props.call<TestResult>('POST', '/database/test', target())
    testedKey.value = key.value
  } catch (e) {
    testError.value = e instanceof ApiError ? e.message : '测试失败'
  } finally {
    testing.value = false
  }
}

async function start() {
  starting.value = true
  startError.value = ''
  try {
    const st = await props.call<MoveStatus>('POST', '/database/migrate', target())
    if (info.value) info.value.status = st
    confirming.value = false
    poll()
  } catch (e) {
    startError.value = e instanceof ApiError ? e.message : '开始迁移失败'
  } finally {
    starting.value = false
  }
}

function poll() {
  clearInterval(timer)
  timer = setInterval(async () => {
    try {
      const r = await props.call<Info>('GET', '/database')
      info.value = r
      if (r.status.stage === 'failed') clearInterval(timer)
      if (r.status.stage === 'done') {
        clearInterval(timer)
        if (r.supervised) waitRestart()
        else restart.value = 'manual'
      }
    } catch {
      // 服务正在重启时请求会失败，等下一次。
    }
  }, 1000)
}

// waitRestart 等服务重启：先等它停下，再等它用 MySQL 重新启动。
function waitRestart() {
  restart.value = 'waiting'
  let wentDown = false
  const started = Date.now()
  timer = setInterval(async () => {
    try {
      const r = await fetch('/api/v1/meta', { cache: 'no-store' })
      if (!r.ok) throw new Error()
      const meta = await r.json()
      // 服务停过一次、又回来了，并且不再处于维护状态，就是重启好了。
      if ((wentDown || Date.now() - started > 15000) && !meta.maintenance) {
        clearInterval(timer)
        restart.value = 'back'
      }
    } catch {
      wentDown = true
    }
  }, 1500)
}

function relogin() {
  location.reload()
}
</script>

<template>
  <section class="panel panel--inset">
    <h2>数据库</h2>
    <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
    <template v-else-if="info">
      <!-- 已经是 MySQL -->
      <template v-if="info.driver === 'mysql' && status?.stage !== 'done'">
        <p class="state"><span class="dot dot--ok" />现在用的是 <strong>MySQL</strong>。</p>
        <p class="caption">数据都在 MySQL 里，可以放心让更多人同时使用。</p>
      </template>

      <!-- 迁移完成 -->
      <div v-else-if="status?.stage === 'done'" class="result">
        <p class="state"><span class="dot dot--ok" />迁移完成，{{ status.copied.toLocaleString() }} 行数据全部核对无误。</p>
        <template v-if="restart === 'waiting'">
          <p class="caption">正在重启服务，切换到 MySQL……（一般十几秒）</p>
          <div class="workspace-progress indeterminate"><i /></div>
        </template>
        <template v-else-if="restart === 'back'">
          <p>服务已经用 MySQL 重新启动。重启后需要重新登录管理后台。</p>
          <button class="btn btn-primary" @click="relogin">重新登录</button>
        </template>
        <template v-else>
          <p>配置已经切换到 MySQL，<strong>重启服务后生效</strong>。在服务器上运行：</p>
          <pre class="cmd">{{ info.restart }}</pre>
          <p class="caption">重启之前网站一直是只能查看的状态（防止新数据只写进旧的 SQLite）。</p>
        </template>
        <p class="caption">原来的 SQLite 文件还留着（<code>{{ info.sqlite_path }}</code>），确认一切正常后可以自己删掉或留作备份。</p>
      </div>

      <!-- 迁移中 -->
      <div v-else-if="status?.stage === 'running'" class="result">
        <p class="state"><span class="dot dot--busy" />正在迁移到 {{ status.target }}：{{ status.step }}</p>
        <div class="workspace-progress"><i :style="{ width: `${pct}%` }" /></div>
        <p class="caption">{{ status.copied.toLocaleString() }} / {{ status.rows.toLocaleString() }} 行（{{ pct }}%）· 迁移期间网站只能查看，不能发消息。不要关掉服务。</p>
        <button type="button" class="link caption" @click="showTables = !showTables">{{ showTables ? '收起' : '查看每张表' }}</button>
        <ul v-if="showTables" class="tables">
          <li v-for="t in status.tables" :key="t.name"><code>{{ t.name }}</code><span>{{ t.copied }} / {{ t.rows }}</span></li>
        </ul>
      </div>

      <!-- 可以迁移 -->
      <template v-else>
        <p class="state">
          <span class="dot" />现在用的是 <strong>SQLite</strong><template v-if="info.sqlite_bytes">（{{ formatSize(info.sqlite_bytes) }}）</template>。
        </p>
        <p class="caption intro">
          SQLite 是一个文件，简单省心，几十个人用完全够。人多了、或者想用宝塔之类的面板统一管理和备份数据库时，可以一键搬到 MySQL：
          所有聊天记录、记忆、待办、余额和卡密都会原样搬过去，核对无误后自动切换。
        </p>

        <div v-if="status?.stage === 'failed'" class="form-flash form-flash--error">
          上次迁移没有成功：{{ status.error }}<br />已经自动恢复，网站照常使用，SQLite 里的数据没有任何变化。改一下再试就行。
        </div>
        <p v-if="info.blocked" class="form-flash form-flash--error">{{ info.blocked }}</p>

        <template v-else>
          <ol class="steps">
            <li>
              <strong>准备一个空的 MySQL 数据库</strong>（MySQL 5.7 以上，推荐 8.0）。用宝塔的话：「数据库 → 添加数据库」，
              数据库名 <code>buugoo</code>，用户名和密码自己定，访问权限选「本地服务器」。数据库不存在时，布谷也会试着自己建。
            </li>
            <li><strong>填好下面的信息，点「测试连接」。</strong></li>
            <li><strong>点「开始迁移」</strong>，等进度走完，服务会自动重启并切换到 MySQL。</li>
          </ol>

          <form class="grid" autocomplete="off" @submit.prevent="test">
            <label class="field">
              <span class="field__label">地址</span>
              <input v-model="form.host" class="text-input" placeholder="127.0.0.1" spellcheck="false" />
            </label>
            <label class="field">
              <span class="field__label">端口</span>
              <input v-model.number="form.port" class="text-input" type="number" min="1" max="65535" />
            </label>
            <label class="field">
              <span class="field__label">用户名</span>
              <input v-model="form.user" class="text-input" spellcheck="false" autocomplete="off" />
            </label>
            <label class="field">
              <span class="field__label">密码</span>
              <input v-model="form.password" class="text-input" type="password" autocomplete="new-password" />
            </label>
            <label class="field field--wide">
              <span class="field__label">数据库名</span>
              <input v-model="form.database" class="text-input" spellcheck="false" />
              <span class="field__hint">必须是空的（或者还不存在）。密码会保存在服务器数据目录的设置文件里（只有服务自己能读）。</span>
            </label>
            <div class="actions field--wide">
              <button class="btn" :disabled="testing || !form.user">{{ testing ? '正在测试' : '测试连接' }}</button>
              <button type="button" class="btn btn-primary" :disabled="!tested" @click="confirming = true">开始迁移</button>
            </div>
          </form>
          <p v-if="testError" class="form-flash form-flash--error">{{ testError }}</p>
          <p v-else-if="tested" class="form-flash ok">
            连接成功（MySQL {{ tested.version }}）。要搬 {{ tested.tables }} 张表、{{ tested.source_rows.toLocaleString() }} 行数据。可以开始迁移了。
          </p>
        </template>
      </template>
    </template>
    <div v-else class="skeleton skel" aria-hidden="true" />

    <Modal :open="confirming" title="开始迁移？" @close="confirming = false">
      <ul class="confirm-list">
        <li>迁移期间网站<strong>只能查看、不能发消息</strong>，聊天软件发来的消息也会暂时回不了。数据不多的话一两分钟就好。</li>
        <li>全部复制并核对无误后，配置自动切换到 MySQL{{ info?.supervised ? '，服务会自动重启' : '，然后需要你手动重启服务' }}。</li>
        <li>中途出任何问题都会自动恢复，原来的 SQLite 文件一直保留，不会动它。</li>
      </ul>
      <p v-if="startError" class="form-flash form-flash--error">{{ startError }}</p>
      <div class="modal-actions">
        <button type="button" class="btn btn-ghost" @click="confirming = false">取消</button>
        <button type="button" class="btn btn-primary" :disabled="starting" @click="start">{{ starting ? '正在开始' : '开始迁移' }}</button>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
h2 { margin: 0 0 12px; }
.skel { height: 160px; border-radius: var(--radius-md); }
.state { display: flex; align-items: center; gap: 8px; margin: 0 0 6px; color: var(--color-ink); }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--color-hairline-strong); flex-shrink: 0; }
.dot--ok { background: var(--color-accent); }
.dot--busy { background: var(--color-accent); animation: blink 1s ease-in-out infinite; }
@keyframes blink { 50% { opacity: .3; } }
.intro { margin: 0 0 14px; line-height: 1.7; }
.steps { margin: 0 0 16px; padding-left: 20px; font-size: 14px; line-height: 1.8; color: var(--color-text-secondary); }
.steps strong { color: var(--color-ink); font-weight: 500; }
.grid { display: grid; grid-template-columns: minmax(0, 2fr) minmax(0, 1fr); gap: 4px 12px; }
.field--wide { grid-column: 1 / -1; }
.actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 4px; }
.ok { border-color: rgba(91, 122, 74, .3); background: var(--color-accent-tint, rgba(91, 122, 74, .1)); color: var(--color-accent-active); }
.result { display: grid; gap: 10px; }
.result p { margin: 0; }
.result .btn { justify-self: start; }
.workspace-progress { height: 8px; }
.indeterminate i { width: 30% !important; animation: slide 1.2s ease-in-out infinite; }
@keyframes slide { 0% { transform: translateX(-100%); } 100% { transform: translateX(340%); } }
.cmd { margin: 0; padding: 10px 12px; border-radius: var(--radius-md); background: var(--color-canvas); font-family: var(--font-mono); font-size: 13px; overflow-x: auto; }
.tables { margin: 0; padding: 0; list-style: none; font-size: 13px; columns: 2; column-gap: 24px; }
.tables li { display: flex; justify-content: space-between; gap: 8px; padding: 2px 0; break-inside: avoid; font-variant-numeric: tabular-nums; }
.link { justify-self: start; padding: 0; text-decoration: underline; text-underline-offset: 3px; }
.confirm-list { margin: 0 0 12px; padding-left: 18px; line-height: 1.8; font-size: 14px; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; }
@media (max-width: 560px) {
  .grid { grid-template-columns: minmax(0, 1fr); }
  .tables { columns: 1; }
}
</style>
