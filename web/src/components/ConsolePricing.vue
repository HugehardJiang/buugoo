<script setup lang="ts">
// 管理后台 → 计费：扣费方式、分时段计价（高峰 / 空闲），以及每个模型、后台调用、联网搜索的价格。保存后立即生效。
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, type Price, type Pricing, type PricingItem } from '../api'
import { toast } from '../toast'

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

type Num = number | ''
interface Row { input: Num; cache_input: Num; output: Num; per_k: Num }
const empty = (): Row => ({ input: '', cache_input: '', output: '', per_k: '' })

const items = ref<PricingItem[] | null>(null)
const mode = ref<Pricing['mode']>('quota_first')
// 表单里的价格：高峰（或不分时段时的唯一价格）和空闲两套，空着表示不收钱（缓存价空着表示和输入价一样）。
const peakPrices = ref<Record<string, Row>>({})
const offPrices = ref<Record<string, Row>>({})
const peakOn = ref(false)
const peak = reactive({ days: [1, 2, 3, 4, 5] as number[], ranges: '09:00-12:00, 14:00-18:00', holidays: '' })
const saved = ref('')
const error = ref('')
const saving = ref(false)

const weekdays = [[1, '一'], [2, '二'], [3, '三'], [4, '四'], [5, '五'], [6, '六'], [0, '日']] as const

const groups = computed(() => {
  const out: { name: string; items: PricingItem[] }[] = []
  for (const it of items.value ?? []) {
    let g = out.find((x) => x.name === it.group)
    if (!g) out.push((g = { name: it.group, items: [] }))
    g.items.push(it)
  }
  return out
})

const snapshot = () => JSON.stringify([mode.value, peakPrices.value, offPrices.value, peakOn.value, peak])
const dirty = computed(() => saved.value !== '' && snapshot() !== saved.value)

onMounted(load)

const toRow = (p?: Price): Row => ({ input: p?.input || '', cache_input: p?.cache_input || '', output: p?.output || '', per_k: p?.per_k || '' })
const toPrice = (r: Row): Price => ({ input: Number(r.input) || 0, cache_input: Number(r.cache_input) || 0, output: Number(r.output) || 0, per_k: Number(r.per_k) || 0 })
const isZero = (p: Price) => !p.input && !p.cache_input && !p.output && !p.per_k

function fill(p: Pricing, list: PricingItem[]) {
  mode.value = p.mode
  const hi: Record<string, Row> = {}
  const lo: Record<string, Row> = {}
  for (const it of list) {
    hi[it.key] = toRow(p.prices[it.key])
    lo[it.key] = toRow(p.prices[it.key]?.off_peak)
  }
  peakPrices.value = hi
  offPrices.value = lo
  peakOn.value = !!p.peak
  if (p.peak) {
    peak.days = [...p.peak.days]
    peak.ranges = p.peak.ranges.join(', ')
    peak.holidays = compactDates(p.peak.holidays).join('\n')
  }
  saved.value = snapshot()
}

// compactDates 把连续的日期合成「2026-10-01~2026-10-07」，方便看。
function compactDates(days: string[]): string[] {
  const out: string[] = []
  let start = '', prev = ''
  const next = (d: string) => {
    const t = new Date(d + 'T00:00:00Z')
    t.setUTCDate(t.getUTCDate() + 1)
    return t.toISOString().slice(0, 10)
  }
  for (const d of [...days].sort()) {
    if (prev && next(prev) === d) {
      prev = d
      continue
    }
    if (start) out.push(start === prev ? start : `${start}~${prev}`)
    start = prev = d
  }
  if (start) out.push(start === prev ? start : `${start}~${prev}`)
  return out
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
    const prices: Record<string, Price> = {}
    for (const it of items.value ?? []) {
      const p = toPrice(peakPrices.value[it.key])
      const off = toPrice(offPrices.value[it.key])
      if (peakOn.value && !isZero(off)) p.off_peak = off
      if (!isZero(p) || p.off_peak) prices[it.key] = p
    }
    const body: Pricing = { mode: mode.value, prices }
    if (peakOn.value) {
      body.peak = {
        days: peak.days,
        ranges: peak.ranges.split(/[,，\s]+/).filter(Boolean),
        holidays: peak.holidays.split(/[\n,，\s]+/).filter(Boolean),
      }
    }
    const r = await props.call<{ pricing: Pricing }>('PUT', '/pricing', body)
    fill(r.pricing, items.value ?? [])
    toast('计费设置已保存，立即生效')
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

function toggleDay(d: number) {
  peak.days = peak.days.includes(d) ? peak.days.filter((x) => x !== d) : [...peak.days, d]
}

// DeepSeek 官方价格（元 / 百万 token）：高峰 / 空闲。空闲是高峰的一半；联网搜索和读网页各 5 元 / 千次。
const deepseek: Record<string, { hi: Row; lo: Row }> = {
  flash: { hi: { input: 2, cache_input: 0.04, output: 8, per_k: '' }, lo: { input: 1, cache_input: 0.02, output: 4, per_k: '' } },
  pro: { hi: { input: 9, cache_input: 0.3, output: 27, per_k: '' }, lo: { input: 4.5, cache_input: 0.15, output: 13.5, per_k: '' } },
}

function deepseekKind(model = ''): 'flash' | 'pro' | '' {
  const m = model.toLowerCase()
  if (!m.includes('deepseek')) return ''
  if (m.includes('pro')) return 'pro'
  if (m.includes('flash') || m.includes('chat') || m.includes('reasoner')) return 'flash'
  return ''
}

const deepseekRows = computed(() => (items.value ?? []).filter((it) => it.kind === 'tokens' && deepseekKind(it.model)))

function fillDeepSeek() {
  let n = 0
  for (const it of items.value ?? []) {
    const k = it.kind === 'tokens' ? deepseekKind(it.model) : ''
    if (k) {
      peakPrices.value[it.key] = { ...deepseek[k].hi }
      offPrices.value[it.key] = { ...deepseek[k].lo }
      n++
    }
    if (it.key === 'search:deepseek' || it.key === 'fetch:deepseek') {
      peakPrices.value[it.key] = { ...empty(), per_k: 5 }
      offPrices.value[it.key] = empty()
      n++
    }
  }
  // DeepSeek 的高峰时段：周一至周五 9:00-12:00、14:00-18:00（北京时间），法定节假日全天空闲。
  peakOn.value = true
  peak.days = [1, 2, 3, 4, 5]
  peak.ranges = '09:00-12:00, 14:00-18:00'
  toast(`填好了 ${n} 项，别忘了补上今年的法定节假日，然后保存`)
}
</script>

<template>
  <section class="panel panel--inset">
    <div class="head">
      <h2>计费</h2>
      <div class="head__btns">
        <button v-if="deepseekRows.length" type="button" class="btn btn-sm" @click="fillDeepSeek">填入 DeepSeek 官方价格</button>
        <button type="button" class="btn btn-sm btn-primary" :disabled="!dirty || saving" @click="save">{{ saving ? '正在保存' : '保存' }}</button>
      </div>
    </div>
    <p class="muted intro">
      额度内的用量不花钱；额度用完后，每次调用按下面的价格和实际用量（token 数或次数）从用户的余额里扣，余额用完就停。
      没填价格的项不收钱。用户自己添加的模型（自己的 API Key）不计费。
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

      <h3>分时段计价</h3>
      <label class="check-row"><input v-model="peakOn" type="checkbox" /> 分高峰和空闲两套价格</label>
      <div v-if="peakOn" class="peak">
        <div class="peak__row">
          <span class="field__label">高峰的日子</span>
          <div class="days" role="group" aria-label="高峰的星期几">
            <button v-for="[d, name] in weekdays" :key="d" type="button" :class="{ 'is-active': peak.days.includes(d) }" @click="toggleDay(d)">周{{ name }}</button>
          </div>
        </div>
        <div class="peak__row">
          <label class="field__label" for="peak-ranges">高峰时间段</label>
          <input id="peak-ranges" v-model="peak.ranges" class="text-input" placeholder="09:00-12:00, 14:00-18:00" />
        </div>
        <div class="peak__row">
          <label class="field__label" for="peak-hol">法定节假日（全天按空闲价）</label>
          <textarea id="peak-hol" v-model="peak.holidays" class="textarea small mono" placeholder="2026-10-01~2026-10-07&#10;2027-01-01" />
          <p class="field__hint">一行一个日期或一段日期。节假日每年不一样，需要自己填；调休上班的周末本来就不算高峰，不用填。时间按系统设置里的时区算。</p>
        </div>
        <p class="caption">其余时段都按空闲价。某一项空闲价空着时，全天按高峰价。</p>
      </div>

      <div v-for="g in groups" :key="g.name" class="group">
        <h3>{{ g.name }}</h3>
        <div class="table-wrap">
          <table class="table">
            <thead>
              <tr v-if="g.items[0]?.kind === 'tokens'">
                <th>项目</th><th v-if="peakOn" />
                <th>输入·未命中缓存</th><th>输入·命中缓存</th><th>输出</th>
              </tr>
              <tr v-else><th>项目</th><th v-if="peakOn" /><th>价格（元 / 千次）</th></tr>
            </thead>
            <tbody>
              <template v-for="it in g.items" :key="it.key">
                <tr v-for="(set, i) in (peakOn ? [peakPrices, offPrices] : [peakPrices])" :key="it.key + i" :class="{ 'is-sub': i === 1 }">
                  <td v-if="i === 0" :rowspan="peakOn ? 2 : 1">
                    {{ it.label }}
                    <span v-if="it.hint" class="caption"><br />{{ it.hint }}</span>
                  </td>
                  <td v-if="peakOn" class="caption when">{{ i === 0 ? '高峰' : '空闲' }}</td>
                  <template v-if="it.kind === 'tokens'">
                    <td><input v-model.number="set[it.key].input" class="text-input num" type="number" min="0" step="any" :placeholder="i ? '同高峰' : '不收钱'" :aria-label="`${it.label} 输入价格`" /></td>
                    <td><input v-if="it.key !== 'embedding'" v-model.number="set[it.key].cache_input" class="text-input num" type="number" min="0" step="any" placeholder="同未命中" :aria-label="`${it.label} 缓存命中价格`" /><span v-else class="caption">—</span></td>
                    <td><input v-if="it.key !== 'embedding'" v-model.number="set[it.key].output" class="text-input num" type="number" min="0" step="any" :placeholder="i ? '同高峰' : '不收钱'" :aria-label="`${it.label} 输出价格`" /><span v-else class="caption">—</span></td>
                  </template>
                  <td v-else><input v-model.number="set[it.key].per_k" class="text-input num" type="number" min="0" step="any" :placeholder="i ? '同高峰' : '不收钱'" :aria-label="`${it.label} 价格`" /></td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
        <p v-if="g.items[0]?.kind === 'tokens'" class="caption unit">单位：元 / 百万 token</p>
      </div>
      <p class="caption">
        「命中缓存」是服务商返回的缓存命中的输入 token（DeepSeek 等会自动缓存重复的开头部分，命中的价格低很多）；
        服务商不返回时全部按未命中算。联网搜索的次数单独算：时长套餐里的搜索次数（或每日免费次数）用完后才按这里的价格扣，搜不了也不影响聊天。
      </p>
    </template>
  </section>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; gap: 8px; flex-wrap: wrap; }
.head h2 { margin: 0; }
.head__btns { display: flex; gap: 8px; }
.intro { margin: 8px 0 12px; font-size: 14px; line-height: 1.7; }
.skel { height: 160px; border-radius: var(--radius-md); }
h3 { margin: 18px 0 8px; font-size: 15px; }
.modes { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.mode { display: flex; gap: 10px; align-items: flex-start; padding: 12px 14px; border: 1px solid var(--color-divider); border-radius: var(--radius-md); cursor: pointer; }
.mode.is-active { border-color: var(--color-accent); background: var(--color-canvas-soft); }
.mode input { margin-top: 3px; }
.mode strong { display: block; margin-bottom: 4px; font-size: 14px; font-weight: 500; color: var(--color-ink); }
.mode .caption { line-height: 1.6; }
.peak { display: grid; gap: 12px; margin-top: 10px; padding: 14px; border: 1px solid var(--color-divider); border-radius: var(--radius-md); }
.peak__row { display: grid; gap: 6px; }
.peak p { margin: 0; }
.days { display: flex; flex-wrap: wrap; gap: 6px; }
.days button { padding: 5px 10px; border: 1px solid var(--color-divider); border-radius: 999px; background: none; font-size: 13px; cursor: pointer; }
.days button.is-active { border-color: var(--color-accent); background: var(--color-accent); color: #fff; }
.textarea.small { min-height: 70px; }
.mono { font-family: var(--font-mono); font-size: 13px; }
.table-wrap { overflow-x: auto; }
.table { width: 100%; border-collapse: collapse; font-size: 14px; }
.table th { padding: 8px; border-bottom: 1px solid var(--color-divider); color: var(--color-muted); font-weight: 500; text-align: left; white-space: nowrap; font-size: 13px; }
.table td { padding: 6px 8px; border-bottom: 1px solid var(--color-divider); vertical-align: middle; }
.table tr.is-sub td { padding-top: 2px; }
.when { white-space: nowrap; }
.num { width: 110px; }
.unit { margin: 4px 0 0; }
@media (max-width: 640px) { .modes { grid-template-columns: minmax(0, 1fr); } }
</style>
