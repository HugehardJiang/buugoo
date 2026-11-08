<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, ApiError, type ModelInput, type ModelView, type ProviderInput, type ProviderView, type RemoteModel, type TestResult, type ThinkingStyle } from '../api'
import { session } from '../session'
import { toast } from '../toast'
import Modal from './Modal.vue'
import ModelIcon from './ModelIcon.vue'
import { shortName } from '../vendors'

const models = ref<ModelView[]>([])
const providers = ref<ProviderView[]>([])
const error = ref('')
const editing = ref<ModelView | null>(null)
const formOpen = ref(false)
const saving = ref(false)
const testing = ref(false)
const testResult = ref<TestResult | null>(null)
const formError = ref('')
const removing = ref<ModelView | null>(null)
const rowTest = reactive<Record<number, string>>({})

const isMulti = computed(() => session.meta?.mode === 'multi')
const isAdmin = computed(() => !!session.user?.is_admin)

const blank = (): ModelInput => ({
  name: '',
  base_url: '',
  api_key: '',
  model: '',
  thinking_style: 'none',
  vision: false,
  tools: true,
  thinking_model: '',
  temperature: 0.8,
  max_tokens: 0,
  context_tokens: 0,
  public: false,
  restricted: false,
})
// 上下文长度在表单里以十万 token 为单位填写，留空表示自动识别。
// 注意：type="number" 的输入框经 v-model 拿到的是数字（清空时是空字符串），不能直接当字符串用。
const contextUnits = ref<string | number>('')
watch(contextUnits, (v) => {
  const n = typeof v === 'number' ? v : parseFloat(String(v ?? ''))
  form.context_tokens = Number.isFinite(n) && n > 0 ? Math.round(n * 100000) : 0
})
const sourceLabels: Record<string, string> = { manual: '手动填写', api: '服务商接口', table: '常见模型表', default: '识别不出，按默认' }
function wan(n: number) {
  return n >= 10000 ? `${Math.round(n / 1000) / 10} 万` : String(n)
}
const form = reactive<ModelInput>(blank())

// 常见服务的预设，选中后自动填写地址、模型名与思考方式，仍可修改。
const presets: { label: string; v: Partial<ModelInput> }[] = [
  { label: 'DeepSeek', v: { name: 'DeepSeek', base_url: 'https://api.deepseek.com/v1', model: 'deepseek-flash', thinking_style: 'deepseek', thinking_model: '', vision: true, tools: true } },
  { label: '阿里云百炼', v: { name: '通义千问', base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', model: 'qwen-plus', thinking_style: 'enable_thinking', thinking_model: '', vision: false, tools: true } },
  { label: '硅基流动', v: { name: '硅基流动', base_url: 'https://api.siliconflow.cn/v1', model: 'Qwen/Qwen3-32B', thinking_style: 'enable_thinking', thinking_model: '', vision: false, tools: true } },
  { label: 'OpenAI', v: { name: 'OpenAI', base_url: 'https://api.openai.com/v1', model: 'gpt-5-mini', thinking_style: 'reasoning_effort', thinking_model: '', vision: true, tools: true } },
  // OpenCode Go：所有文本模型都能走这个 OpenAI 兼容接口；模型名见 opencode.ai/docs/go（例如 glm-5.2、kimi-k3、deepseek-v4-pro）。
  // 一般用上面的「OpenCode Go」一键添加，这里留着给想手动填的人。
  { label: 'OpenCode Go', v: { name: 'OpenCode Go', base_url: 'https://opencode.ai/inference/go/openai/v1', model: 'glm-5.2', thinking_style: 'reasoning_effort', thinking_model: '', vision: false, tools: true } },
]

// OpenCode Go 一键添加：填自己的 Key、勾选模型，每个模型添加成一个「我的」模型（用自己的 Key 不计费）。
type OcModel = { id: string; name: string; vision?: boolean; free?: boolean; note?: string }
const oc = reactive({ open: false, key: '', models: [] as OcModel[], mine: [] as string[], picked: new Set<string>(), busy: false, error: '', date: '' })
async function openOpenCode() {
  Object.assign(oc, { open: true, key: '', error: '', picked: new Set<string>() })
  try {
    const r = await api.openCodeCatalog()
    Object.assign(oc, { models: r.models, mine: r.mine, date: r.date })
  } catch (e) {
    oc.error = e instanceof ApiError ? e.message : '加载失败'
  }
}
function ocToggle(id: string) {
  const s = new Set(oc.picked)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  oc.picked = s
}
const ocNeedKey = computed(() => [...oc.picked].some((id) => !oc.mine.includes(id)))
const ocPickedMine = computed(() => [...oc.picked].filter((id) => oc.mine.includes(id)))
async function removeOpenCode() {
  const ids = ocPickedMine.value
  if (!ids.length || oc.busy) return
  if (!confirm(`删除选中的 ${ids.length} 个已添加的模型？正在用它们的对话会改回默认模型。`)) return
  oc.busy = true
  oc.error = ''
  try {
    const r = await api.removeMyOpenCode(ids)
    toast(`删除了 ${r.removed} 个模型`)
    oc.mine = oc.mine.filter((id) => !ids.includes(id))
    oc.picked = new Set([...oc.picked].filter((id) => !ids.includes(id)))
    await load()
  } catch (e) {
    oc.error = e instanceof ApiError ? e.message : '删除失败'
  } finally {
    oc.busy = false
  }
}
async function saveOpenCode() {
  if (!oc.picked.size || oc.busy) return
  oc.busy = true
  oc.error = ''
  try {
    const r = await api.addMyOpenCode(oc.key, [...oc.picked])
    toast(`添加了 ${r.added} 个${r.updated ? `，更新了 ${r.updated} 个` : ''}`)
    oc.open = false
    await load()
  } catch (e) {
    oc.error = e instanceof ApiError ? e.message : '添加失败'
  } finally {
    oc.busy = false
  }
}
const isOpenCode = computed(() => /(^|\.)opencode\.ai(\/|:|$)/.test(form.base_url.replace(/^https?:\/\//, '')))

const styles: { v: ThinkingStyle; l: string; hint: string }[] = [
  { v: 'none', l: '不支持思考', hint: '对话中不显示思考等级。' },
  { v: 'deepseek', l: 'DeepSeek', hint: 'thinking 开关 + reasoning_effort，对话中可选关 / 低 / 高 / 最高。' },
  { v: 'reasoning_effort', l: 'reasoning_effort 参数', hint: 'OpenAI o 系列 / GPT-5 等，低、中、高对应 low / medium / high。' },
  { v: 'enable_thinking', l: 'enable_thinking 参数', hint: '通义千问 Qwen3、硅基流动等，等级对应不同的思考预算。' },
  { v: 'model_switch', l: '换用推理模型', hint: '开启思考时改用另一个推理模型，只有开和关。' },
]
const styleHint = computed(() => styles.find((s) => s.v === form.thinking_style)?.hint ?? '')

function scopeLabel(m: ModelView) {
  if (m.scope === 'builtin') return '配置文件'
  if (m.scope === 'personal') return '我的'
  return isMulti.value ? '公共' : ''
}

function fail(e: unknown, fallback = '操作失败') {
  error.value = e instanceof ApiError ? e.message : fallback
}

async function load() {
  try {
    const r = await api.models()
    models.value = r.models
    providers.value = r.providers ?? []
  } catch (e) {
    fail(e, '加载模型失败')
  }
}

function openForm(m: ModelView | null) {
  editing.value = m
  Object.assign(form, blank())
  if (m) {
    Object.assign(form, {
      name: m.name, base_url: m.base_url, model: m.model, thinking_style: m.thinking_style,
      thinking_model: m.thinking_model, temperature: m.temperature, max_tokens: m.max_tokens, public: m.scope === 'public',
      vision: m.vision, tools: m.tools, context_tokens: m.context_tokens, restricted: m.restricted, provider_id: m.provider_id || undefined,
    })
  }
  contextUnits.value = form.context_tokens > 0 ? String(form.context_tokens / 100000) : ''
  testResult.value = null
  formError.value = ''
  formOpen.value = true
}

function applyPreset(p: Partial<ModelInput>) {
  Object.assign(form, p)
  testResult.value = null
}

async function test() {
  testing.value = true
  testResult.value = null
  formError.value = ''
  try {
    testResult.value = await api.testModel(editing.value?.id ?? 0, { ...form })
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : '测试失败'
  } finally {
    testing.value = false
  }
}

async function save() {
  saving.value = true
  formError.value = ''
  try {
    if (editing.value) await api.updateModel(editing.value.id, { ...form })
    else await api.createModel({ ...form })
    formOpen.value = false
    toast(editing.value ? '模型已保存' : '模型已添加，可以在聊天时选用')
    await load()
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function setDefault(m: ModelView) {
  try {
    await api.setDefaultModel(m.id)
    toast(`已把「${m.name}」设为默认模型`)
    await load()
  } catch (e) {
    fail(e)
  }
}

async function testRow(m: ModelView) {
  rowTest[m.id] = '测试中…'
  try {
    const r = await api.testModel(m.id)
    rowTest[m.id] = r.ok ? `可用 · ${r.latency_ms} ms` : `失败：${r.error}`
  } catch (e) {
    rowTest[m.id] = e instanceof ApiError ? `失败：${e.message}` : '失败'
  }
}

async function remove() {
  const m = removing.value
  if (!m) return
  try {
    await api.deleteModel(m.id)
    removing.value = null
    toast('模型已删除，使用它的对话改回默认模型')
    await load()
  } catch (e) {
    fail(e)
  }
}

// 默认模型的设置权限：个人模型自己可设；公共模型与配置文件模型只有管理员可设（设为全站默认）。
function canSetDefault(m: ModelView) {
  if (m.is_default) return false
  return m.scope === 'builtin' ? isAdmin.value : m.editable
}

// 普通用户把自己的模型设为默认后，可以改回站点默认。
const hasPersonalDefault = computed(() => models.value.some((m) => m.is_default && m.scope === 'personal'))

async function clearDefault() {
  try {
    await api.clearDefaultModel()
    toast('已改回站点默认模型')
    await load()
  } catch (e) {
    fail(e)
  }
}

// ---------- 供应商 ----------

// groups 把模型按供应商分组：配置文件的模型、公共供应商、自己的供应商。空的供应商也列出来，方便往里加模型。
const groups = computed(() => {
  const out: { provider: ProviderView | null; label: string; items: ModelView[] }[] = []
  const builtin = models.value.filter((m) => m.scope === 'builtin')
  if (builtin.length) out.push({ provider: null, label: '配置文件', items: builtin })
  for (const p of providers.value) out.push({ provider: p, label: p.name, items: models.value.filter((m) => m.provider_id === p.id) })
  const loose = models.value.filter((m) => m.scope !== 'builtin' && !providers.value.some((p) => p.id === m.provider_id))
  if (loose.length) out.push({ provider: null, label: '其他', items: loose })
  return out
})

const providerPresets: { label: string; base_url: string }[] = [
  { label: 'DeepSeek', base_url: 'https://api.deepseek.com/v1' },
  { label: '阿里云百炼', base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1' },
  { label: '硅基流动', base_url: 'https://api.siliconflow.cn/v1' },
  { label: 'OpenAI', base_url: 'https://api.openai.com/v1' },
  { label: 'OpenRouter', base_url: 'https://openrouter.ai/api/v1' },
  { label: '火山方舟', base_url: 'https://ark.cn-beijing.volces.com/api/v3' },
  { label: '智谱', base_url: 'https://open.bigmodel.cn/api/paas/v4' },
  { label: 'Moonshot', base_url: 'https://api.moonshot.cn/v1' },
  { label: 'OpenCode Go', base_url: 'https://opencode.ai/inference/go/openai/v1' },
]
const pf = reactive<ProviderInput & { open: boolean; editing: ProviderView | null; busy: boolean; error: string }>({
  open: false, editing: null, busy: false, error: '', name: '', base_url: '', api_key: '', public: false,
})
function openProvider(p: ProviderView | null) {
  Object.assign(pf, { open: true, editing: p, busy: false, error: '', name: p?.name ?? '', base_url: p?.base_url ?? '', api_key: '', public: isMulti.value && isAdmin.value && !p })
}
function providerPreset(x: { label: string; base_url: string }) {
  pf.name = x.label
  pf.base_url = x.base_url
}
async function saveProvider() {
  if (pf.busy) return
  pf.busy = true
  pf.error = ''
  try {
    const input = { name: pf.name, base_url: pf.base_url, api_key: pf.api_key, public: pf.public }
    if (pf.editing) {
      await api.updateProvider(pf.editing.id, input)
      toast('供应商已保存，下面的模型一起生效')
      pf.open = false
      await load()
    } else {
      const p = await api.createProvider(input)
      pf.open = false
      await load()
      openAdd(p) // 建好就接着挑模型
    }
  } catch (e) {
    pf.error = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    pf.busy = false
  }
}
const removingProvider = ref<ProviderView | null>(null)
async function removeProvider() {
  const p = removingProvider.value
  if (!p) return
  try {
    const r = await api.deleteProvider(p.id)
    removingProvider.value = null
    toast(`已删除「${p.name}」${r.removed ? `和下面的 ${r.removed} 个模型` : ''}`)
    await load()
  } catch (e) {
    fail(e)
  }
}

// 在供应商下面批量添加模型：读供应商的模型列表勾选，读不到时手动填模型名。
type Pick = { id: string; vision: boolean; tools: boolean; thinking_style: ThinkingStyle | ''; context_tokens?: number }
const add = reactive({
  open: false, provider: null as ProviderView | null, loading: false, error: '', list: [] as RemoteModel[], query: '',
  picked: new Map<string, Pick>(), manual: '', restricted: false, busy: false, showAll: false,
})
async function openAdd(p: ProviderView) {
  Object.assign(add, { open: true, provider: p, loading: true, error: '', list: [], query: '', picked: new Map(), manual: '', restricted: false, busy: false, showAll: false })
  try {
    add.list = (await api.remoteModels(p.id)).models
  } catch (e) {
    add.error = e instanceof ApiError ? e.message : '读取模型列表失败'
  } finally {
    add.loading = false
  }
}
const addShown = computed(() => {
  const q = add.query.trim().toLowerCase()
  return add.list.filter((m) => (add.showAll || m.chat || add.picked.has(m.id)) && (!q || m.id.toLowerCase().includes(q)))
})
const hiddenCount = computed(() => add.list.filter((m) => !m.chat).length)
function togglePick(m: RemoteModel) {
  const next = new Map(add.picked)
  if (next.has(m.id)) next.delete(m.id)
  else next.set(m.id, { id: m.id, vision: m.vision, tools: m.tools, thinking_style: m.thinking_style, context_tokens: m.context_tokens })
  add.picked = next
}
function addManual() {
  const ids = add.manual.split(/[\s,，]+/).map((x) => x.trim()).filter(Boolean)
  const next = new Map(add.picked)
  for (const id of ids) {
    if (!next.has(id)) next.set(id, { id, vision: false, tools: true, thinking_style: '' })
    if (!add.list.some((m) => m.id === id)) add.list.unshift({ id, added: false, chat: true, vision: false, tools: true, thinking_style: 'none' })
  }
  add.picked = next
  add.manual = ''
}
async function saveAdd() {
  if (!add.provider || !add.picked.size || add.busy) return
  add.busy = true
  add.error = ''
  try {
    const items = [...add.picked.values()].map((x) => ({ model: x.id, vision: x.vision, tools: x.tools, thinking_style: x.thinking_style as ThinkingStyle, context_tokens: x.context_tokens ?? 0 }))
    const r = await api.addProviderModels(add.provider.id, items, add.restricted)
    toast(r.added ? `添加了 ${r.added} 个模型` : '选中的模型都已经添加过了')
    add.open = false
    await load()
  } catch (e) {
    add.error = e instanceof ApiError ? e.message : '添加失败'
  } finally {
    add.busy = false
  }
}

onMounted(load)
defineExpose({ load })
</script>

<template>
  <section id="models" class="panel panel--inset">
    <div class="panel-head">
      <h2>模型</h2>
      <span class="head-actions">
        <button class="btn" @click="openOpenCode">OpenCode Go</button>
        <button class="btn btn-primary" @click="openProvider(null)">添加供应商</button>
      </span>
    </div>
    <p class="muted intro">
      <template v-if="isMulti">
        可以添加你自己的模型（填自己的 API Key），只有你能看到和使用，用它聊天不占每日限额。<template v-if="isAdmin">管理员还可以添加所有人都能用的公共模型。</template>
      </template>
      <template v-else>支持 OpenAI 兼容接口的服务，如 DeepSeek、通义千问、硅基流动、OpenAI、本地 Ollama。</template>
      先添加供应商（接口地址和 API Key 只填一次），再在它下面勾选要用的模型。API Key 加密保存，添加后不会再显示明文。聊天时可以为每段对话单独选择模型。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>

    <p v-if="!models.length && !providers.length" class="empty muted">还没有可用的模型，先点「添加供应商」。</p>
    <section v-for="g in groups" :key="g.provider ? `p${g.provider.id}` : g.label" class="prov">
      <div class="prov__head">
        <ModelIcon model="" :name="g.label" :size="22" />
        <div class="prov__title">
          <strong>{{ g.label }}</strong>
          <span v-if="g.provider && isMulti" class="badge-pill">{{ g.provider.scope === 'public' ? '公共' : '我的' }}</span>
          <p v-if="g.provider" class="caption">{{ g.provider.base_url }} · Key {{ g.provider.key_mask }} · {{ g.items.length }} 个模型</p>
          <p v-else-if="g.label === '配置文件'" class="caption">来自 config.yaml 的 llm.chat，需在配置文件中修改</p>
        </div>
        <div v-if="g.provider?.editable" class="prov__actions">
          <button class="btn btn-sm btn-primary" @click="openAdd(g.provider)">添加模型</button>
          <button class="btn btn-sm" @click="openProvider(g.provider)">修改</button>
          <button class="btn btn-sm btn-danger-lite" @click="removingProvider = g.provider">删除</button>
        </div>
      </div>
      <p v-if="g.provider && !g.items.length" class="caption prov__empty">还没有模型，点「添加模型」从这个供应商的模型列表里勾选。</p>
    <div v-for="m in g.items" :key="m.id" class="model-row">
      <div class="model-row__main">
        <div class="model-row__title">
          <ModelIcon :model="m.model" :name="m.name" :size="18" />
          <strong :title="m.name">{{ m.provider_id ? shortName(m.name) : m.name }}</strong>
          <span v-if="m.is_default" class="chip-note">默认</span>
          <span v-if="scopeLabel(m) && !m.provider_id && m.scope !== 'builtin'" class="badge-pill">{{ scopeLabel(m) }}</span>
          <span v-if="m.restricted" class="badge-pill" title="只有管理员在用户管理里授权的用户能用">仅限授权用户</span>
        </div>
        <p class="caption">
          {{ m.model }} · 上下文 {{ wan(m.context.tokens) }}<template v-if="m.thinking_style !== 'none'"> · 可调思考</template><template v-if="m.vision"> · 能看图</template><template v-if="m.tools"> · 可用工具</template><template v-if="!m.provider_id"> · {{ m.base_url }} · Key {{ m.key_mask || '未填写' }}</template>
        </p>
        <p v-if="rowTest[m.id]" class="caption test-line">{{ rowTest[m.id] }}</p>
      </div>
      <div class="model-row__actions">
        <button v-if="canSetDefault(m)" class="btn btn-ghost" @click="setDefault(m)">设为默认</button>
        <button v-if="!isMulti || isAdmin || m.scope === 'personal'" class="btn" @click="testRow(m)">测试</button>
        <template v-if="m.editable && m.scope !== 'builtin'">
          <button class="btn" @click="openForm(m)">编辑</button>
          <button class="btn btn-danger-lite" @click="removing = m">删除</button>
        </template>
      </div>
    </div>
    </section>
    <p v-if="hasPersonalDefault && isMulti" class="caption foot">
      正在用你自己的模型作为默认。<button class="text-btn" @click="clearDefault">改回站点默认</button>
    </p>

    <Modal
      :open="formOpen"
      :title="editing ? '编辑模型' : '添加模型'"
      subtitle="填写 OpenAI 兼容接口的地址、API Key 与模型名。保存前可以先测试一下能不能连通。"
      @close="formOpen = false"
    >
      <form @submit.prevent="save">
        <div v-if="!editing" class="presets">
          <span class="caption">快速填写</span>
          <button v-for="p in presets" :key="p.label" type="button" class="badge-pill preset" @click="applyPreset(p.v)">{{ p.label }}</button>
        </div>
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="m-name">名称</label>
            <input id="m-name" v-model="form.name" class="text-input" maxlength="64" required placeholder="例如：DeepSeek" />
          </div>
          <div class="field">
            <label class="field__label" for="m-model">模型名</label>
            <input id="m-model" v-model="form.model" class="text-input" required placeholder="例如：deepseek-chat" />
          </div>
        </div>
        <p v-if="form.provider_id" class="caption">
          供应商：{{ providers.find((p) => p.id === form.provider_id)?.name }}（接口地址和 API Key 在供应商里改，下面的模型一起生效）
        </p>
        <div v-if="!form.provider_id" class="field">
          <label class="field__label" for="m-url">Base URL</label>
          <input id="m-url" v-model="form.base_url" class="text-input" required placeholder="https://api.deepseek.com/v1" inputmode="url" />
        </div>
        <div v-if="!form.provider_id" class="field">
          <label class="field__label" for="m-key">API Key</label>
          <input
            id="m-key"
            v-model="form.api_key"
            class="text-input"
            type="password"
            autocomplete="off"
            :required="!editing"
            :placeholder="editing ? `已保存 ${editing.key_mask}，留空表示不修改` : 'sk-…'"
          />
        </div>
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="m-style">思考方式</label>
            <select id="m-style" v-model="form.thinking_style" class="text-input">
              <option v-for="s in styles" :key="s.v" :value="s.v">{{ s.l }}</option>
            </select>
            <p class="field__hint">{{ styleHint }}</p>
          </div>
          <div v-if="form.thinking_style === 'model_switch'" class="field">
            <label class="field__label" for="m-tmodel">推理模型名</label>
            <input id="m-tmodel" v-model="form.thinking_model" class="text-input" required placeholder="deepseek-reasoner" />
          </div>
        </div>
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="m-temp">Temperature</label>
            <input id="m-temp" v-model.number="form.temperature" class="text-input" type="number" min="0" max="2" step="0.1" />
          </div>
          <div class="field">
            <label class="field__label" for="m-max">最大输出 token</label>
            <input id="m-max" v-model.number="form.max_tokens" class="text-input" type="number" min="0" max="2000000" step="1024" />
            <p class="field__hint">0 表示不限制，由服务决定（推荐）；想限制长度时再填，例如 8192、32768。</p>
          </div>
        </div>
        <div class="field">
          <label class="field__label" for="m-ctx">上下文长度</label>
          <div class="ctx-row">
            <input id="m-ctx" v-model="contextUnits" class="text-input" type="number" min="0" step="0.01" placeholder="留空自动识别" />
            <span class="caption">× 10 万 token<template v-if="form.context_tokens"> = {{ wan(form.context_tokens) }}</template></span>
          </div>
          <p class="field__hint">
            例如 1.28 表示 12.8 万。留空时自动识别（先问服务商接口，再查常见模型表），识别不出按 6.4 万算。
            <template v-if="editing && !form.context_tokens">现在识别的是 {{ wan(editing.context.tokens) }}（{{ sourceLabels[editing.context.source] }}）。</template>
          </p>
        </div>
        <label class="check-row">
          <input v-model="form.vision" type="checkbox" />
          <span>能看图（多模态）：图片直接交给这个模型；不勾选时，图片会先由其他能看图的模型转述成文字</span>
        </label>
        <label class="check-row">
          <input v-model="form.tools" type="checkbox" />
          <span>支持工具调用：可以自己决定运行 Python 代码做计算；DeepSeek 模型还能联网搜索和读取网页</span>
        </label>
        <label v-if="isMulti && isAdmin && !editing" class="check-row">
          <input v-model="form.public" type="checkbox" />
          <span>公共模型：所有用户都能使用，计入每日限额</span>
        </label>
        <label v-if="isMulti && isAdmin && (form.public || editing?.scope === 'public')" class="check-row">
          <input v-model="form.restricted" type="checkbox" />
          <span>仅限授权用户：只有在「管理后台 → 用户管理」里授权了「专属模型」的用户能用（管理员总能用）</span>
        </label>
        <p v-if="isOpenCode" class="caption oc-note">
          OpenCode Go 的模型名见 <a href="https://opencode.ai/docs/go/" target="_blank" rel="noopener noreferrer">官方文档</a>（例如 glm-5.2、kimi-k3、qwen3.8-flash、deepseek-v4-pro）。
          布谷会自动带上 OpenCode 要求的客户端标识和会话编号。注意：官方说明 Go 套餐是给编程助手用的，用在聊天上有被限制的风险；DeepSeek 模型要先在 OpenCode 控制台的隐私设置里把地区选成 Global。
        </p>

        <p v-if="testResult" class="form-flash" :class="testResult.ok ? 'form-flash--ok' : 'form-flash--error'">
          <template v-if="testResult.ok">连通了（{{ testResult.latency_ms }} ms）：{{ testResult.reply }}</template>
          <template v-else>连不上：{{ testResult.error }}</template>
        </p>
        <p v-if="formError" class="form-flash form-flash--error">{{ formError }}</p>

        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" :disabled="testing" @click="test">{{ testing ? '测试中…' : '测试连接' }}</button>
          <button type="button" class="btn" @click="formOpen = false">取消</button>
          <button class="btn btn-primary" :disabled="saving">{{ saving ? '正在保存' : '保存' }}</button>
        </div>
      </form>
    </Modal>

    <Modal :open="oc.open" title="添加 OpenCode Go 模型" subtitle="填上你自己的 OpenCode Go API Key，勾选要用的模型，一键添加。用自己的 Key 不计费。" wide @close="oc.open = false">
      <form @submit.prevent="saveOpenCode">
        <div class="field">
          <label class="field__label" for="oc-key">API Key</label>
          <input id="oc-key" v-model="oc.key" class="text-input" type="password" autocomplete="new-password"
            :placeholder="oc.mine.length ? '只更新已添加的模型时可以不填' : '在 OpenCode 控制台（opencode.ai/console）复制'" />
        </div>
        <p class="caption oc-note">
          官方说明 Go 套餐是给编程助手用的，用在聊天上有被限制的风险。DeepSeek 模型要先在 OpenCode 控制台 Settings › Privacy 里把地区选成 Global。
          模型列表 {{ oc.date }} 更新。
        </p>
        <div class="oc-list">
          <label v-for="m in oc.models" :key="m.id" class="oc-item" :class="{ 'is-on': oc.picked.has(m.id) }">
            <input type="checkbox" :checked="oc.picked.has(m.id)" @change="ocToggle(m.id)" />
            <ModelIcon :model="m.id" :name="m.name" :size="18" />
            <span>
              <strong>{{ m.name }}</strong>
              <span v-if="oc.mine.includes(m.id)" class="badge-pill">已添加</span>
              <span v-if="m.free" class="badge-pill">免费</span>
              <span v-if="m.vision" class="caption"> · 能看图</span>
              <span v-if="m.note" class="caption"><br />{{ m.note }}</span>
            </span>
          </label>
        </div>
        <p v-if="oc.error" class="form-flash form-flash--error">{{ oc.error }}</p>
        <div class="modal-actions">
          <button v-if="ocPickedMine.length" type="button" class="btn btn-danger-lite oc-remove" :disabled="oc.busy" @click="removeOpenCode">
            删除选中的 {{ ocPickedMine.length }} 个已添加模型
          </button>
          <button type="button" class="btn btn-ghost" @click="oc.open = false">取消</button>
          <button type="submit" class="btn btn-primary" :disabled="!oc.picked.size || oc.busy || (ocNeedKey && !oc.key.trim())">
            {{ oc.busy ? '正在添加' : `添加 ${oc.picked.size} 个模型` }}
          </button>
        </div>
      </form>
    </Modal>

    <Modal :open="pf.open" :title="pf.editing ? '修改供应商' : '添加供应商'" subtitle="填一次接口地址和 API Key，下面的模型都用它。支持所有 OpenAI 兼容接口。" @close="pf.open = false">
      <form @submit.prevent="saveProvider">
        <div v-if="!pf.editing" class="presets">
          <span class="caption">常见的</span>
          <button v-for="x in providerPresets" :key="x.label" type="button" class="badge-pill preset" @click="providerPreset(x)">{{ x.label }}</button>
        </div>
        <div class="field">
          <label class="field__label" for="p-url">Base URL</label>
          <input id="p-url" v-model="pf.base_url" class="text-input" required placeholder="https://api.deepseek.com/v1" inputmode="url" />
        </div>
        <div class="field">
          <label class="field__label" for="p-key">API Key</label>
          <input id="p-key" v-model="pf.api_key" class="text-input" type="password" autocomplete="new-password" :required="!pf.editing"
            :placeholder="pf.editing ? `已保存 ${pf.editing.key_mask}，留空表示不修改` : 'sk-…'" />
        </div>
        <div class="field">
          <label class="field__label" for="p-name">名称</label>
          <input id="p-name" v-model="pf.name" class="text-input" maxlength="64" placeholder="留空按地址自动起名，例如 DeepSeek" />
          <p class="field__hint">聊天时选模型会按这个名字分组显示。</p>
        </div>
        <label v-if="isMulti && isAdmin && !pf.editing" class="check-row">
          <input v-model="pf.public" type="checkbox" />
          <span>公共供应商：下面的模型所有用户都能用，按「计费」里的价格计入额度和余额；不勾就是只有你自己能用</span>
        </label>
        <p v-if="pf.editing" class="caption">改了地址或 Key，下面的 {{ models.filter((m) => m.provider_id === pf.editing?.id).length }} 个模型一起生效。</p>
        <p v-if="pf.error" class="form-flash form-flash--error">{{ pf.error }}</p>
        <div class="modal-actions">
          <button type="button" class="btn" @click="pf.open = false">取消</button>
          <button class="btn btn-primary" :disabled="pf.busy">{{ pf.busy ? '正在保存' : pf.editing ? '保存' : '下一步：选模型' }}</button>
        </div>
      </form>
    </Modal>

    <Modal :open="add.open" :title="`给「${add.provider?.name ?? ''}」添加模型`" subtitle="从供应商的模型列表里勾选，能力是按名字猜的，可以改；添加后还能逐个编辑。" wide @close="add.open = false">
      <form @submit.prevent="saveAdd">
        <div class="add-tools">
          <input v-model="add.query" class="text-input" type="search" placeholder="搜索模型名" aria-label="搜索模型名" />
          <label v-if="hiddenCount" class="check-row add-all"><input v-model="add.showAll" type="checkbox" /> 显示向量、语音等 {{ hiddenCount }} 个非聊天模型</label>
        </div>
        <p v-if="add.loading" class="caption">正在读取供应商的模型列表…</p>
        <p v-if="add.error" class="form-flash form-flash--error">{{ add.error }}</p>
        <div v-if="addShown.length" class="oc-list add-list">
          <div v-for="m in addShown" :key="m.id" class="oc-item" :class="{ 'is-on': add.picked.has(m.id), 'is-added': m.added }">
            <input type="checkbox" :checked="add.picked.has(m.id) || m.added" :disabled="m.added" :aria-label="m.id" @change="togglePick(m)" />
            <ModelIcon :model="m.id" :size="18" />
            <span class="add-item__main">
              <strong class="add-item__id" @click="!m.added && togglePick(m)">{{ m.id }}</strong>
              <span v-if="m.added" class="badge-pill">已添加</span>
              <span v-if="add.picked.has(m.id)" class="add-item__caps">
                <label><input v-model="add.picked.get(m.id)!.vision" type="checkbox" /> 看图</label>
                <label><input v-model="add.picked.get(m.id)!.tools" type="checkbox" /> 工具</label>
              </span>
              <span v-else-if="!m.added" class="caption">{{ [m.vision && '看图', m.thinking_style !== 'none' && '可调思考', m.context_tokens && `${wan(m.context_tokens)} 上下文`].filter(Boolean).join(' · ') }}</span>
            </span>
          </div>
        </div>
        <p v-else-if="!add.loading && add.list.length" class="caption">没有叫「{{ add.query }}」的模型。</p>
        <div class="add-manual">
          <input v-model="add.manual" class="text-input" placeholder="列表里没有？手动填模型名，多个用逗号或空格隔开" aria-label="手动填模型名" @keydown.enter.prevent="addManual" />
          <button type="button" class="btn btn-sm" :disabled="!add.manual.trim()" @click="addManual">加入</button>
        </div>
        <label v-if="isMulti && isAdmin && add.provider?.scope === 'public'" class="check-row">
          <input v-model="add.restricted" type="checkbox" />
          <span>仅限授权用户：只有在「管理后台 → 用户管理」里授权了「专属模型」的用户能用</span>
        </label>
        <div class="modal-actions">
          <button type="button" class="btn" @click="add.open = false">取消</button>
          <button class="btn btn-primary" :disabled="!add.picked.size || add.busy">{{ add.busy ? '正在添加' : `添加 ${add.picked.size} 个模型` }}</button>
        </div>
      </form>
    </Modal>

    <Modal v-if="removingProvider" :title="`删除供应商「${removingProvider.name}」？`" subtitle="下面的模型会一起删除，正在用它们的对话改回默认模型，聊天记录不受影响。" @close="removingProvider = null">
      <div class="modal-actions">
        <button class="btn" @click="removingProvider = null">取消</button>
        <button class="btn btn-danger" @click="removeProvider">删除</button>
      </div>
    </Modal>

    <Modal v-if="removing" :title="`删除「${removing.name}」？`" subtitle="使用这个模型的对话会改回默认模型，聊天记录不受影响。" @close="removing = null">
      <div class="modal-actions">
        <button class="btn" @click="removing = null">取消</button>
        <button class="btn btn-danger" @click="remove">删除模型</button>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
.ctx-row { display: flex; align-items: center; gap: 10px; }
.ctx-row .text-input { max-width: 160px; }
.panel-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
.panel-head h2 { margin: 0; }
.intro { margin: 0 0 16px; font-size: 14px; line-height: 1.65; }
.empty { margin: 0; padding: 12px 0; }
.model-row { display: flex; align-items: center; flex-wrap: wrap; gap: 10px 16px; padding: 14px 0; border-top: 1px solid var(--color-divider); }
.model-row__main { flex: 1; min-width: 220px; }
.model-row__title { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-bottom: 4px; }
.model-row__title strong { font-weight: 500; }
.model-row .caption { margin: 0; overflow-wrap: anywhere; }
.test-line { color: var(--color-text-secondary); }
.model-row__actions { display: flex; flex-wrap: wrap; gap: 6px; }
.foot { margin: 8px 0 0; }
.text-btn { padding: 0; color: var(--color-accent-active); font-size: inherit; text-decoration: underline; text-underline-offset: 3px; }
.presets { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin-bottom: 18px; }
.preset { cursor: pointer; min-height: 28px; padding: 0 10px; }
.preset:hover { border-color: var(--color-muted); color: var(--color-ink); }
.oc-remove { margin-right: auto; }
.head-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.oc-note { margin: 4px 0 10px; line-height: 1.7; }
.oc-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 6px; max-height: 46vh; overflow-y: auto; margin-bottom: 12px; }
.oc-item { display: flex; align-items: flex-start; gap: 8px; padding: 8px 10px; border: 1px solid var(--color-divider); border-radius: var(--radius-md); font-size: 13px; cursor: pointer; line-height: 1.5; }
.oc-item.is-on { border-color: var(--color-accent); background: var(--color-accent-tint, rgba(91, 122, 74, .08)); }
.oc-item input { margin-top: 3px; }
.prov { margin-top: 14px; padding: 14px 16px 2px; border: 1px solid var(--color-divider); border-radius: var(--radius-lg); }
.prov__head { display: flex; align-items: flex-start; flex-wrap: wrap; gap: 10px; padding-bottom: 10px; }
.prov__title { flex: 1; min-width: 200px; }
.prov__title strong { font-size: 15px; font-weight: 600; margin-right: 6px; }
.prov__title .caption { margin: 2px 0 0; overflow-wrap: anywhere; }
.prov__actions { display: flex; flex-wrap: wrap; gap: 6px; }
.prov__empty { margin: 0 0 12px; }
.add-tools { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 14px; margin-bottom: 10px; }
.add-tools .text-input { flex: 1; min-width: 180px; }
.add-all { margin: 0; font-size: 13px; }
.add-list { grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); }
.oc-item.is-added { opacity: .6; cursor: default; }
.add-item__main { display: grid; gap: 2px; min-width: 0; }
.add-item__id { overflow-wrap: anywhere; cursor: pointer; font-weight: 500; }
.add-item__caps { display: flex; gap: 10px; font-size: 12px; color: var(--color-text-secondary); }
.add-item__caps label { display: inline-flex; align-items: center; gap: 3px; cursor: pointer; }
.add-manual { display: flex; gap: 8px; margin: 4px 0 12px; }
.add-manual .text-input { flex: 1; min-width: 0; }
</style>
