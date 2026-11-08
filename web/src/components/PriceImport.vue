<script setup lang="ts">
// 计费页的「按价格表导入」：复制一段话发给任意 AI 助手，再把供应商的价格表截图发过去，
// AI 整理成固定格式的文本；粘贴回来，预览对上了哪些模型，按汇率、倍率换算成「元 / 百万 token」填进价格表。
import { computed, reactive, ref, watch } from 'vue'
import type { PricingItem } from '../api'
import { toast } from '../toast'
import Modal from './Modal.vue'

const props = defineProps<{ open: boolean; items: PricingItem[] }>()
const emit = defineEmits<{ close: []; apply: [prices: Record<string, { input: number; cache_input: number; output: number }>] }>()

type Entry = { model: string; input: number; output: number; cache_read?: number; cache_write?: number }
type Parsed = { currency: string; multiplier: number; prices: Entry[] }

const text = ref('')
const opt = reactive({ group: '', rate: 1, markup: 1, writeExtra: 0 })
const off = ref<Set<string>>(new Set())

// 只导入按 token 计价的聊天模型。
const modelItems = computed(() => props.items.filter((it) => it.kind === 'tokens' && it.model && it.group.startsWith('聊天模型')))
const groupNames = computed(() => [...new Set(modelItems.value.map((it) => it.group))])
const targets = computed(() => modelItems.value.filter((it) => !opt.group || it.group === opt.group))

watch(
  () => props.open,
  (v) => {
    if (!v) return
    text.value = ''
    off.value = new Set()
    Object.assign(opt, { group: groupNames.value.length === 1 ? groupNames.value[0] : '', rate: 1, markup: 1, writeExtra: 0 })
  },
)

const prompt = computed(() => {
  const names = [...new Set(modelItems.value.map((it) => it.model))].sort()
  return `我要把一张大模型 API 的价格表整理成固定格式，导入到我的计费系统里。等下我会发价格表的截图（可能有好几张），请你按下面的要求整理。

要求：
1. 只输出一个 JSON 代码块，不要别的解释。格式如下：
\`\`\`json
{
  "currency": "USD",
  "multiplier": 1,
  "prices": [
    { "model": "模型名", "input": 0, "output": 0, "cache_read": 0, "cache_write": 0 }
  ]
}
\`\`\`
2. 所有价格都是「每 100 万 token」的价格，用截图上的原始数字，不要自己换算汇率、不要乘倍率。截图上是每 1K token 的，乘以 1000 换算成每 100 万。
3. currency 填截图上价格的货币：美元写 USD，人民币写 CNY。
4. multiplier 填截图上的分组倍率或折扣倍率（例如「1.3x」就写 1.3，「0.25x」写 0.25）；没有就写 1。
5. input 是普通输入价，output 是输出价；cache_read 是「缓存读取 / 缓存命中」的价格，cache_write 是「缓存写入 / 缓存创建」的价格。
   缓存写入有好几档（例如 5 分钟和 1 小时）时填最便宜的那档。截图上没有的项不要写或者写 0。
6. 同一个模型有按长度分档的价格时，用最低一档（例如 200K 以内）的价格。
7. model 用截图上的模型名（原样，例如 claude-opus-5-5、gpt-5.5、glm-5.2）。截图里的模型都列出来，不要漏，也不要编造截图上没有的模型。

我的系统里现在有这些模型（供你对照名字的写法；截图里有但这里没有的也照样列出来）：
${names.join('、') || '（还没有）'}`
})

async function copyPrompt() {
  try {
    await navigator.clipboard.writeText(prompt.value)
    toast('复制好了，发给 AI 助手，再发价格表截图')
  } catch {
    toast('复制失败，请手动选中下面的文字复制')
  }
}

// parse 从粘贴的文字里找出 JSON（AI 常常会包一层代码块或者多说几句话）。
const parsed = computed<{ data?: Parsed; error?: string }>(() => {
  const raw = text.value.trim()
  if (!raw) return {}
  const start = raw.indexOf('{')
  const end = raw.lastIndexOf('}')
  if (start < 0 || end <= start) return { error: '没找到 JSON：请把 AI 回复里的代码块整个粘贴进来' }
  let obj: unknown
  try {
    obj = JSON.parse(raw.slice(start, end + 1))
  } catch {
    return { error: 'JSON 格式不对，可以让 AI 重新只输出一个 JSON 代码块' }
  }
  const o = obj as Partial<Parsed> & { models?: Entry[] }
  const list = Array.isArray(o.prices) ? o.prices : Array.isArray(o.models) ? o.models : null
  if (!list) return { error: '里面没有 prices 列表' }
  const num = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) && v >= 0 ? v : Number(v) >= 0 ? Number(v) : 0)
  const prices = list
    .filter((e) => e && typeof e.model === 'string' && e.model.trim())
    .map((e) => ({ model: e.model.trim(), input: num(e.input), output: num(e.output), cache_read: num(e.cache_read), cache_write: num(e.cache_write) }))
  return { data: { currency: String(o.currency || 'USD').toUpperCase(), multiplier: num(o.multiplier) || 1, prices } }
})

// 截图是人民币时汇率默认 1；有缓存写入比普通输入贵的模型（Claude）时，默认给未命中输入多加 15% 抵掉缓存写入的差价。
watch(
  () => parsed.value.data,
  (d, old) => {
    if (!d || (old && old.currency === d.currency && old.prices.length === d.prices.length)) return
    if (d.currency === 'CNY' || d.currency === 'RMB') opt.rate = 1
    opt.writeExtra = d.prices.some((p) => p.cache_write && p.cache_write > p.input) ? 15 : 0
  },
)

const norm = (s: string) => s.toLowerCase().trim().replace(/^.*\//, '').replace(/[\s_]+/g, '-')

type Row = { entry: Entry; matches: PricingItem[]; input: number; cache_input: number; output: number }
const rows = computed<Row[]>(() => {
  const d = parsed.value.data
  if (!d) return []
  const k = d.multiplier * (Number(opt.rate) || 0) * (Number(opt.markup) || 0)
  const round = (x: number) => Math.round(x * 10000) / 10000
  return d.prices.map((e) => ({
    entry: e,
    matches: targets.value.filter((it) => norm(it.model ?? '') === norm(e.model)),
    input: round(e.input * k * (1 + (Number(opt.writeExtra) || 0) / 100)),
    cache_input: round((e.cache_read ?? 0) * k),
    output: round(e.output * k),
  }))
})
const matched = computed(() => rows.value.filter((r) => r.matches.length && !off.value.has(r.entry.model)))
const unmatched = computed(() => rows.value.filter((r) => !r.matches.length))

function toggle(model: string) {
  const s = new Set(off.value)
  if (s.has(model)) s.delete(model)
  else s.add(model)
  off.value = s
}

function apply() {
  const out: Record<string, { input: number; cache_input: number; output: number }> = {}
  for (const r of matched.value) for (const it of r.matches) out[it.key] = { input: r.input, cache_input: r.cache_input, output: r.output }
  emit('apply', out)
}
</script>

<template>
  <Modal :open="open" title="按价格表导入" subtitle="让 AI 助手看截图整理价格，粘贴回来自动填进对应的模型。" wide @close="emit('close')">
    <ol class="steps">
      <li>
        <div class="step-head">
          <strong>复制下面这段话，发给任意 AI 助手（Claude、ChatGPT、DeepSeek、豆包都行）</strong>
          <button type="button" class="btn btn-sm btn-primary" @click="copyPrompt">一键复制</button>
        </div>
        <textarea class="textarea mono prompt" readonly :value="prompt" aria-label="发给 AI 的话" @focus="($event.target as HTMLTextAreaElement).select()" />
      </li>
      <li><strong>再把供应商的价格表截图发给它</strong>（可以好几张），它会回一段 JSON。</li>
      <li>
        <strong>把 AI 的回复整段粘贴到这里</strong>
        <textarea v-model="text" class="textarea mono paste" placeholder="{ &quot;currency&quot;: &quot;USD&quot;, &quot;multiplier&quot;: 1.3, &quot;prices&quot;: [ … ] }" aria-label="AI 整理好的价格" />
        <p v-if="parsed.error" class="form-flash form-flash--error">{{ parsed.error }}</p>
      </li>
    </ol>

    <template v-if="parsed.data">
      <div class="opts">
        <label class="field">
          <span class="field__label">导入到</span>
          <select v-model="opt.group" class="text-input">
            <option value="">所有供应商里同名的模型</option>
            <option v-for="g in groupNames" :key="g" :value="g">{{ g.replace('聊天模型 · ', '') }}</option>
          </select>
        </label>
        <label class="field">
          <span class="field__label">1 {{ parsed.data.currency === 'USD' ? '美元' : parsed.data.currency }} 实际花多少元</span>
          <input v-model.number="opt.rate" class="text-input" type="number" min="0" step="any" />
        </label>
        <label class="field">
          <span class="field__label">加价倍数</span>
          <input v-model.number="opt.markup" class="text-input" type="number" min="0" step="any" />
        </label>
        <label class="field">
          <span class="field__label">未命中输入多加（%）</span>
          <input v-model.number="opt.writeExtra" class="text-input" type="number" min="0" max="200" step="1" />
        </label>
      </div>
      <p class="caption hint">
        截图上的价格 × 分组倍率 {{ parsed.data.multiplier }} × 汇率 × 加价倍数 = 填进去的价格（元 / 百万 token）。
        汇率填你在这个供应商充 1 {{ parsed.data.currency === 'USD' ? '美元' : '单位' }}额度实际花的人民币（很多中转站是 1 元充 1 美元，就填 1）。
        「未命中输入多加」用来抵掉缓存写入比普通输入贵的差价（Claude 建议 10～25%，布谷看不到缓存写入的用量）。
      </p>

      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th /><th>模型</th><th>对上了</th><th>未命中</th><th>命中缓存</th><th>输出</th></tr>
          </thead>
          <tbody>
            <tr v-for="r in rows" :key="r.entry.model" :class="{ 'is-off': !r.matches.length || off.has(r.entry.model) }">
              <td><input type="checkbox" :disabled="!r.matches.length" :checked="r.matches.length > 0 && !off.has(r.entry.model)" :aria-label="r.entry.model" @change="toggle(r.entry.model)" /></td>
              <td class="mono">{{ r.entry.model }}</td>
              <td class="caption">{{ r.matches.length ? r.matches.map((m) => m.label).join('、') : '没有这个模型' }}</td>
              <td class="num">{{ r.input }}</td>
              <td class="num">{{ r.cache_input || '—' }}</td>
              <td class="num">{{ r.output }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="unmatched.length" class="caption">
        {{ unmatched.length }} 个模型布谷里还没有（或不在选中的供应商下面），先在「设置 → 模型」里添加，再导入一次就能填上。
      </p>
    </template>

    <div class="modal-actions">
      <button type="button" class="btn" @click="emit('close')">取消</button>
      <button type="button" class="btn btn-primary" :disabled="!matched.length" @click="apply">填入 {{ matched.reduce((n, r) => n + r.matches.length, 0) }} 个模型</button>
    </div>
  </Modal>
</template>

<style scoped>
.steps { display: grid; gap: 14px; margin: 0 0 14px; padding-left: 20px; }
.steps li { font-size: 14px; line-height: 1.6; }
.step-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; margin-bottom: 6px; }
.prompt { width: 100%; height: 110px; font-size: 12px; color: var(--color-text-secondary); }
.paste { width: 100%; height: 120px; margin-top: 6px; font-size: 12px; }
.mono { font-family: var(--font-mono); }
.opts { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 10px; }
.opts .field { margin: 0; }
.hint { margin: 8px 0 12px; line-height: 1.7; }
.table-wrap { max-height: 40vh; overflow: auto; }
.table td.num { font-variant-numeric: tabular-nums; text-align: right; white-space: nowrap; }
tr.is-off td { opacity: .5; }
</style>
