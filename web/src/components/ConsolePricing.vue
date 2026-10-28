<script setup lang="ts">
// 管理后台 → 计费：扣费方式，以及每个模型、后台调用、联网搜索的价格。保存后立即生效。
import { computed, onMounted, ref } from 'vue'
import { ApiError, type Price, type Pricing, type PricingItem } from '../api'
import { toast } from '../toast'

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

const items = ref<PricingItem[] | null>(null)
const mode = ref<Pricing['mode']>('quota_first')
// 表单里的价格：每一项的输入、输出（元 / 百万 token）或每千次（元），空着表示不收钱。
const prices = ref<Record<string, { input: number | ''; output: number | ''; per_k: number | '' }>>({})
const saved = ref('')
const error = ref('')
const saving = ref(false)

const groups = computed(() => {
  const out: { name: string; items: PricingItem[] }[] = []
  for (const it of items.value ?? []) {
    let g = out.find((x) => x.name === it.group)
    if (!g) out.push((g = { name: it.group, items: [] }))
    g.items.push(it)
  }
  return out
})

const snapshot = () => JSON.stringify({ mode: mode.value, prices: prices.value })
const dirty = computed(() => saved.value !== '' && snapshot() !== saved.value)

onMounted(load)

function fill(p: Pricing, list: PricingItem[]) {
  mode.value = p.mode
  const out: typeof prices.value = {}
  for (const it of list) {
    const v: Price = p.prices[it.key] ?? {}
    out[it.key] = { input: v.input || '', output: v.output || '', per_k: v.per_k || '' }
  }
  prices.value = out
  saved.value = snapshot()
}

async function load() {
  try {
    const r = await props.call<{ pricing: Pricing; items: PricingItem[] }>('GET', '/pricing')
    items.value = r.items
    fill(r.pricing, r.items)
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function save() {
  saving.value = true
  try {
    const out: Record<string, Price> = {}
    for (const [k, v] of Object.entries(prices.value)) {
      const p: Price = { input: Number(v.input) || 0, output: Number(v.output) || 0, per_k: Number(v.per_k) || 0 }
      if (p.input || p.output || p.per_k) out[k] = p
    }
    const r = await props.call<{ pricing: Pricing }>('PUT', '/pricing', { mode: mode.value, prices: out })
    fill(r.pricing, items.value ?? [])
    toast('计费设置已保存，立即生效')
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="panel panel--inset">
    <div class="head">
      <h2>计费</h2>
      <button type="button" class="btn btn-sm btn-primary" :disabled="!dirty || saving" @click="save">{{ saving ? '正在保存' : '保存' }}</button>
    </div>
    <p class="muted intro">
      额度内的用量不花钱；额度用完后，每次调用按下面的价格和实际用量（token 数或次数）从用户的余额里扣，余额用完就停。
      没填价格的项不收钱。用户自己添加的模型（自己的 API Key）不计费，管理员按「账号与安全」里的设置可以不受限额限制。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <div v-else-if="items === null" class="skeleton skel" aria-hidden="true" />
    <template v-else>
      <h3>扣费方式</h3>
      <div class="modes" role="radiogroup">
        <label class="mode" :class="{ 'is-active': mode === 'quota_first' }">
          <input v-model="mode" type="radio" value="quota_first" />
          <span>
            <strong>先用额度，用完再扣余额</strong>
            <span class="caption">没有套餐的用户先用每日免费额度（「系统设置 → 聊天与上下文 → 每日限额」），开通了时长套餐的用套餐额度。</span>
          </span>
        </label>
        <label class="mode" :class="{ 'is-active': mode === 'pay' }">
          <input v-model="mode" type="radio" value="pay" />
          <span>
            <strong>全部按量扣费</strong>
            <span class="caption">没有免费额度：没开套餐的用户每次使用都扣余额；开了时长套餐的先用套餐额度。</span>
          </span>
        </label>
      </div>

      <div v-for="g in groups" :key="g.name" class="group">
        <h3>{{ g.name }}</h3>
        <div class="table-wrap">
          <table class="table">
            <thead>
              <tr v-if="g.items[0]?.kind === 'tokens'"><th>项目</th><th>输入（元 / 百万 token）</th><th>输出（元 / 百万 token）</th></tr>
              <tr v-else><th>项目</th><th>价格（元 / 千次）</th></tr>
            </thead>
            <tbody>
              <tr v-for="it in g.items" :key="it.key">
                <td>
                  {{ it.label }}
                  <span v-if="it.hint" class="caption"><br />{{ it.hint }}</span>
                </td>
                <template v-if="it.kind === 'tokens'">
                  <td><input v-model.number="prices[it.key].input" class="text-input num" type="number" min="0" step="any" placeholder="不收钱" :aria-label="`${it.label} 输入价格`" /></td>
                  <td><input v-if="it.key !== 'embedding'" v-model.number="prices[it.key].output" class="text-input num" type="number" min="0" step="any" placeholder="不收钱" :aria-label="`${it.label} 输出价格`" /><span v-else class="caption">—</span></td>
                </template>
                <td v-else><input v-model.number="prices[it.key].per_k" class="text-input num" type="number" min="0" step="any" placeholder="不收钱" :aria-label="`${it.label} 价格`" /></td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
      <p class="caption">
        参考：DeepSeek 联网搜索和读网页官方价格各 5 元 / 千次。可以照成本价填，也可以加一点。
        联网搜索的次数单独算：时长套餐里的搜索次数（或每日免费次数）用完后才按这里的价格扣，搜不了也不影响聊天。
      </p>
    </template>
  </section>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.head h2 { margin: 0; }
.intro { margin: 8px 0 12px; font-size: 14px; line-height: 1.7; }
.skel { height: 160px; border-radius: var(--radius-md); }
h3 { margin: 18px 0 8px; font-size: 15px; }
.modes { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.mode { display: flex; gap: 10px; align-items: flex-start; padding: 12px 14px; border: 1px solid var(--color-divider); border-radius: var(--radius-md); cursor: pointer; }
.mode.is-active { border-color: var(--color-accent); background: var(--color-canvas-soft); }
.mode input { margin-top: 3px; }
.mode strong { display: block; margin-bottom: 4px; font-size: 14px; font-weight: 500; color: var(--color-ink); }
.mode .caption { line-height: 1.6; }
.table-wrap { overflow-x: auto; }
.table { width: 100%; border-collapse: collapse; font-size: 14px; }
.table th { padding: 8px; border-bottom: 1px solid var(--color-divider); color: var(--color-muted); font-weight: 500; text-align: left; white-space: nowrap; font-size: 13px; }
.table td { padding: 8px; border-bottom: 1px solid var(--color-divider); vertical-align: middle; }
.num { width: 130px; }
@media (max-width: 640px) { .modes { grid-template-columns: minmax(0, 1fr); } }
</style>
