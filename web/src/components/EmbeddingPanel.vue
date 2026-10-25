<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { api, ApiError, type EmbeddingInput, type EmbeddingView, type ReembedStatus, type TestResult } from '../api'
import { session } from '../session'
import { toast } from '../toast'

const view = ref<EmbeddingView | null>(null)
const pending = ref(0)
const counts = ref<Record<string, number>>({})
const status = ref<ReembedStatus | null>(null)
const form = reactive<EmbeddingInput>({ base_url: '', api_key: '', model: '', dim: 0 })
const error = ref('')
const saving = ref(false)
const testing = ref(false)
const testResult = ref<TestResult | null>(null)
let timer: ReturnType<typeof setTimeout> | undefined

const name = computed(() => session.meta?.name ?? '布谷')
const progress = computed(() => {
  const s = status.value
  return s && s.total > 0 ? Math.min(100, Math.round((s.done / s.total) * 100)) : 0
})
const otherModels = computed(() => Object.entries(counts.value).filter(([m]) => m !== view.value?.model))

function fail(e: unknown, fallback = '操作失败') {
  error.value = e instanceof ApiError ? e.message : fallback
}

async function load() {
  try {
    const r = await api.embedding()
    view.value = r.embedding
    pending.value = r.pending
    counts.value = r.counts
    status.value = r.reembed
    Object.assign(form, { base_url: r.embedding.base_url, api_key: '', model: r.embedding.model, dim: r.embedding.dim })
    if (r.reembed.running) poll()
  } catch (e) {
    fail(e, '加载向量模型失败')
  }
}

async function test() {
  testing.value = true
  testResult.value = null
  try {
    testResult.value = await api.testEmbedding({ ...form })
  } catch (e) {
    fail(e, '测试失败')
  } finally {
    testing.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  try {
    const r = await api.setEmbedding({ ...form })
    toast(r.changed && r.pending ? `已保存。换了模型，有 ${r.pending} 条记忆需要重新计算向量` : '向量模型已保存')
    await load()
    if (session.meta) session.meta.embedding_ready = r.embedding.ready
  } catch (e) {
    fail(e, '保存失败')
  } finally {
    saving.value = false
  }
}

function poll() {
  clearTimeout(timer)
  timer = setTimeout(async () => {
    try {
      status.value = await api.reembedStatus()
      if (status.value.running) poll()
      else {
        await load()
        toast(status.value.error ? `重新计算中断：${status.value.error}` : `重新计算完成：${status.value.done} 条`)
      }
    } catch {
      poll()
    }
  }, 1000)
}

async function reembed() {
  error.value = ''
  try {
    status.value = await api.startReembed(false)
    poll()
  } catch (e) {
    fail(e)
  }
}

onMounted(load)
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <section id="embedding" class="panel panel--inset">
    <h2>向量模型</h2>
    <p class="muted intro">
      {{ name }}用向量模型理解记忆的意思，以便在聊天时想起相关的事。全站共用一个，只有管理员可以修改。
      不同模型的向量不能互相比较，更换模型后需要重新计算已有记忆的向量。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>

    <template v-if="view">
      <p class="caption source">
        当前：{{ view.model }} · Key {{ view.key_mask || '未填写' }} · 来自{{ view.source === 'web' ? '网页设置' : '配置文件 llm.embedding' }}
      </p>

      <div v-if="pending > 0 || status?.running" class="reembed">
        <template v-if="status?.running">
          <p class="caption">正在重新计算向量：{{ status.done }} / {{ status.total }}</p>
          <div class="workspace-progress"><i :style="{ width: progress + '%' }" /></div>
        </template>
        <template v-else>
          <p class="reembed__text">
            有 {{ pending }} 条记忆的向量不是由 {{ view.model }} 生成的<template v-if="otherModels.length">（{{ otherModels.map(([m, n]) => `${m || '无向量'} ${n} 条`).join('，') }}）</template>，
            在重新计算之前检索不到它们。
          </p>
          <button class="btn btn-primary" @click="reembed">重新计算 {{ pending }} 条</button>
        </template>
      </div>

      <form @submit.prevent="save">
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="e-url">Base URL</label>
            <input id="e-url" v-model="form.base_url" class="text-input" required placeholder="https://api.siliconflow.cn/v1" />
          </div>
          <div class="field">
            <label class="field__label" for="e-model">模型名</label>
            <input id="e-model" v-model="form.model" class="text-input" required placeholder="BAAI/bge-m3" />
          </div>
        </div>
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="e-key">API Key</label>
            <input
              id="e-key"
              v-model="form.api_key"
              class="text-input"
              type="password"
              autocomplete="off"
              :placeholder="view.key_mask ? `已保存 ${view.key_mask}，留空表示不修改` : 'sk-…'"
            />
          </div>
          <div class="field">
            <label class="field__label" for="e-dim">维度</label>
            <input id="e-dim" v-model.number="form.dim" class="text-input" type="number" min="0" max="8192" />
            <p class="field__hint">0 表示使用模型默认维度。阿里云百炼 text-embedding-v3 可填 1024。</p>
          </div>
        </div>
        <p v-if="testResult" class="form-flash" :class="testResult.ok ? 'form-flash--ok' : 'form-flash--error'">
          <template v-if="testResult.ok">连通了（{{ testResult.latency_ms }} ms），返回 {{ testResult.reply }}向量。</template>
          <template v-else>连不上：{{ testResult.error }}</template>
        </p>
        <div class="actions">
          <button class="btn btn-primary" :disabled="saving || status?.running">{{ saving ? '正在保存' : '保存' }}</button>
          <button type="button" class="btn btn-ghost" :disabled="testing" @click="test">{{ testing ? '测试中…' : '测试连接' }}</button>
        </div>
      </form>
    </template>
  </section>
</template>

<style scoped>
.intro { margin: 0 0 14px; font-size: 14px; line-height: 1.65; }
.source { margin: 0 0 16px; overflow-wrap: anywhere; }
.reembed { display: grid; gap: 10px; margin-bottom: 20px; padding: var(--surface-inset-sm); border-radius: var(--radius-md); background: var(--color-canvas-soft); }
.reembed p { margin: 0; }
.reembed__text { font-size: 14px; line-height: 1.6; }
.reembed .btn { justify-self: start; }
.actions { display: flex; gap: 8px; flex-wrap: wrap; }
</style>
