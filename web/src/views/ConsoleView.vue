<script setup lang="ts">
// 管理后台：用配置文件中的管理员密码登录，查看与修改全部系统设置。
// 保存后写入数据目录中的 settings.yaml，管理员需要自己重启服务才会生效。
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError } from '../api'
import Modal from '../components/Modal.vue'
import { session } from '../session'
import { toast } from '../toast'

interface Field {
  key: string
  group: string
  label: string
  help?: string
  type: 'string' | 'bool' | 'int' | 'float' | 'duration' | 'list' | 'select' | 'secret'
  options?: string[]
  value: unknown
  saved?: unknown
  pending: boolean
  locked: boolean
  env: boolean
}

interface SettingsResp {
  fields: Field[]
  groups: { key: string; label: string }[]
  pending: number
  settings_path: string
  config_path: string
  default_password: boolean
  restart_hint: string
}

const TOKEN_KEY = 'buugoo.console'
const token = ref(read())
const password = ref('')
const loggingIn = ref(false)
const data = ref<SettingsResp | null>(null)
const error = ref('')
const draft = reactive<Record<string, unknown>>({})
const saving = ref(false)
const justSaved = ref<string[]>([])
const query = ref('')
const tab = ref<'settings' | 'persona'>('settings')

interface PersonaResp {
  yaml: string
  source: 'web' | 'file' | 'builtin'
  path: string
  name: string
  default: string
}
const persona = ref<PersonaResp | null>(null)
const personaText = ref('')
const personaError = ref('')
const personaSaving = ref(false)
const confirmReset = ref(false)
const personaDirty = computed(() => !!persona.value && personaText.value !== persona.value.yaml)
const sourceLabel = computed(() => {
  switch (persona.value?.source) {
    case 'web':
      return '管理后台保存的人格'
    case 'file':
      return '配置文件 persona.file 指定的文件'
    default:
      return '内置的默认人格'
  }
})

function applyPersona(r: PersonaResp) {
  persona.value = r
  personaText.value = r.yaml
  if (session.meta) session.meta.name = r.name
}

async function loadPersona() {
  try {
    applyPersona(await call<PersonaResp>('GET', '/persona'))
  } catch (e) {
    personaError.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function savePersona() {
  personaSaving.value = true
  personaError.value = ''
  try {
    applyPersona(await call<PersonaResp>('PUT', '/persona', { yaml: personaText.value }))
    toast(`人格已更新，${persona.value?.name}从下一句话开始就是新的样子`)
  } catch (e) {
    personaError.value = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    personaSaving.value = false
  }
}

async function resetPersona() {
  confirmReset.value = false
  personaError.value = ''
  try {
    applyPersona(await call<PersonaResp>('DELETE', '/persona'))
    toast('已恢复默认人格')
  } catch (e) {
    personaError.value = e instanceof ApiError ? e.message : '操作失败'
  }
}

function openTab(t: 'settings' | 'persona') {
  tab.value = t
  if (t === 'persona' && !persona.value) loadPersona()
}

function read() {
  try {
    return sessionStorage.getItem(TOKEN_KEY) ?? ''
  } catch {
    return ''
  }
}
function store(t: string) {
  token.value = t
  try {
    if (t) sessionStorage.setItem(TOKEN_KEY, t)
    else sessionStorage.removeItem(TOKEN_KEY)
  } catch {
    // 无痕模式下只在本页有效。
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const resp = await fetch(`/api/v1/console${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Console-Token': token.value },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!resp.ok) {
    let msg = `请求失败（HTTP ${resp.status}）`
    let code = 'http_error'
    try {
      const j = await resp.json()
      msg = j?.error?.message ?? msg
      code = j?.error?.code ?? code
    } catch {
      // 非 JSON 响应
    }
    if (resp.status === 401 && path !== '/login') store('')
    throw new ApiError(resp.status, code, msg)
  }
  if (resp.status === 204) return undefined as T
  return (await resp.json()) as T
}

async function login() {
  loggingIn.value = true
  error.value = ''
  try {
    const r = await call<{ token: string }>('POST', '/login', { password: password.value })
    store(r.token)
    password.value = ''
    await load()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '登录失败'
  } finally {
    loggingIn.value = false
  }
}

async function load() {
  try {
    data.value = await call<SettingsResp>('GET', '/settings')
    for (const k of Object.keys(draft)) delete draft[k]
  } catch (e) {
    if (!token.value) return
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function logout() {
  await call('POST', '/logout').catch(() => {})
  store('')
  data.value = null
}

// 输入框里显示的值：有未保存的修改用修改值，否则用已保存（待生效）的值，再否则用运行中的值。
function shown(f: Field): unknown {
  if (f.key in draft) return draft[f.key]
  if (f.type === 'secret') return ''
  if (f.saved !== undefined && f.saved !== null) return f.type === 'list' && Array.isArray(f.saved) ? f.saved.join(', ') : f.saved
  if (f.type === 'list' && Array.isArray(f.value)) return (f.value as string[]).join(', ')
  return f.value
}

function edit(f: Field, v: unknown) {
  draft[f.key] = v
}

const dirty = computed(() => Object.keys(draft).length)

const groups = computed(() => {
  if (!data.value) return []
  const q = query.value.trim().toLowerCase()
  return data.value.groups
    .map((g) => ({
      ...g,
      fields: data.value!.fields.filter(
        (f) => f.group === g.key && (!q || f.key.toLowerCase().includes(q) || f.label.toLowerCase().includes(q) || (f.help ?? '').includes(q)),
      ),
    }))
    .filter((g) => g.fields.length)
})

async function save() {
  saving.value = true
  error.value = ''
  try {
    const changes: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(draft)) {
      const f = data.value!.fields.find((x) => x.key === k)!
      changes[k] = f.type === 'int' || f.type === 'float' ? Number(v) : v
    }
    const r = await call<{ changed: string[] }>('PUT', '/settings', { changes })
    justSaved.value = r.changed
    toast(`已保存 ${r.changed.length} 项，重启服务后生效`)
    await load()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function discard(f: Field) {
  try {
    await call('POST', '/settings/discard', { keys: [f.key] })
    toast(`「${f.label}」已恢复为配置文件中的设置，重启后生效`)
    await load()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '操作失败'
  }
}

function display(f: Field) {
  if (f.type === 'secret') return f.value ? '已设置' : '未设置'
  if (Array.isArray(f.value)) return f.value.join(', ') || '（空）'
  if (f.value === '' || f.value === null) return '（空）'
  return String(f.value)
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast('已复制')
  } catch {
    // 复制失败时用户可以手动选择
  }
}

onMounted(() => {
  if (token.value) load()
})
</script>

<template>
  <main class="main">
    <header class="topbar">
      <div class="topbar__titles">
        <span class="eyebrow">CONSOLE · 管理后台</span>
        <h1>管理后台</h1>
      </div>
      <button v-if="data" class="btn btn-ghost" @click="logout">退出管理后台</button>
    </header>

    <div class="page">
      <div class="page__inner">
        <section v-if="!data" class="panel panel--inset login">
          <h2>输入管理员密码</h2>
          <p class="muted">管理员密码在服务器的配置文件 <code>admin.password</code> 中设置，默认是 1228。</p>
          <form @submit.prevent="login">
            <div class="field">
              <label class="field__label" for="admin-pw">管理员密码</label>
              <input id="admin-pw" v-model="password" class="text-input" type="password" autocomplete="current-password" required />
            </div>
            <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
            <button class="btn btn-primary" :disabled="loggingIn">{{ loggingIn ? '正在验证' : '进入管理后台' }}</button>
          </form>
        </section>

        <template v-else>
          <p v-if="data.default_password" class="form-flash form-flash--error">
            管理员密码还是默认的 1228，任何能打开这个网站的人都能修改系统设置。请在服务器上编辑
            <code>{{ data.config_path || '配置文件' }}</code> 中的 <code>admin.password</code>，然后重启服务。
          </p>
          <div v-if="data.pending" class="form-flash restart">
            <p><strong>{{ data.pending }} 项修改尚未生效。</strong>请在服务器上执行下面的命令重启服务：</p>
            <div class="cmd">
              <code>{{ data.restart_hint }}</code>
              <button class="btn" @click="copy(data.restart_hint)">复制</button>
            </div>
          </div>
          <div class="type-switch tabs" role="tablist" aria-label="管理后台">
            <button type="button" role="tab" :class="{ 'is-active': tab === 'settings' }" @click="openTab('settings')">系统设置</button>
            <button type="button" role="tab" :class="{ 'is-active': tab === 'persona' }" @click="openTab('persona')">人格</button>
          </div>

          <template v-if="tab === 'persona'">
            <section class="panel panel--inset">
              <h2>人格档案</h2>
              <p class="muted intro">
                决定{{ persona?.name ?? '布谷' }}是谁、怎么说话、在乎什么。保存后<strong>立即生效</strong>，不需要重启；
                对话中没有任何办法修改它。当前使用的是{{ sourceLabel }}<template v-if="persona?.path">（<code>{{ persona.path }}</code>）</template>。
              </p>
              <ul class="caption guide">
                <li><code>name</code> 名字、<code>identity</code> 一句话自我认知（写在同一行）、<code>personality</code> 性格、<code>speaking_style</code> 说话风格、<code>catchphrases</code> 口头禅、<code>interests</code> 兴趣、<code>values</code> 价值观、<code>principles</code> 必须遵守的原则</li>
                <li>name、identity、personality、principles 必填；列表每行以「- 」开头</li>
              </ul>
              <textarea v-model="personaText" class="textarea persona-editor" spellcheck="false" aria-label="人格档案 YAML" />
              <p v-if="personaError" class="form-flash form-flash--error">{{ personaError }}</p>
              <div class="persona-actions">
                <button class="btn btn-danger-lite" :disabled="persona?.source === 'builtin' && !personaDirty" @click="confirmReset = true">恢复默认</button>
                <span class="spacer" />
                <button class="btn btn-ghost" :disabled="!personaDirty" @click="personaText = persona?.yaml ?? ''">撤销修改</button>
                <button class="btn btn-primary" :disabled="!personaDirty || personaSaving" @click="savePersona">{{ personaSaving ? '正在保存' : '保存并生效' }}</button>
              </div>
            </section>
          </template>

          <template v-else>
          <p class="muted intro">
            这里的修改保存在 <code>{{ data.settings_path }}</code>，覆盖配置文件中的同名设置；环境变量中的设置优先级最高，
            这里改了也不会生效。管理员密码和加密密钥只能在配置文件中修改。
          </p>
          <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>

          <input v-model="query" class="text-input search" placeholder="搜索设置" aria-label="搜索设置" />

          <section v-for="g in groups" :key="g.key" class="panel panel--inset group">
            <h2>{{ g.label }}</h2>
            <div v-for="f in g.fields" :key="f.key" class="row" :class="{ 'is-dirty': f.key in draft }">
              <div class="row__head">
                <label class="field__label" :for="`s-${f.key}`">{{ f.label }}</label>
                <code class="row__key">{{ f.key }}</code>
                <span v-if="f.pending" class="badge-pill pending">待重启生效</span>
                <span v-if="f.env" class="badge-pill">由环境变量设置</span>
              </div>
              <div class="row__input">
                <select
                  v-if="f.type === 'select'"
                  :id="`s-${f.key}`"
                  class="text-input"
                  :value="shown(f)"
                  :disabled="f.locked || f.env"
                  @change="edit(f, ($event.target as HTMLSelectElement).value)"
                >
                  <option v-for="o in f.options" :key="o" :value="o">{{ o || '（沿用聊天模型）' }}</option>
                </select>
                <label v-else-if="f.type === 'bool'" class="check-row">
                  <input
                    :id="`s-${f.key}`"
                    type="checkbox"
                    :checked="Boolean(shown(f))"
                    :disabled="f.locked || f.env"
                    @change="edit(f, ($event.target as HTMLInputElement).checked)"
                  />
                  <span>{{ shown(f) ? '开启' : '关闭' }}</span>
                </label>
                <input
                  v-else
                  :id="`s-${f.key}`"
                  class="text-input"
                  :type="f.type === 'secret' ? 'password' : f.type === 'int' || f.type === 'float' ? 'number' : 'text'"
                  :step="f.type === 'float' ? 'any' : undefined"
                  autocomplete="off"
                  :value="shown(f)"
                  :placeholder="f.type === 'secret' ? (f.value ? '已设置，留空表示不修改' : '未设置') : ''"
                  :disabled="f.locked || f.env"
                  @input="edit(f, ($event.target as HTMLInputElement).value)"
                />
              </div>
              <p class="field__hint">
                <template v-if="f.help">{{ f.help }}</template>
                <template v-if="f.pending"> 正在运行的值：{{ display(f) }}。</template>
                <button v-if="f.saved !== undefined && f.saved !== null" type="button" class="text-btn" @click="discard(f)">恢复为配置文件中的设置</button>
              </p>
            </div>
          </section>

          </template>

          <Modal :open="confirmReset" title="恢复默认人格？" subtitle="管理后台保存的人格会被删除，恢复为配置文件指定的或内置的人格，立即生效。" @close="confirmReset = false">
            <div class="modal-actions">
              <button class="btn" @click="confirmReset = false">取消</button>
              <button class="btn btn-danger" @click="resetPersona">恢复默认</button>
            </div>
          </Modal>

          <div v-if="dirty && tab === 'settings'" class="savebar">
            <span>{{ dirty }} 项修改未保存</span>
            <button class="btn btn-ghost" @click="load">放弃</button>
            <button class="btn btn-primary" :disabled="saving" @click="save">{{ saving ? '正在保存' : '保存' }}</button>
          </div>
        </template>
      </div>
    </div>
  </main>
</template>

<style scoped>
.login { max-width: 460px; margin: 6vh auto 0; }
.tabs { max-width: 280px; margin-bottom: 20px; }
.guide { margin: 0 0 12px; padding-left: 18px; line-height: 1.7; }
.persona-editor {
  min-height: 520px; font-family: var(--font-mono); font-size: 13px; line-height: 1.65; tab-size: 2; white-space: pre; overflow-x: auto;
}
.persona-actions { display: flex; align-items: center; gap: 8px; margin-top: 14px; flex-wrap: wrap; }
.spacer { flex: 1; }
.login p { margin: 0 0 16px; }
.intro { margin: 0 0 16px; font-size: 14px; line-height: 1.7; }
.restart p { margin: 0 0 8px; }
.cmd { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.cmd code { padding: 6px 10px; border-radius: var(--radius-sm); background: var(--color-canvas-soft); font-size: 13px; }
.search { margin-bottom: 20px; }
.group h2 { margin-bottom: 4px; }
.row { padding: 14px 0; border-top: 1px solid var(--color-divider); }
.group h2 + .row { border-top: 0; }
.row.is-dirty .field__label { color: var(--color-accent-active); }
.row__head { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-bottom: 7px; }
.row__key { color: var(--color-muted); }
.pending { border-color: var(--color-warning); color: var(--color-warning); }
.row__input { max-width: 520px; }
.row .check-row { margin: 0; }
.row .field__hint { margin-top: 6px; }
.text-btn { margin-left: 6px; padding: 0; color: var(--color-accent-active); font-size: inherit; text-decoration: underline; text-underline-offset: 3px; }
.savebar {
  position: sticky; bottom: 16px; display: flex; align-items: center; gap: 8px; margin-top: 20px; padding: 12px 16px;
  border: 1px solid var(--color-divider); border-radius: var(--radius-lg); background: var(--color-surface); box-shadow: var(--shadow-float);
}
.savebar span { flex: 1; font-size: 14px; }
</style>
