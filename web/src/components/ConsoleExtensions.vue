<script setup lang="ts">
// 管理后台 → 技能与插件：安装 skill（GitHub、zip、粘贴 SKILL.md），添加 MCP 插件（远程地址或本机命令）。
// 装好后全站可用，用户可以在自己的设置里关掉。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError } from '../api'
import { formatSize } from '../format'
import Modal from './Modal.vue'
import { toast } from '../toast'

interface Skill {
  id: number
  name: string
  description: string
  source: string
  source_url?: string
  network: boolean
  enabled: boolean
  files: number
  size: number
  updated_at: string
}
interface Candidate {
  path: string
  name: string
  description: string
}
interface Plugin {
  id: number
  name: string
  transport: 'stdio' | 'http' | 'sse'
  command?: string
  args?: string[]
  url?: string
  env_keys?: string[]
  header_keys?: string[]
  enabled: boolean
  connected: boolean
  connecting: boolean
  server?: string
  tools: { name: string; description: string }[]
  error?: string
}

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T>; token: string }>()

const skills = ref<Skill[] | null>(null)
const sandbox = ref('')
const plugins = ref<Plugin[] | null>(null)
const error = ref('')

// 安装 skill
const mode = ref<'github' | 'zip' | 'text'>('github')
const ghURL = ref('')
const zipFile = ref<File | null>(null)
const skillText = ref('')
const installing = ref(false)
const candidates = ref<Candidate[]>([])
const detail = ref<{ skill: Skill; body: string; files: string[] } | null>(null)
const removing = ref<{ kind: 'skill' | 'plugin'; id: number; name: string } | null>(null)

// 添加插件
const showAdd = ref(false)
const form = ref({ name: '', transport: 'http' as Plugin['transport'], url: '', headers: '', command: '', args: '', env: '' })
const adding = ref(false)
const addError = ref('')

let timer: ReturnType<typeof setInterval> | undefined
const anyConnecting = computed(() => plugins.value?.some((p) => p.enabled && p.connecting) ?? false)

onMounted(() => {
  load()
  timer = setInterval(() => {
    if (anyConnecting.value) loadPlugins()
  }, 3000)
})
onBeforeUnmount(() => clearInterval(timer))

function fail(e: unknown, fallback: string) {
  const msg = e instanceof ApiError ? e.message : fallback
  toast(msg)
  return msg
}

async function load() {
  error.value = ''
  try {
    const r = await props.call<{ skills: Skill[]; sandbox: string }>('GET', '/skills')
    skills.value = r.skills
    sandbox.value = r.sandbox
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
  await loadPlugins()
}

async function loadPlugins() {
  try {
    plugins.value = (await props.call<{ plugins: Plugin[] }>('GET', '/plugins')).plugins
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function done(r: { skill?: Skill; candidates?: Candidate[] }) {
  if (r.candidates?.length) {
    candidates.value = r.candidates
    toast(`找到 ${r.candidates.length} 个 skill，选一个装`)
    return
  }
  candidates.value = []
  ghURL.value = ''
  skillText.value = ''
  zipFile.value = null
  toast(`已安装「${r.skill?.name}」`)
  await load()
}

async function install(path = '') {
  if (installing.value) return
  installing.value = true
  try {
    if (mode.value === 'github') {
      await done(await props.call('POST', '/skills/github', { url: ghURL.value, path }))
    } else if (mode.value === 'text') {
      await done(await props.call('POST', '/skills/text', { content: skillText.value }))
    } else if (zipFile.value) {
      const body = new FormData()
      body.append('file', zipFile.value)
      if (path) body.append('path', path)
      const resp = await fetch('/api/v1/console/skills/zip', { method: 'POST', headers: { 'X-Console-Token': props.token }, body })
      const j = await resp.json().catch(() => null)
      if (!resp.ok) throw new ApiError(resp.status, j?.error?.code ?? 'http_error', j?.error?.message ?? `上传失败（HTTP ${resp.status}）`)
      await done(j)
    }
  } catch (e) {
    fail(e, '安装失败')
  } finally {
    installing.value = false
  }
}

function pickZip(e: Event) {
  zipFile.value = (e.target as HTMLInputElement).files?.[0] ?? null
  candidates.value = []
}

const canInstall = computed(() =>
  mode.value === 'github' ? ghURL.value.trim() !== '' : mode.value === 'text' ? skillText.value.trim() !== '' : !!zipFile.value,
)

async function updateSkill(s: Skill, patch: { enabled?: boolean; network?: boolean }) {
  try {
    Object.assign(s, await props.call<Skill>('PATCH', `/skills/${s.id}`, patch))
  } catch (e) {
    fail(e, '保存失败')
  }
}

async function openSkill(s: Skill) {
  try {
    detail.value = await props.call('GET', `/skills/${s.id}`)
  } catch (e) {
    fail(e, '加载失败')
  }
}

async function confirmRemove() {
  const r = removing.value
  removing.value = null
  if (!r) return
  try {
    await props.call('DELETE', `/${r.kind === 'skill' ? 'skills' : 'plugins'}/${r.id}`)
    toast(`已删除「${r.name}」`)
    await load()
  } catch (e) {
    fail(e, '删除失败')
  }
}

function parsePairs(text: string, sep: string): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const i = line.indexOf(sep)
    if (i <= 0) continue
    out[line.slice(0, i).trim()] = line.slice(i + 1).trim()
  }
  return Object.keys(out).length ? out : undefined
}

async function addPlugin() {
  adding.value = true
  addError.value = ''
  const f = form.value
  const remote = f.transport !== 'stdio'
  try {
    await props.call('POST', '/plugins', {
      name: f.name,
      transport: f.transport,
      url: remote ? f.url : '',
      headers: remote ? parsePairs(f.headers, ':') : undefined,
      command: remote ? '' : f.command,
      args: remote ? undefined : f.args.split('\n').map((s) => s.trim()).filter(Boolean),
      env: remote ? undefined : parsePairs(f.env, '='),
    })
    showAdd.value = false
    form.value = { name: '', transport: 'http', url: '', headers: '', command: '', args: '', env: '' }
    toast('已添加，正在连接')
    await loadPlugins()
  } catch (e) {
    addError.value = e instanceof ApiError ? e.message : '添加失败'
  } finally {
    adding.value = false
  }
}

async function setPlugin(p: Plugin, enabled: boolean) {
  try {
    await props.call('PATCH', `/plugins/${p.id}`, { enabled })
    await loadPlugins()
  } catch (e) {
    fail(e, '保存失败')
  }
}

const reconnecting = ref(0)
async function reconnect(p: Plugin) {
  reconnecting.value = p.id
  try {
    await props.call('POST', `/plugins/${p.id}/reconnect`)
    toast(`「${p.name}」已连接`)
  } catch (e) {
    fail(e, '连接失败')
  } finally {
    reconnecting.value = 0
    await loadPlugins()
  }
}

function where(p: Plugin) {
  return p.transport === 'stdio' ? [p.command, ...(p.args ?? [])].join(' ') : p.url
}
function sourceLabel(s: Skill) {
  return { github: 'GitHub', zip: 'zip 上传', paste: '粘贴' }[s.source] ?? s.source
}
</script>

<template>
  <div class="ext">
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>

    <section class="panel panel--inset">
      <h2>技能（skill）</h2>
      <p class="muted intro">
        skill 是标准格式的技能包（一个带 <code>SKILL.md</code> 的文件夹，GitHub 上有很多开源的）。聊天时布谷看到用得上的 skill 会先读说明，需要时在沙箱里运行里面的脚本。装好后所有用户都能用，用户可以在自己的设置里关掉。
      </p>
      <p v-if="sandbox === 'off' || sandbox === ''" class="form-flash">
        系统设置里没有开启运行代码（tools.python.runner），skill 只能读说明，里面的脚本不能运行。
      </p>

      <div class="type-switch tabs" role="tablist">
        <button type="button" role="tab" :aria-selected="mode === 'github'" :class="{ 'is-active': mode === 'github' }" @click="mode = 'github'; candidates = []">GitHub 链接</button>
        <button type="button" role="tab" :aria-selected="mode === 'zip'" :class="{ 'is-active': mode === 'zip' }" @click="mode = 'zip'; candidates = []">上传 zip</button>
        <button type="button" role="tab" :aria-selected="mode === 'text'" :class="{ 'is-active': mode === 'text' }" @click="mode = 'text'; candidates = []">粘贴 SKILL.md</button>
      </div>
      <form class="install" @submit.prevent="install()">
        <template v-if="mode === 'github'">
          <input v-model="ghURL" class="text-input" placeholder="https://github.com/用户/仓库/tree/main/skills/weather" aria-label="GitHub 地址" />
          <p class="caption">可以填仓库首页、某个 skill 文件夹的地址，或者 SKILL.md 文件的地址。只支持公开仓库。</p>
        </template>
        <template v-else-if="mode === 'zip'">
          <input class="text-input" type="file" accept=".zip,application/zip" aria-label="选择 zip 文件" @change="pickZip" />
          <p class="caption">最大 100 MB。压缩包里可以有一个或多个 skill 文件夹。</p>
        </template>
        <template v-else>
          <textarea
            v-model="skillText"
            class="textarea mono"
            spellcheck="false"
            placeholder="---&#10;name: weather&#10;description: 查询城市天气，用户问天气时使用&#10;---&#10;&#10;# 怎么查天气&#10;……"
            aria-label="SKILL.md 内容"
          />
          <p class="caption">开头的 name 和 description 必填。只有一个文件，适合纯说明类的 skill。</p>
        </template>
        <div class="install__btns">
          <button class="btn btn-primary" :disabled="!canInstall || installing">{{ installing ? '正在安装' : '安装' }}</button>
        </div>
      </form>

      <div v-if="candidates.length" class="candidates">
        <p class="caption">这里面有 {{ candidates.length }} 个 skill，选一个安装：</p>
        <div v-for="c in candidates" :key="c.path" class="candidate">
          <div class="candidate__text">
            <p class="name">{{ c.name }} <span class="caption">{{ c.path || '（根目录）' }}</span></p>
            <p class="caption desc">{{ c.description }}</p>
          </div>
          <button type="button" class="btn btn-sm" :disabled="installing" @click="install(c.path)">安装</button>
        </div>
      </div>

      <div v-if="skills === null" class="skeleton skel" aria-hidden="true" />
      <p v-else-if="!skills.length" class="caption empty">还没有安装 skill。</p>
      <ul v-else class="items">
        <li v-for="s in skills" :key="s.id" class="item" :class="{ 'is-off': !s.enabled }">
          <div class="item__main">
            <button type="button" class="name link" @click="openSkill(s)">{{ s.name }}</button>
            <p class="caption desc">{{ s.description }}</p>
            <p class="caption meta">
              {{ sourceLabel(s) }} · {{ s.files }} 个文件 · {{ formatSize(s.size) }} · {{ s.updated_at }}
              <a v-if="s.source_url" :href="s.source_url" target="_blank" rel="noopener">来源</a>
            </p>
          </div>
          <div class="item__ctl">
            <label class="check-row"><input type="checkbox" :checked="s.network" @change="updateSkill(s, { network: !s.network })" /> 脚本可联网</label>
            <label class="check-row"><input type="checkbox" :checked="s.enabled" @change="updateSkill(s, { enabled: !s.enabled })" /> 启用</label>
            <button type="button" class="btn btn-sm btn-danger-lite" @click="removing = { kind: 'skill', id: s.id, name: s.name }">卸载</button>
          </div>
        </li>
      </ul>
    </section>

    <section class="panel panel--inset">
      <div class="head">
        <h2>插件（MCP）</h2>
        <button type="button" class="btn btn-sm" @click="showAdd = true">添加插件</button>
      </div>
      <p class="muted intro">
        MCP 插件给布谷提供额外的工具，比如地图、天气、知识库。可以接远程的插件服务（填地址），也可以在服务器上运行一个插件程序（填命令）。API Key 等密钥加密保存。
      </p>
      <div v-if="plugins === null" class="skeleton skel" aria-hidden="true" />
      <p v-else-if="!plugins.length" class="caption empty">还没有添加插件。</p>
      <ul v-else class="items">
        <li v-for="p in plugins" :key="p.id" class="item" :class="{ 'is-off': !p.enabled }">
          <div class="item__main">
            <p class="name">
              <span class="dot" :class="p.connected ? 'is-ok' : p.connecting ? 'is-wait' : p.enabled ? 'is-bad' : ''" />
              {{ p.name }}
              <span class="caption">{{ p.transport }}{{ p.server ? ` · ${p.server}` : '' }}</span>
            </p>
            <p class="caption mono where">{{ where(p) }}</p>
            <p v-if="p.header_keys?.length || p.env_keys?.length" class="caption">密钥：{{ [...(p.header_keys ?? []), ...(p.env_keys ?? [])].join('、') }}</p>
            <p v-if="p.connected" class="caption">
              {{ p.tools.length }} 个工具：{{ p.tools.map((t) => t.name).join('、') || '无' }}
            </p>
            <p v-else-if="p.connecting" class="caption">正在连接…</p>
            <p v-else-if="p.error && p.enabled" class="caption err">{{ p.error }}</p>
          </div>
          <div class="item__ctl">
            <button v-if="p.enabled" type="button" class="btn btn-sm" :disabled="reconnecting === p.id" @click="reconnect(p)">
              {{ reconnecting === p.id ? '正在连接' : '重新连接' }}
            </button>
            <label class="check-row"><input type="checkbox" :checked="p.enabled" @change="setPlugin(p, !p.enabled)" /> 启用</label>
            <button type="button" class="btn btn-sm btn-danger-lite" @click="removing = { kind: 'plugin', id: p.id, name: p.name }">删除</button>
          </div>
        </li>
      </ul>
    </section>

    <Modal :open="showAdd" title="添加插件" subtitle="名字用小写字母、数字和连字符，会出现在工具名里。" @close="showAdd = false">
      <form @submit.prevent="addPlugin">
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="pl-name">名字</label>
            <input id="pl-name" v-model="form.name" class="text-input" placeholder="amap" required />
          </div>
          <div class="field">
            <label class="field__label" for="pl-tr">接入方式</label>
            <select id="pl-tr" v-model="form.transport" class="text-input">
              <option value="http">远程 · Streamable HTTP</option>
              <option value="sse">远程 · SSE（旧版）</option>
              <option value="stdio">本机命令 · stdio</option>
            </select>
          </div>
        </div>
        <template v-if="form.transport !== 'stdio'">
          <div class="field">
            <label class="field__label" for="pl-url">地址</label>
            <input id="pl-url" v-model="form.url" class="text-input" placeholder="https://example.com/mcp" required />
          </div>
          <div class="field">
            <label class="field__label" for="pl-h">请求头（可选，每行一个「名字: 值」）</label>
            <textarea id="pl-h" v-model="form.headers" class="textarea mono small" placeholder="Authorization: Bearer xxxx" spellcheck="false" />
          </div>
        </template>
        <template v-else>
          <div class="field">
            <label class="field__label" for="pl-cmd">命令</label>
            <input id="pl-cmd" v-model="form.command" class="text-input mono" placeholder="npx" required />
          </div>
          <div class="field">
            <label class="field__label" for="pl-args">参数（每行一个）</label>
            <textarea id="pl-args" v-model="form.args" class="textarea mono small" placeholder="-y&#10;@amap/amap-maps-mcp-server" spellcheck="false" />
          </div>
          <div class="field">
            <label class="field__label" for="pl-env">环境变量（可选，每行一个「名字=值」）</label>
            <textarea id="pl-env" v-model="form.env" class="textarea mono small" placeholder="AMAP_MAPS_API_KEY=xxxx" spellcheck="false" />
          </div>
          <p class="caption">命令在服务器上以布谷的身份运行，只添加你信任的插件。npx、uvx 第一次运行要下载，可能要等一会儿。</p>
        </template>
        <p v-if="addError" class="form-flash form-flash--error">{{ addError }}</p>
        <div class="modal-actions">
          <button type="button" class="btn" @click="showAdd = false">取消</button>
          <button class="btn btn-primary" :disabled="adding">{{ adding ? '正在添加' : '添加' }}</button>
        </div>
      </form>
    </Modal>

    <Modal :open="!!detail" :title="detail?.skill.name ?? ''" :subtitle="detail?.skill.description" wide @close="detail = null">
      <template v-if="detail">
        <pre class="body">{{ detail.body }}</pre>
        <p class="caption">文件：{{ detail.files.join('、') }}</p>
      </template>
    </Modal>

    <Modal :open="!!removing" :title="`删除「${removing?.name}」？`" subtitle="所有用户都不能再用它，删除后要用的话得重新安装。" @close="removing = null">
      <div class="modal-actions">
        <button class="btn" @click="removing = null">取消</button>
        <button class="btn btn-danger" @click="confirmRemove">删除</button>
      </div>
    </Modal>
  </div>
</template>

<style scoped>
.ext { display: grid; gap: 16px; }
.intro { margin: 0 0 12px; font-size: 14px; line-height: 1.7; }
.head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.head h2 { margin: 0; }
.tabs { margin-bottom: 10px; }
.install { display: grid; gap: 6px; }
.install .caption { margin: 0; }
.install__btns { display: flex; justify-content: flex-end; }
.mono { font-family: var(--font-mono); font-size: 13px; }
.textarea.mono { min-height: 180px; }
.textarea.small { min-height: 72px; }
.skel { height: 64px; margin-top: 12px; border-radius: var(--radius-md); }
.empty { margin: 14px 0 0; }
.candidates { margin-top: 12px; padding: 10px 12px; border: 1px dashed var(--color-hairline-strong); border-radius: var(--radius-md); }
.candidate { display: flex; align-items: center; gap: 10px; padding: 8px 0; border-top: 1px solid var(--color-divider); }
.candidate:first-of-type { border-top: 0; }
.candidate__text { flex: 1; min-width: 0; }
.items { margin: 14px 0 0; padding: 0; list-style: none; }
.item { display: flex; align-items: flex-start; gap: 12px; padding: 12px 0; border-top: 1px solid var(--color-divider); }
.item.is-off .item__main { opacity: .55; }
.item__main { flex: 1; min-width: 0; }
.item__main p, .candidate p { margin: 0 0 2px; }
.item__ctl { display: flex; align-items: center; flex-wrap: wrap; justify-content: flex-end; gap: 4px 12px; }
.item__ctl .check-row { margin: 0; white-space: nowrap; }
.name { display: flex; align-items: center; gap: 6px; color: var(--color-ink); font-size: 14px; font-weight: 500; }
.link { padding: 0; text-align: left; font-family: var(--font-mono); text-decoration: underline; text-decoration-color: var(--color-hairline-strong); text-underline-offset: 3px; }
.desc { overflow-wrap: anywhere; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.where { overflow-wrap: anywhere; }
.meta a { color: var(--color-accent-active); }
.err { color: var(--color-danger); white-space: pre-wrap; overflow-wrap: anywhere; display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--color-hairline-strong); flex-shrink: 0; }
.dot.is-ok { background: var(--color-success); }
.dot.is-wait { background: var(--color-warning); }
.dot.is-bad { background: var(--color-danger); }
.body { max-height: 60vh; overflow: auto; padding: 12px; border-radius: var(--radius-md); background: var(--color-canvas-soft); font-size: 13px; white-space: pre-wrap; overflow-wrap: anywhere; }
@media (max-width: 640px) {
  .item { flex-direction: column; }
  .item__ctl { justify-content: flex-start; }
}
</style>
