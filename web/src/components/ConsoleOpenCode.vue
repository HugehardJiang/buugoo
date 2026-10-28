<script setup lang="ts">
// 管理后台 → OpenCode Go：用站长的 OpenCode Go Key 一键添加模型（默认仅限授权用户），
// 并按「官方价 × 套餐月费 ÷ 这个模型的月额度 × 汇率 × 加价倍数」写好计费价格。
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError } from '../api'
import { toast } from '../toast'

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

interface Rate { input: number; output: number; cached: number }
interface Spec {
  id: string
  name: string
  rate: Rate
  off_peak?: Rate
  limits: Record<string, number>
  free?: boolean
  vision?: boolean
  note?: string
}
interface Info {
  catalog: Spec[]
  catalog_date: string
  plans: Record<string, number>
  base_url: string
  options: { tier: 'go' | 'plus'; rate: number; markup: number; restricted: boolean }
  imported: { id: number; model: string; name: string; restricted: boolean }[] | null
  billing: boolean
  multi: boolean
}

const info = ref<Info | null>(null)
const loadError = ref('')
const opts = reactive({ tier: 'go' as 'go' | 'plus', rate: 7.2, markup: 1, restricted: true, api_key: '' })
const picked = ref<Set<string>>(new Set())
const busy = ref(false)
const result = ref('')

const imported = computed(() => new Map((info.value?.imported ?? []).map((m) => [m.model, m])))
const needKey = computed(() => [...picked.value].some((id) => !imported.value.has(id)))

onMounted(load)

async function load() {
  try {
    const r = await props.call<Info>('GET', '/opencode')
    info.value = r
    Object.assign(opts, r.options)
    // 默认勾上已经添加过的（重新导入时一起更新价格）。
    if (!picked.value.size) picked.value = new Set((r.imported ?? []).map((m) => m.model))
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

function factor(s: Spec) {
  const limit = s.limits[opts.tier]
  if (s.free || !limit) return 0
  return (info.value?.plans[opts.tier] ?? 0) / limit
}

function cny(usd: number, s: Spec) {
  return usd * factor(s) * (Number(opts.rate) || 0) * (Number(opts.markup) || 0)
}

function fmt(x: number) {
  if (x === 0) return '0'
  return x < 0.01 ? x.toFixed(4) : x < 1 ? x.toFixed(3) : x.toFixed(2)
}

function toggle(id: string) {
  const s = new Set(picked.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  picked.value = s
}

function pickAll(on: boolean) {
  picked.value = on ? new Set(info.value?.catalog.map((s) => s.id)) : new Set()
}

async function submit() {
  if (!picked.value.size || busy.value) return
  busy.value = true
  result.value = ''
  try {
    const r = await props.call<{ message: string; peak_note: string }>('POST', '/opencode/import', {
      api_key: opts.api_key, models: [...picked.value], tier: opts.tier, rate: Number(opts.rate), markup: Number(opts.markup), restricted: opts.restricted,
    })
    result.value = r.message + (r.peak_note ? '。' + r.peak_note : '')
    toast(r.message)
    opts.api_key = ''
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '导入失败')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="panel panel--inset">
    <h2>OpenCode Go</h2>
    <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
    <template v-else-if="info">
      <p class="caption intro">
        用你自己的 OpenCode Go 订阅 Key 一键添加模型。默认<strong>只有你在「用户管理」里授权了「专属模型」的用户</strong>能用（管理员总能用）。
        价格按「官方价 × 套餐月费 ÷ 这个模型的月额度 × 汇率 × 加价倍数」折算成你的实际成本，写进计费设置。
        例如 Go 档 GLM-5.2 每月有 $60 额度、套餐 $10，相当于官方价的 1/6。
        用户想用自己的 Key 的话，在「设置 → 模型」里选「OpenCode Go」预设自己添加，不计费。
      </p>
      <p class="form-flash warn">
        官方说明 Go 套餐是给 OpenCode 等编程助手用的，并会监控滥用。布谷会按要求带上客户端标识和会话编号，但聊天不是编程请求，
        有被限制或封号的风险，请自己权衡；另外 Go 只允许单人工作区，不要把 Key 给别人。
      </p>

      <div class="grid">
        <label class="field">
          <span class="field__label">套餐档位</span>
          <select v-model="opts.tier" class="text-input">
            <option value="go">Go（${{ info.plans.go }}/月）</option>
            <option value="plus">Go Plus（${{ info.plans.plus }}/月）</option>
          </select>
        </label>
        <label class="field">
          <span class="field__label">汇率（1 美元 = ? 元）</span>
          <input v-model.number="opts.rate" class="text-input" type="number" min="0.01" step="0.01" />
        </label>
        <label class="field">
          <span class="field__label">加价倍数</span>
          <input v-model.number="opts.markup" class="text-input" type="number" min="0.01" step="0.1" title="1 = 按成本价收；1.2 = 加价 20%" />
        </label>
        <label class="field">
          <span class="field__label">API Key</span>
          <input v-model="opts.api_key" class="text-input" type="password" autocomplete="new-password" :placeholder="imported.size ? '不填则沿用已保存的 Key' : 'OpenCode 控制台里复制的 Key'" />
        </label>
      </div>
      <p class="caption hint">加价倍数：1 = 按成本价收，1.2 = 在成本价上加 20%。</p>
      <label class="check-row">
        <input v-model="opts.restricted" type="checkbox" />
        <span>仅限授权用户使用（取消勾选则所有用户都能用）</span>
      </label>
      <p v-if="!info.billing" class="caption">现在是单用户版，没有计费，只会添加模型。</p>

      <div class="table-head">
        <span class="caption">价格都是每百万 token。价格表日期 {{ info.catalog_date }}（来自 opencode.ai/docs/go，官方会调整）</span>
        <span class="links">
          <button type="button" class="link caption" @click="pickAll(true)">全选</button>
          <button type="button" class="link caption" @click="pickAll(false)">全不选</button>
        </span>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th /><th>模型</th><th>月额度</th><th>折算</th><th>价格（输入 / 输出 / 缓存命中）</th></tr>
          </thead>
          <tbody>
            <tr v-for="s in info.catalog" :key="s.id" :class="{ 'is-picked': picked.has(s.id) }" @click="toggle(s.id)">
              <td><input type="checkbox" :checked="picked.has(s.id)" :aria-label="`选择 ${s.name}`" @click.stop="toggle(s.id)" /></td>
              <td>
                <span class="name">{{ s.name }}</span>
                <span v-if="imported.has(s.id)" class="badge-pill">已添加</span>
                <br /><code class="caption">{{ s.id }}</code>
                <p v-if="s.note" class="caption note">{{ s.note }}</p>
              </td>
              <td class="num">{{ s.free ? '不限' : `$${s.limits[opts.tier]}` }}</td>
              <td class="num">{{ s.free ? '-' : `×${fmt(factor(s))}` }}</td>
              <td class="num">
                <template v-if="s.free">免费</template>
                <template v-else>
                  <span class="caption">官方 ${{ s.rate.input }} / {{ s.rate.output }} / {{ s.rate.cached }}</span><br />
                  <span class="strong">收费 ¥{{ fmt(cny(s.rate.input, s)) }} / {{ fmt(cny(s.rate.output, s)) }} / {{ fmt(cny(s.rate.cached, s)) }}</span>
                  <template v-if="s.off_peak">
                    <br /><span class="caption">空闲：官方 ${{ s.off_peak.input }} / {{ s.off_peak.output }} / {{ s.off_peak.cached }}</span><br />
                    <span class="strong">空闲收费 ¥{{ fmt(cny(s.off_peak.input, s)) }} / {{ fmt(cny(s.off_peak.output, s)) }} / {{ fmt(cny(s.off_peak.cached, s)) }}</span>
                  </template>
                </template>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="caption">每个模型每 5 小时最多用掉月额度的 20%、每周 50%，超出后 OpenCode 会拒绝请求（可以在 OpenCode 控制台打开 Extra Usage 改为按量付费）。</p>
      <div class="actions">
        <span v-if="result" class="caption ok">{{ result }}</span>
        <button class="btn btn-primary" :disabled="!picked.size || busy || (needKey && !opts.api_key.trim())" @click="submit">
          {{ busy ? '正在导入' : `导入选中的 ${picked.size} 个模型` }}
        </button>
      </div>
      <p v-if="needKey && !opts.api_key.trim()" class="caption right">有还没添加过的模型，要先填 API Key。</p>
    </template>
    <div v-else class="skeleton skel" aria-hidden="true" />
  </section>
</template>

<style scoped>
h2 { margin: 0 0 10px; }
.skel { height: 200px; border-radius: var(--radius-md); }
.intro { margin: 0 0 10px; line-height: 1.7; }
.warn { border-color: rgba(181, 120, 40, .35); background: rgba(181, 120, 40, .08); color: var(--color-ink); font-size: 13px; line-height: 1.7; }
.grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 4px 12px; margin-top: 12px; align-items: end; }
.grid .field__label { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.hint { margin: -4px 0 8px; }
.table th:nth-child(2), .table td:nth-child(2) { min-width: 170px; }
.table-head { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; margin: 14px 0 6px; flex-wrap: wrap; }
.links { display: flex; gap: 12px; }
.link { padding: 0; text-decoration: underline; text-underline-offset: 3px; }
.table-wrap { overflow-x: auto; }
.table { width: 100%; font-size: 13px; }
.table th { white-space: nowrap; }
.table td { vertical-align: top; }
.table tbody tr { cursor: pointer; }
.table tr.is-picked { background: var(--color-accent-tint, rgba(91, 122, 74, .08)); }
.num { font-variant-numeric: tabular-nums; white-space: nowrap; }
.strong { color: var(--color-ink); font-weight: 500; }
.name { color: var(--color-ink); font-weight: 500; }
.note { margin: 2px 0 0; max-width: 260px; line-height: 1.5; white-space: normal; }
.actions { display: flex; justify-content: flex-end; align-items: center; gap: 12px; margin-top: 12px; }
.ok { color: var(--color-accent-active); }
.right { text-align: right; margin: 4px 0 0; }
@media (max-width: 760px) { .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
