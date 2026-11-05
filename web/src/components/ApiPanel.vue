<script setup lang="ts">
// 设置 → API：用 OpenAI 或 Anthropic 格式的接口调用自己能用的模型。套餐包含 API 接入时才能用。
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type APIKeyInfo, type APIModel } from '../api'
import { session } from '../session'
import { toast } from '../toast'
import Icon from '../Icon.vue'
import Modal from './Modal.vue'

const name = computed(() => session.meta?.name ?? '布谷')
const loaded = ref(false)
const allowed = ref(false)
const keys = ref<APIKeyInfo[]>([])
const models = ref<APIModel[]>([])
const error = ref('')
const origin = location.origin
const tab = ref<'openai' | 'anthropic'>('openai')

const charged = computed(() => models.value.filter((m) => !m.personal))
// 最低的倍率（用来提醒「比网页贵」）。
const minScale = computed(() => (charged.value.length ? Math.min(...charged.value.map((m) => m.scale)) : 1))
const sample = computed(() => models.value.find((m) => m.is_default)?.name ?? models.value[0]?.name ?? 'deepseek-flash')

onMounted(load)

async function load() {
  try {
    const r = await api.apiAccess()
    allowed.value = r.allowed
    keys.value = r.keys ?? []
    models.value = r.models ?? []
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  } finally {
    loaded.value = true
  }
}

async function copy(text: string, what = '已复制') {
  try {
    await navigator.clipboard.writeText(text)
    toast(what)
  } catch {
    toast('复制失败，请手动选中复制')
  }
}

// ---- Key ----
const creating = ref(false)
const newName = ref('')
const busy = ref(false)
const created = ref('')
const removing = ref<APIKeyInfo | null>(null)

async function create() {
  if (busy.value) return
  busy.value = true
  try {
    const r = await api.createAPIKey(newName.value)
    created.value = r.key
    creating.value = false
    newName.value = ''
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '创建失败')
  } finally {
    busy.value = false
  }
}

async function remove() {
  const k = removing.value
  if (!k) return
  removing.value = null
  try {
    await api.deleteAPIKey(k.id)
    toast('已删除，用它的请求会立即失效')
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '删除失败')
  }
}

function when(s?: string) {
  return s ? new Date(s).toLocaleString() : '还没用过'
}

const examples = computed(() => ({
  openai: `curl ${origin}/v1/chat/completions \\
  -H "Authorization: Bearer 你的 API Key" \\
  -H "Content-Type: application/json" \\
  -d '{"model": "${sample.value}", "messages": [{"role": "user", "content": "你好"}]}'

# Python（openai 库）
from openai import OpenAI
client = OpenAI(base_url="${origin}/v1", api_key="你的 API Key")
r = client.chat.completions.create(model="${sample.value}", messages=[{"role": "user", "content": "你好"}])
print(r.choices[0].message.content)`,
  anthropic: `curl ${origin}/v1/messages \\
  -H "x-api-key: 你的 API Key" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "Content-Type: application/json" \\
  -d '{"model": "${sample.value}", "max_tokens": 1024, "messages": [{"role": "user", "content": "你好"}]}'

# Python（anthropic 库）
import anthropic
client = anthropic.Anthropic(base_url="${origin}", api_key="你的 API Key")
m = client.messages.create(model="${sample.value}", max_tokens=1024, messages=[{"role": "user", "content": "你好"}])
print(m.content[0].text)

# Claude Code 等用 Anthropic 接口的工具
export ANTHROPIC_BASE_URL=${origin}
export ANTHROPIC_API_KEY=你的 API Key`,
}))
</script>

<template>
  <section id="api" class="panel panel--inset">
    <h2>API</h2>
    <p class="muted intro">
      用 OpenAI 或 Anthropic 格式的接口，在自己的程序、脚本或别的聊天工具里调用你在{{ name }}这里能用的所有模型。
      API 只是直接调模型，没有{{ name }}的性格、记忆和工具。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <div v-else-if="!loaded" class="skeleton skel" aria-hidden="true" />
    <div v-else-if="!allowed" class="locked">
      <span class="locked__icon"><Icon name="code" :size="22" /></span>
      <p>你现在的套餐不包含 API 接入。开通包含 API 接入的会员套餐后就能在这里生成 API Key。</p>
      <RouterLink to="/profile" class="btn btn-sm">看看会员套餐</RouterLink>
    </div>
    <template v-else>
      <div v-if="charged.length" class="note">
        API 调用按网页价格的 <strong>{{ minScale }} 倍</strong>起计费（计入额度的 token 也按倍数算），在网页上和{{ name }}聊天更划算。
        自己添加的模型用的是你自己的 Key，不计费。
      </div>

      <h3>接口地址</h3>
      <div class="kv">
        <span class="kv__k">OpenAI 格式</span>
        <code class="kv__v">{{ origin }}/v1</code>
        <button class="icon-btn" title="复制" aria-label="复制 OpenAI 接口地址" @click="copy(`${origin}/v1`)"><Icon name="clip" :size="15" /></button>
      </div>
      <div class="kv">
        <span class="kv__k">Anthropic 格式</span>
        <code class="kv__v">{{ origin }}</code>
        <button class="icon-btn" title="复制" aria-label="复制 Anthropic 接口地址" @click="copy(origin)"><Icon name="clip" :size="15" /></button>
      </div>

      <div class="sub-head">
        <h3>API Key</h3>
        <button class="btn btn-sm btn-primary" :disabled="session.impersonated" @click="creating = true"><Icon name="plus" :size="14" /> 新建</button>
      </div>
      <p v-if="!keys.length" class="caption">还没有 API Key。</p>
      <div v-for="k in keys" :key="k.id" class="key">
        <div class="key__text">
          <p class="key__name">{{ k.name || '未命名' }} <code>{{ k.hint }}</code></p>
          <p class="caption">创建于 {{ new Date(k.created_at).toLocaleDateString() }} · 最近使用：{{ when(k.last_used_at) }}</p>
        </div>
        <button class="btn btn-sm btn-ghost" :disabled="session.impersonated" @click="removing = k">删除</button>
      </div>

      <h3>能用的模型</h3>
      <p class="caption">请求里的 <code>model</code> 填左边的名字；也可以用 <code>GET /v1/models</code> 查。</p>
      <div class="models">
        <div v-for="m in models" :key="m.id" class="model">
          <code class="model__name" :title="`点击复制 ${m.name}`" @click="copy(m.name, `已复制 ${m.name}`)">{{ m.name }}</code>
          <span class="model__label">{{ m.display_name }}<span v-if="m.is_default" class="tag">默认</span></span>
          <span class="model__price">{{ m.personal ? '自己的 Key，不计费' : `网页价 ×${m.scale}` }}</span>
        </div>
      </div>

      <h3>怎么调用</h3>
      <div class="type-switch tabs" role="tablist">
        <button type="button" role="tab" :aria-selected="tab === 'openai'" :class="{ 'is-active': tab === 'openai' }" @click="tab = 'openai'">OpenAI 格式</button>
        <button type="button" role="tab" :aria-selected="tab === 'anthropic'" :class="{ 'is-active': tab === 'anthropic' }" @click="tab = 'anthropic'">Anthropic 格式</button>
      </div>
      <div class="code">
        <button class="icon-btn code__copy" title="复制" aria-label="复制示例" @click="copy(examples[tab])"><Icon name="clip" :size="15" /></button>
        <pre>{{ examples[tab] }}</pre>
      </div>
      <p class="caption">
        支持流式输出（<code>stream: true</code>）、图片（能看图的模型）、工具调用和思考（OpenAI 的 <code>reasoning_effort</code>、Anthropic 的 <code>thinking</code>）。
        每分钟最多 60 次请求，同时最多 4 个。
      </p>
    </template>

    <Modal :open="creating" title="新建 API Key" @close="creating = false">
      <div class="field">
        <label class="field__label" for="k-name">名字（可以不填）</label>
        <input id="k-name" v-model="newName" class="text-input" maxlength="30" placeholder="例如：我的脚本、Cherry Studio" @keydown.enter="create" />
      </div>
      <div class="modal-actions">
        <button class="btn" @click="creating = false">取消</button>
        <button class="btn btn-primary" :disabled="busy" @click="create">{{ busy ? '正在创建' : '创建' }}</button>
      </div>
    </Modal>

    <Modal :open="!!created" title="API Key 已创建" subtitle="只显示这一次，关掉后就看不到了，请现在复制保存好。" @close="created = ''">
      <div class="created">
        <code>{{ created }}</code>
        <button class="btn btn-sm btn-primary" @click="copy(created, 'API Key 已复制')">复制</button>
      </div>
      <p class="caption warn">别把它发给别人或写进公开的代码里：拿到它的人能用你的额度和余额。泄露了就删掉重新建一个。</p>
      <div class="modal-actions">
        <button class="btn btn-primary" @click="created = ''">我已保存</button>
      </div>
    </Modal>

    <Modal :open="!!removing" :title="`删除「${removing?.name || removing?.hint}」？`" subtitle="删除后用它的程序会立即无法调用。" @close="removing = null">
      <div class="modal-actions">
        <button class="btn" @click="removing = null">取消</button>
        <button class="btn btn-danger" @click="remove">删除</button>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
.intro { margin: 0 0 12px; }
.skel { height: 120px; border-radius: var(--radius-md); }
h3 { margin: 22px 0 8px; font-size: 14px; font-weight: 600; color: var(--color-ink); }
.locked { display: grid; justify-items: start; gap: 10px; padding: 16px; border: 1px dashed var(--color-divider); border-radius: var(--radius-lg); }
.locked p { margin: 0; color: var(--color-text-secondary); }
.locked__icon { display: grid; place-items: center; width: 40px; height: 40px; border-radius: 12px; background: var(--color-accent-tint); color: var(--color-accent-active); }
.note { padding: 10px 12px; border-radius: var(--radius-md); background: var(--color-accent-tint); color: var(--color-text-secondary); font-size: 13px; line-height: 1.6; }
.note strong { color: var(--color-accent-active); }
.kv { display: flex; align-items: center; gap: 10px; padding: 6px 0; }
.kv__k { flex-shrink: 0; width: 104px; color: var(--color-muted); font-size: 13px; }
.kv__v { flex: 1; min-width: 0; overflow-wrap: anywhere; font-size: 13px; }
.sub-head { display: flex; align-items: center; justify-content: space-between; margin-top: 22px; }
.sub-head h3 { margin: 0; }
.key { display: flex; align-items: center; gap: 10px; padding: 10px 0; border-top: 1px solid var(--color-divider); }
.key:first-of-type { border-top: 0; }
.key__text { flex: 1; min-width: 0; }
.key__text p { margin: 0; }
.key__name { color: var(--color-ink); font-size: 14px; font-weight: 500; overflow-wrap: anywhere; }
.key__name code { margin-left: 6px; color: var(--color-muted); font-size: 12px; font-weight: 400; }
.models { display: grid; border: 1px solid var(--color-divider); border-radius: var(--radius-md); overflow: hidden; }
.model { display: grid; grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr) auto; align-items: center; gap: 10px; padding: 8px 12px; border-top: 1px solid var(--color-divider); font-size: 13px; }
.model:first-child { border-top: 0; }
.model__name { cursor: pointer; overflow-wrap: anywhere; }
.model__name:hover { color: var(--color-accent-active); }
.model__label { color: var(--color-text-secondary); overflow-wrap: anywhere; }
.model__price { color: var(--color-muted); white-space: nowrap; }
.tag { margin-left: 6px; padding: 0 6px; border-radius: var(--radius-pill); background: var(--color-canvas-soft); font-size: 11px; }
.tabs { margin-bottom: 8px; }
.code { position: relative; }
.code pre { margin: 0 0 8px; padding: 12px 14px; overflow-x: auto; border-radius: var(--radius-md); background: var(--color-canvas-soft); font-family: var(--font-mono); font-size: 12.5px; line-height: 1.6; }
.code__copy { position: absolute; top: 6px; right: 6px; background: var(--color-surface); }
.created { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: var(--radius-md); background: var(--color-canvas-soft); }
.created code { flex: 1; min-width: 0; overflow-wrap: anywhere; font-size: 13px; }
.warn { margin: 10px 0 0; }
@media (max-width: 600px) {
  .model { grid-template-columns: minmax(0, 1fr) auto; }
  .model__label { display: none; }
  .kv__k { width: 84px; }
}
</style>
