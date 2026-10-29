<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, ApiError, type ModelInput, type ModelView, type TestResult, type ThinkingStyle } from '../api'
import { session } from '../session'
import { toast } from '../toast'
import Modal from './Modal.vue'

const models = ref<ModelView[]>([])
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
const contextUnits = ref('')
watch(contextUnits, (v) => {
  const n = parseFloat(v)
  form.context_tokens = v.trim() === '' || !(n > 0) ? 0 : Math.round(n * 100000)
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
  { label: 'OpenCode Go', v: { name: 'OpenCode Go', base_url: 'https://opencode.ai/inference/go/openai/v1', model: 'glm-5.2', thinking_style: 'none', thinking_model: '', vision: false, tools: true } },
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
    models.value = (await api.models()).models
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
      vision: m.vision, tools: m.tools, context_tokens: m.context_tokens, restricted: m.restricted,
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

onMounted(load)
defineExpose({ load })
</script>

<template>
  <section id="models" class="panel panel--inset">
    <div class="panel-head">
      <h2>模型</h2>
      <span class="head-actions">
        <button class="btn" @click="openOpenCode">OpenCode Go</button>
        <button class="btn btn-primary" @click="openForm(null)">添加模型</button>
      </span>
    </div>
    <p class="muted intro">
      <template v-if="isMulti">
        可以添加你自己的模型（填自己的 API Key），只有你能看到和使用，用它聊天不占每日限额。<template v-if="isAdmin">管理员还可以添加所有人都能用的公共模型。</template>
      </template>
      <template v-else>支持 OpenAI 兼容接口的服务，如 DeepSeek、通义千问、硅基流动、OpenAI、本地 Ollama。</template>
      API Key 加密保存，添加后不会再显示明文。聊天时可以为每段对话单独选择模型。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>

    <p v-if="!models.length" class="empty muted">还没有可用的模型。</p>
    <div v-for="m in models" :key="m.id" class="model-row">
      <div class="model-row__main">
        <div class="model-row__title">
          <strong>{{ m.name }}</strong>
          <span v-if="m.is_default" class="chip-note">默认</span>
          <span v-if="scopeLabel(m)" class="badge-pill">{{ scopeLabel(m) }}</span>
          <span v-if="m.restricted" class="badge-pill" title="只有管理员在用户管理里授权的用户能用">仅限授权用户</span>
        </div>
        <p class="caption">
          {{ m.model }} · 上下文 {{ wan(m.context.tokens) }}<template v-if="m.thinking_style !== 'none'"> · 可调思考</template><template v-if="m.vision"> · 能看图</template><template v-if="m.tools"> · 可用工具</template> · {{ m.base_url }} · Key {{ m.key_mask || '未填写' }}
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
    <p v-if="hasPersonalDefault && isMulti" class="caption foot">
      正在用你自己的模型作为默认。<button class="text-btn" @click="clearDefault">改回站点默认</button>
    </p>
    <p v-if="models.some((m) => m.scope === 'builtin')" class="caption foot">「配置文件」模型来自 config.yaml 的 llm.chat，需在配置文件中修改。</p>

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
        <div class="field">
          <label class="field__label" for="m-url">Base URL</label>
          <input id="m-url" v-model="form.base_url" class="text-input" required placeholder="https://api.deepseek.com/v1" inputmode="url" />
        </div>
        <div class="field">
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
            <input id="m-max" v-model.number="form.max_tokens" class="text-input" type="number" min="0" max="65536" />
            <p class="field__hint">0 表示使用服务的默认值。</p>
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
          <button type="button" class="btn btn-ghost" @click="oc.open = false">取消</button>
          <button type="submit" class="btn btn-primary" :disabled="!oc.picked.size || oc.busy || (ocNeedKey && !oc.key.trim())">
            {{ oc.busy ? '正在添加' : `添加 ${oc.picked.size} 个模型` }}
          </button>
        </div>
      </form>
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
.head-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.oc-note { margin: 4px 0 10px; line-height: 1.7; }
.oc-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 6px; max-height: 46vh; overflow-y: auto; margin-bottom: 12px; }
.oc-item { display: flex; align-items: flex-start; gap: 8px; padding: 8px 10px; border: 1px solid var(--color-divider); border-radius: var(--radius-md); font-size: 13px; cursor: pointer; line-height: 1.5; }
.oc-item.is-on { border-color: var(--color-accent); background: var(--color-accent-tint, rgba(91, 122, 74, .08)); }
.oc-item input { margin-top: 3px; }
</style>
