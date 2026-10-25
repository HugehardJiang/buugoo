<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
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
  public: false,
})
const form = reactive<ModelInput>(blank())

// 常见服务的预设，选中后自动填写地址、模型名与思考方式，仍可修改。
const presets: { label: string; v: Partial<ModelInput> }[] = [
  { label: 'DeepSeek', v: { name: 'DeepSeek', base_url: 'https://api.deepseek.com/v1', model: 'deepseek-flash', thinking_style: 'deepseek', thinking_model: '', vision: true, tools: true } },
  { label: '阿里云百炼', v: { name: '通义千问', base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', model: 'qwen-plus', thinking_style: 'enable_thinking', thinking_model: '', vision: false, tools: true } },
  { label: '硅基流动', v: { name: '硅基流动', base_url: 'https://api.siliconflow.cn/v1', model: 'Qwen/Qwen3-32B', thinking_style: 'enable_thinking', thinking_model: '', vision: false, tools: true } },
  { label: 'OpenAI', v: { name: 'OpenAI', base_url: 'https://api.openai.com/v1', model: 'gpt-5-mini', thinking_style: 'reasoning_effort', thinking_model: '', vision: true, tools: true } },
]

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
      vision: m.vision, tools: m.tools,
    })
  }
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
      <button class="btn btn-primary" @click="openForm(null)">添加模型</button>
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
        </div>
        <p class="caption">
          {{ m.model }}<template v-if="m.thinking_style !== 'none'"> · 可调思考</template><template v-if="m.vision"> · 能看图</template><template v-if="m.tools"> · 可用工具</template> · {{ m.base_url }} · Key {{ m.key_mask || '未填写' }}
        </p>
        <p v-if="rowTest[m.id]" class="caption test-line">{{ rowTest[m.id] }}</p>
      </div>
      <div class="model-row__actions">
        <button v-if="canSetDefault(m)" class="btn btn-ghost" @click="setDefault(m)">设为默认</button>
        <button class="btn" @click="testRow(m)">测试</button>
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

    <Modal v-if="removing" :title="`删除「${removing.name}」？`" subtitle="使用这个模型的对话会改回默认模型，聊天记录不受影响。" @close="removing = null">
      <div class="modal-actions">
        <button class="btn" @click="removing = null">取消</button>
        <button class="btn btn-danger" @click="remove">删除模型</button>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
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
</style>
