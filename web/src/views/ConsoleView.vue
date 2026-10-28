<script setup lang="ts">
// 管理后台：用配置文件中的管理员密码登录，查看与修改全部系统设置。
// 保存后写入数据目录中的 settings.yaml，管理员需要自己重启服务才会生效。
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ApiError } from '../api'
import { getConsoleToken, setConsoleToken } from '../consoleToken'
import ConsoleChannels from '../components/ConsoleChannels.vue'
import ConsoleDatabase from '../components/ConsoleDatabase.vue'
import ConsoleOpenCode from '../components/ConsoleOpenCode.vue'
import ConsoleCards from '../components/ConsoleCards.vue'
import ConsoleDiagnostics from '../components/ConsoleDiagnostics.vue'
import ConsoleExtensions from '../components/ConsoleExtensions.vue'
import ConsolePlans from '../components/ConsolePlans.vue'
import ConsolePricing from '../components/ConsolePricing.vue'
import ConsoleUsers from '../components/ConsoleUsers.vue'
import Modal from '../components/Modal.vue'
import SettingField, { type Field } from '../components/SettingField.vue'
import { session } from '../session'
import { toast } from '../toast'

interface SettingsResp {
  fields: Field[]
  groups: { key: string; label: string; help?: string }[]
  pending: number
  settings_path: string
  config_path: string
  default_password: boolean
  restart_hint: string
}

const token = ref(read())
const password = ref('')
const loggingIn = ref(false)
const data = ref<SettingsResp | null>(null)
const error = ref('')
const draft = reactive<Record<string, unknown>>({})
const saving = ref(false)
const justSaved = ref<string[]>([])
const query = ref('')
// tab 是左侧目录中选中的一项：设置分类的 key，或者 persona（人格档案）、extensions（技能与插件）、channels（聊天软件连接）。
const tab = ref(location.hash.slice(1) || 'models')
const showAdvanced = reactive<Record<string, boolean>>({})

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

function openTab(t: string) {
  tab.value = t
  query.value = ''
  if (t === 'persona' && !persona.value) loadPersona()
  try {
    history.replaceState(null, '', `#${t}`)
  } catch {
    // 忽略
  }
  window.scrollTo({ top: 0, behavior: 'smooth' })
}
watch(
  () => data.value,
  (d) => {
    if (!d) return
    if (tab.value === 'persona' && !persona.value) loadPersona()
    if (!['persona', 'channels', 'extensions', 'users', 'plans', 'pricing', 'cards', 'diagnostics', 'database', 'opencode'].includes(tab.value) && !d.groups.some((g) => g.key === tab.value)) tab.value = 'models'
  },
)

function read() {
  return getConsoleToken()
}
function store(t: string) {
  token.value = t
  setConsoleToken(t)
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

function matches(f: Field, q: string) {
  return f.key.toLowerCase().includes(q) || f.label.toLowerCase().includes(q) || (f.help ?? '').toLowerCase().includes(q) || f.section.includes(q)
}

interface SectionView {
  key: string
  label: string
  common: Field[]
  advanced: Field[]
}
interface GroupView {
  key: string
  label: string
  help?: string
  sections: SectionView[]
  count: number
}

function sectionsOf(fields: Field[], groupKey: string): SectionView[] {
  const out: SectionView[] = []
  for (const f of fields) {
    let s = out.find((x) => x.label === f.section)
    if (!s) out.push((s = { key: `${groupKey}/${f.section}`, label: f.section, common: [], advanced: [] }))
    ;(f.advanced ? s.advanced : s.common).push(f)
  }
  return out
}

// 目录：每个分类和其中待重启、未保存的数量。
const nav = computed(() =>
  (data.value?.groups ?? [])
    .map((g) => {
      const fs = data.value!.fields.filter((f) => f.group === g.key)
      return { ...g, total: fs.length, pending: fs.filter((f) => f.pending).length, dirty: fs.filter((f) => f.key in draft).length }
    })
    .filter((g) => g.total > 0),
)

// 正在看的内容：有搜索词时显示所有分类里匹配的设置（高级设置也一起），否则只显示选中的分类。
const visible = computed<GroupView[]>(() => {
  if (!data.value) return []
  const q = query.value.trim().toLowerCase()
  return data.value.groups
    .filter((g) => (q ? true : g.key === tab.value))
    .map((g) => {
      const fs = data.value!.fields.filter((f) => f.group === g.key && (!q || matches(f, q)))
      return { key: g.key, label: g.label, help: g.help, sections: sectionsOf(fs, g.key), count: fs.length }
    })
    .filter((g) => g.count > 0)
})
const searching = computed(() => query.value.trim() !== '')

async function save() {
  saving.value = true
  error.value = ''
  try {
    const changes: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(draft)) {
      const f = data.value!.fields.find((x) => x.key === k)!
      changes[k] = f.type === 'int' || f.type === 'float' || f.type === 'context' ? Number(v) : v
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
          <div class="console">
            <nav class="toc" aria-label="设置目录">
              <input v-model="query" class="text-input toc__search" type="search" placeholder="搜索设置" aria-label="搜索设置" />
              <p class="toc__title">系统设置</p>
              <button
                v-for="g in nav"
                :key="g.key"
                type="button"
                class="toc__item"
                :class="{ 'is-active': !searching && tab === g.key }"
                @click="openTab(g.key)"
              >
                <span>{{ g.label }}</span>
                <span v-if="g.dirty" class="toc__badge toc__badge--dirty" :title="`${g.dirty} 项未保存`">{{ g.dirty }}</span>
                <span v-else-if="g.pending" class="toc__badge" :title="`${g.pending} 项待重启生效`">{{ g.pending }}</span>
              </button>
              <template v-if="session.meta?.billing">
                <p class="toc__title">运营</p>
                <button type="button" class="toc__item" :class="{ 'is-active': !searching && tab === 'users' }" @click="openTab('users')">用户管理</button>
                <button type="button" class="toc__item" :class="{ 'is-active': !searching && tab === 'plans' }" @click="openTab('plans')">会员套餐</button>
                <button type="button" class="toc__item" :class="{ 'is-active': !searching && tab === 'pricing' }" @click="openTab('pricing')">计费</button>
                <button type="button" class="toc__item" :class="{ 'is-active': !searching && tab === 'cards' }" @click="openTab('cards')">卡密</button>
              </template>
              <p class="toc__title">其他</p>
              <button type="button" class="toc__item" :class="{ 'is-active': !searching && tab === 'persona' }" @click="openTab('persona')">人格档案</button>
              <button type="button" class="toc__item" :class="{ 'is-active': !searching && tab === 'extensions' }" @click="openTab('extensions')">技能与插件</button>
              <button type="button" class="toc__item" :class="{ 'is-active': !searching && tab === 'channels' }" @click="openTab('channels')">聊天软件连接</button>
              <button type="button" class="toc__item" :class="{ 'is-active': !searching && tab === 'opencode' }" @click="openTab('opencode')">OpenCode Go</button>
              <button type="button" class="toc__item" :class="{ 'is-active': !searching && tab === 'database' }" @click="openTab('database')">数据库迁移</button>
              <button type="button" class="toc__item" :class="{ 'is-active': !searching && tab === 'diagnostics' }" @click="openTab('diagnostics')">诊断</button>
            </nav>

            <div class="console__body">
          <template v-if="!searching && tab === 'persona'">
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

          <ConsoleChannels v-else-if="!searching && tab === 'channels'" :call="call" />

          <ConsoleExtensions v-else-if="!searching && tab === 'extensions'" :call="call" :token="token" />

          <template v-else-if="!searching && ['users', 'plans', 'pricing', 'cards'].includes(tab)">
            <p v-if="data.default_password" class="form-flash form-flash--error">
              管理员密码还是默认的 1228，为了安全，用户管理、会员和卡密暂时不能用。请先在服务器上改掉配置文件里的 <code>admin.password</code> 并重启服务。
            </p>
            <ConsoleUsers v-else-if="tab === 'users'" :call="call" />
            <ConsolePlans v-else-if="tab === 'plans'" :call="call" />
            <ConsolePricing v-else-if="tab === 'pricing'" :call="call" />
            <ConsoleCards v-else :call="call" :token="token" />
          </template>

          <ConsoleDiagnostics v-else-if="!searching && tab === 'diagnostics'" :call="call" />

          <ConsoleDatabase v-else-if="!searching && tab === 'database'" :call="call" />

          <ConsoleOpenCode v-else-if="!searching && tab === 'opencode'" :call="call" />

          <template v-else>
            <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
            <p v-if="searching && !visible.length" class="form-flash">没有找到「{{ query }}」相关的设置</p>
            <section v-for="g in visible" :key="g.key" class="group">
              <header class="group__head">
                <h2>{{ g.label }}</h2>
                <p v-if="g.help && !searching" class="muted">{{ g.help }}</p>
              </header>
              <section v-for="sec in g.sections" :key="sec.key" class="panel panel--inset section">
                <h3 class="section__title">{{ sec.label }}</h3>
                <SettingField
                  v-for="f in sec.common"
                  :key="f.key"
                  :field="f"
                  :value="shown(f)"
                  :dirty="f.key in draft"
                  @edit="(v) => edit(f, v)"
                  @discard="discard(f)"
                />
                <template v-if="sec.advanced.length">
                  <template v-if="searching || showAdvanced[sec.key] || !sec.common.length">
                    <SettingField
                      v-for="f in sec.advanced"
                      :key="f.key"
                      :field="f"
                      :value="shown(f)"
                      :dirty="f.key in draft"
                      @edit="(v) => edit(f, v)"
                      @discard="discard(f)"
                    />
                  </template>
                  <button
                    v-if="!searching && sec.common.length"
                    type="button"
                    class="adv-toggle"
                    :aria-expanded="!!showAdvanced[sec.key]"
                    @click="showAdvanced[sec.key] = !showAdvanced[sec.key]"
                  >
                    {{ showAdvanced[sec.key] ? '收起高级设置' : `高级设置（${sec.advanced.length}）` }}
                  </button>
                </template>
              </section>
            </section>
            <p class="muted foot">
              这里的修改保存在 <code>{{ data.settings_path }}</code>，覆盖配置文件中的同名设置，<strong>重启服务后生效</strong>；
              环境变量中的设置优先级最高。管理员密码和加密密钥只能在配置文件中修改。
            </p>
          </template>
            </div>
          </div>

          <Modal :open="confirmReset" title="恢复默认人格？" subtitle="管理后台保存的人格会被删除，恢复为配置文件指定的或内置的人格，立即生效。" @close="confirmReset = false">
            <div class="modal-actions">
              <button class="btn" @click="confirmReset = false">取消</button>
              <button class="btn btn-danger" @click="resetPersona">恢复默认</button>
            </div>
          </Modal>

          <div v-if="dirty" class="savebar">
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
.console { display: grid; grid-template-columns: 200px minmax(0, 1fr); gap: 28px; align-items: start; }
.toc { position: sticky; top: 16px; display: grid; gap: 2px; }
.toc__search { margin-bottom: 10px; }
.toc__title { margin: 12px 10px 4px; color: var(--color-muted); font-size: 12px; letter-spacing: .04em; }
.toc__title:first-of-type { margin-top: 0; }
.toc__item {
  display: flex; align-items: center; gap: 8px; min-height: 36px; padding: 0 10px; border-radius: var(--radius-md);
  color: var(--color-text-secondary); font-size: 14px; text-align: left; transition: background-color .15s ease, color .15s ease;
}
.toc__item span:first-child { flex: 1; }
.toc__item:hover { background: var(--color-hover-wash); color: var(--color-ink); }
.toc__item.is-active { background: var(--color-surface); color: var(--color-ink); font-weight: 500; box-shadow: 0 1px 3px rgba(20, 20, 19, .08), 0 0 0 1px rgba(20, 20, 19, .04); }
.toc__badge { min-width: 18px; padding: 0 5px; border-radius: 999px; background: var(--color-warning); color: #fff; font-size: 11px; line-height: 18px; text-align: center; }
.toc__badge--dirty { background: var(--color-accent); }
.group + .group { margin-top: 28px; }
.group__head { margin-bottom: 12px; }
.group__head h2 { margin: 0; }
.group__head p { margin: 4px 0 0; font-size: 14px; }
.section { margin-bottom: 16px; }
.section__title { margin: 0 0 4px; color: var(--color-ink); font-size: 15px; font-weight: 600; }
.adv-toggle { margin-top: 6px; padding: 6px 0; color: var(--color-accent-active); font-size: 13px; }
.foot { margin: 8px 0 0; font-size: 13px; line-height: 1.7; }
@media (max-width: 900px) {
  .console { grid-template-columns: minmax(0, 1fr); gap: 16px; }
  .toc { position: static; display: flex; gap: 6px; overflow-x: auto; padding-bottom: 4px; scrollbar-width: none; }
  .toc__search { flex: 0 0 160px; margin: 0; }
  .toc__title { display: none; }
  .toc__item { flex: 0 0 auto; white-space: nowrap; border: 1px solid var(--color-divider); }
}
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
.savebar {
  position: sticky; bottom: 16px; display: flex; align-items: center; gap: 8px; margin-top: 20px; padding: 12px 16px;
  border: 1px solid var(--color-divider); border-radius: var(--radius-lg); background: var(--color-surface); box-shadow: var(--shadow-float);
}
.savebar span { flex: 1; font-size: 14px; }
</style>
