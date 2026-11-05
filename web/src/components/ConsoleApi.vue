<script setup lang="ts">
// 管理后台 → API 接入：API 调用的价格倍率。只对管理员添加的模型生效（用户自己添加的模型用自己的 Key，不计费）。
// 倍率同时作用在扣的钱和计入额度的 token 上，用来引导大家多用网页。哪些用户能用 API 在「会员套餐」里勾选。
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, type APISettings } from '../api'
import { toast } from '../toast'

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

interface Row { id: number; name: string; model: string; scope: string }
const rows = ref<Row[] | null>(null)
const error = ref('')
const saving = ref(false)
const form = reactive({ default: 1.5 as number | '', models: {} as Record<string, number | ''> })
const origin = location.origin

onMounted(load)

async function load() {
  try {
    const r = await props.call<{ settings: APISettings; models: Row[] }>('GET', '/api-settings')
    rows.value = r.models
    form.default = r.settings.default
    form.models = Object.fromEntries(r.models.map((m) => [String(m.id), r.settings.models?.[String(m.id)] ?? '']))
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

const effective = (id: number) => {
  const v = form.models[String(id)]
  return v === '' || v === undefined || Number(v) <= 0 ? Number(form.default) || 1.5 : Number(v)
}
const dirty = ref(false)
const valid = computed(() => Number(form.default) > 0)

async function save() {
  if (saving.value || !valid.value) return
  saving.value = true
  const models: Record<string, number> = {}
  for (const [k, v] of Object.entries(form.models)) if (v !== '' && Number(v) > 0) models[k] = Number(v)
  try {
    await props.call('PUT', '/api-settings', { default: Number(form.default), models })
    dirty.value = false
    toast('倍率已保存，马上生效')
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="panel panel--inset">
    <h2>API 接入</h2>
    <p class="muted intro">
      用户可以用 OpenAI（<code>{{ origin }}/v1</code>）或 Anthropic（<code>{{ origin }}</code>）格式的接口调用自己能用的模型。
      只有开通了「包含 API 接入」套餐的用户（在「会员套餐」里勾选）和管理员能用。
    </p>
    <p class="muted intro">
      API 调用按网页价格乘以倍率扣费，计入每日 / 每月额度的 token 数也乘以倍率（例如 1.5 倍时，用 1 万 token 按 1.5 万算），用来引导大家多用网页。
      用户自己添加的模型用的是自己的 Key，不计费，倍率不适用。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <div v-else-if="rows === null" class="skeleton skel" aria-hidden="true" />
    <template v-else>
      <div class="field">
        <label class="field__label" for="api-default">默认倍率</label>
        <div class="inline">
          <span class="caption">网页价 ×</span>
          <input id="api-default" v-model.number="form.default" class="text-input num" type="number" min="0.1" max="100" step="0.1" @input="dirty = true" />
          <span class="caption">没单独设置的模型都用这个</span>
        </div>
      </div>
      <p class="label">各模型单独设置（留空用默认倍率）</p>
      <div class="table">
        <div v-for="m in rows" :key="m.id" class="tr">
          <div class="td name">
            <span class="title">{{ m.name }}</span>
            <span class="caption">{{ m.model }}{{ m.scope === 'builtin' ? ' · 配置文件' : '' }}</span>
          </div>
          <div class="td">
            <span class="caption">×</span>
            <input v-model.number="form.models[String(m.id)]" class="text-input num" type="number" min="0.1" max="100" step="0.1" :placeholder="String(Number(form.default) || 1.5)" :aria-label="`${m.name} 的倍率`" @input="dirty = true" />
          </div>
          <div class="td eff caption">现在 ×{{ effective(m.id) }}</div>
        </div>
        <p v-if="!rows.length" class="caption empty">还没有管理员添加的模型。</p>
      </div>
      <div class="actions">
        <button class="btn btn-primary" :disabled="saving || !dirty || !valid" @click="save">{{ saving ? '正在保存' : '保存' }}</button>
      </div>
    </template>
  </section>
</template>

<style scoped>
.intro { margin: 0 0 10px; }
.skel { height: 120px; border-radius: var(--radius-md); }
.inline { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.text-input.num { width: 96px; }
.label { margin: 18px 0 6px; color: var(--color-ink); font-size: 13px; font-weight: 500; }
.table { border: 1px solid var(--color-divider); border-radius: var(--radius-md); overflow: hidden; }
.tr { display: grid; grid-template-columns: minmax(0, 1fr) auto 90px; align-items: center; gap: 12px; padding: 8px 12px; border-top: 1px solid var(--color-divider); }
.tr:first-child { border-top: 0; }
.td { display: flex; align-items: center; gap: 6px; min-width: 0; }
.name { display: grid; gap: 1px; }
.title { color: var(--color-ink); font-size: 14px; overflow-wrap: anywhere; }
.eff { justify-content: flex-end; }
.empty { margin: 0; padding: 12px; }
.actions { display: flex; justify-content: flex-end; margin-top: 14px; }
@media (max-width: 600px) {
  .tr { grid-template-columns: minmax(0, 1fr) auto; }
  .eff { display: none; }
}
</style>
