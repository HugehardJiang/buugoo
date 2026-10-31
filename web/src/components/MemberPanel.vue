<script setup lang="ts">
// 个人中心 → 会员：现在的套餐、余额和消费、这个周期用了多少，可以买的套餐（带购买链接），兑换卡密。
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type BillingAccount, type Plan, type Quota } from '../api'
import { dateTime, mtok, planSummary, yuan } from '../format'
import { toast } from '../toast'

const account = ref<BillingAccount | null>(null)
const quota = ref<Quota | null>(null)
const plans = ref<Plan[]>([])
const loadError = ref('')
const code = ref('')
const redeeming = ref(false)
const redeemError = ref('')
const showLogs = ref(false)
const labels = ref<Record<string, string>>({})

const todayCost = computed(() => (account.value?.today ?? []).reduce((n, c) => n + c.amount, 0))
const monthCost = computed(() => (account.value?.month ?? []).reduce((n, c) => n + c.amount, 0))

const usage = computed(() => {
  const q = quota.value
  if (!q) return []
  const out: { label: string; used: number; limit: number; unit: string }[] = []
  if (q.messages_limit) out.push({ label: '消息', used: q.messages, limit: q.messages_limit, unit: 'messages' })
  if (q.tokens_limit) out.push({ label: '百万 token', used: q.tokens, limit: q.tokens_limit, unit: 'tokens' })
  if (q.searches_limit) out.push({ label: '联网搜索', used: q.searches, limit: q.searches_limit, unit: 'searches' })
  return out
})

onMounted(load)

async function load() {
  try {
    const r = await api.billing()
    account.value = r.account
    quota.value = r.quota
    plans.value = r.plans
    labels.value = r.labels ?? {}
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function redeem() {
  if (!code.value.trim() || redeeming.value) return
  redeeming.value = true
  redeemError.value = ''
  try {
    const r = await api.redeem(code.value)
    code.value = ''
    toast(`兑换成功：${r.message}`)
    await load()
  } catch (e) {
    redeemError.value = e instanceof ApiError ? e.message : '兑换失败'
  } finally {
    redeeming.value = false
  }
}

function pct(used: number, limit: number) {
  return Math.min(100, Math.round((used / limit) * 100))
}

// 顶部的一句话（参考 claude.ai 的用量页）。
const planName = computed(() => account.value?.subscription?.plan_name ?? (quota.value?.free ? '免费' : '按量付费'))
const maxPct = computed(() => usage.value.reduce((m, u) => Math.max(m, pct(u.used, u.limit)), 0))
const resetText = computed(() => {
  const q = quota.value
  if (!q?.reset_at) return q?.period === 'month' ? '每月重置' : '每天重置'
  return `${dateTime(q.reset_at)} 重置`
})
const summary = computed(() => {
  const q = quota.value
  if (!q) return ''
  if (q.exempt) return '你是管理员，不受额度限制。'
  if (q.paying) return q.free ? '这个周期的额度用完了，现在每次使用都按价格从余额里扣。' : '没有免费额度，每次使用按价格从余额里扣。'
  if (!usage.value.length) return q.free ? '额度不限，放心用。' : '没有免费额度，每次使用按价格从余额里扣。'
  if (maxPct.value >= 90) return `额度快用完了，${resetText.value}。`
  if (maxPct.value >= 60) return `已经用了一大半，${resetText.value}。`
  return `进度正常，在${resetText.value.replace(' 重置', '')}重置前额度还很充裕。`
})
// 本月各项的消费，按金额排序，条的长短是占本月总花费的比例。
const monthRows = computed(() => {
  const list = [...(account.value?.month ?? [])].sort((a, b) => b.amount - a.amount)
  const total = list.reduce((n, c) => n + c.amount, 0) || 1
  return list.map((c) => ({ ...c, share: Math.round((c.amount / total) * 100) }))
})
</script>

<template>
  <section id="member" class="panel panel--inset usage">
    <div class="usage__head">
      <h2>你的用量</h2>
      <span v-if="account" class="usage__plan">{{ planName }}</span>
    </div>
    <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
    <template v-else-if="account && quota">
      <p class="usage__summary">{{ summary }}</p>

      <div v-if="usage.length" class="rows">
        <div v-for="u in usage" :key="u.label" class="row">
          <div class="row__label">
            <span>{{ quota.period === 'month' ? '本月' : '今天' }}的{{ u.label }}</span>
            <small>{{ u.unit === 'tokens' ? `${mtok(u.used)} / ${mtok(u.limit)}` : `${u.used} / ${u.limit}` }} · {{ resetText }}</small>
          </div>
          <div class="bar" role="progressbar" :aria-valuenow="pct(u.used, u.limit)" aria-valuemin="0" aria-valuemax="100">
            <i :style="{ width: `${pct(u.used, u.limit)}%` }" :class="{ 'is-high': pct(u.used, u.limit) >= 90 }" />
          </div>
          <span class="row__pct">已使用 {{ pct(u.used, u.limit) }}%</span>
        </div>
      </div>

      <h3 class="sec">余额</h3>
      <div class="line">
        <div class="row__label">
          <span>可用余额</span>
          <small>今天花了 {{ yuan(todayCost) }} · 本月 {{ yuan(monthCost) }}<template v-if="account.subscription"> · 套餐 {{ dateTime(account.subscription.expires_at) }} 到期</template></small>
        </div>
        <strong class="balance" :class="{ neg: account.balance < 0 }">{{ yuan(account.balance) }}</strong>
      </div>

      <template v-if="monthRows.length">
        <h3 class="sec">本月各项的用量</h3>
        <div class="rows">
          <div v-for="c in monthRows" :key="c.key" class="row">
            <div class="row__label">
              <span>{{ labels[c.key] ?? c.key }}</span>
              <small>{{ c.tokens ? `${mtok(c.tokens)} 百万 token` : `${c.calls} 次` }}</small>
            </div>
            <div class="bar"><i :style="{ width: `${Math.max(c.share, 1)}%` }" /></div>
            <span class="row__pct">{{ yuan(c.amount) }}</span>
          </div>
        </div>
      </template>

      <form class="redeem" @submit.prevent="redeem">
        <label class="field__label" for="redeem-code">兑换卡密（开通套餐或充值）</label>
        <div class="redeem__row">
          <input id="redeem-code" v-model="code" class="text-input mono" autocomplete="off" spellcheck="false" placeholder="BG-XXXX-XXXX-XXXX-XXXX" maxlength="80" />
          <button class="btn btn-primary" :disabled="!code.trim() || redeeming">{{ redeeming ? '正在兑换' : '兑换' }}</button>
        </div>
        <p v-if="redeemError" class="form-flash form-flash--error">{{ redeemError }}</p>
      </form>

      <div v-if="plans.length" class="plans">
        <p class="caption">可以购买的套餐</p>
        <div class="plan-grid">
          <article v-for="p in plans" :key="p.id" class="plan">
            <div class="plan__head">
              <h3>{{ p.name }}</h3>
              <span v-if="p.price" class="price">{{ p.price }}</span>
            </div>
            <p class="caption">{{ planSummary(p) }}</p>
            <p v-if="p.description" class="desc">{{ p.description }}</p>
            <a v-if="p.buy_url" :href="p.buy_url" target="_blank" rel="noopener noreferrer" class="btn btn-sm buy">去购买</a>
          </article>
        </div>
        <p class="caption">买到卡密后在上面兑换。续费同一个时长套餐会接着现在的到期时间往后加；充值的钱一直有效。</p>
      </div>

      <button v-if="account.logs.length" type="button" class="link caption" @click="showLogs = !showLogs">{{ showLogs ? '收起记录' : '查看兑换和充值记录' }}</button>
      <ul v-if="showLogs" class="logs">
        <li v-for="l in account.logs" :key="l.id"><span class="caption">{{ dateTime(l.created_at) }}</span> {{ l.detail }}</li>
      </ul>
    </template>
    <div v-else class="skeleton skel" aria-hidden="true" />
  </section>
</template>

<style scoped>
/* 参考 claude.ai 的用量页：一句话总结，下面每一项一行（名称、细进度条、百分比），细线分隔 */
.usage__head { display: flex; align-items: baseline; gap: 10px; margin-bottom: 14px; }
.usage__head h2 { margin: 0; }
.usage__plan { color: var(--color-muted); font-size: 14px; }
.usage__summary { margin: 0 0 6px; padding-bottom: 14px; border-bottom: 1px solid var(--color-divider); color: var(--color-ink); font-size: 16px; }
.skel { height: 140px; border-radius: var(--radius-md); }
.rows { display: grid; }
.row { display: grid; grid-template-columns: minmax(0, 220px) minmax(0, 1fr) 96px; align-items: center; gap: 24px; padding: 16px 0; }
.row__label { display: grid; gap: 3px; min-width: 0; }
.row__label span { color: var(--color-ink); font-size: 15px; }
.row__label small { color: var(--color-muted); font-size: 13px; }
.bar { height: 8px; overflow: hidden; border-radius: 999px; background: var(--color-divider); }
.bar i { display: block; height: 100%; border-radius: inherit; background: var(--color-accent); transition: width .5s var(--ease-editorial); }
.bar i.is-high { background: var(--color-warning); }
.row__pct { color: var(--color-text-secondary); font-size: 14px; font-variant-numeric: tabular-nums; text-align: right; white-space: nowrap; }
.sec { margin: 28px 0 4px; font-size: 16px; font-weight: 600; }
.line { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 0 16px; border-bottom: 1px solid var(--color-divider); }
.balance { color: var(--color-ink); font-size: 18px; font-weight: 600; font-variant-numeric: tabular-nums; }
.neg { color: var(--color-danger) !important; }
.redeem { padding: 22px 0 16px; }
.redeem__row { display: flex; gap: 8px; margin-top: 8px; }
.redeem__row .text-input { flex: 1; min-width: 0; }
.mono { font-family: var(--font-mono); letter-spacing: .04em; }
.plans { padding-top: 4px; }
.plan-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 10px; margin: 8px 0; }
.plan { display: flex; flex-direction: column; gap: 6px; padding: 16px; border: 1px solid var(--color-divider); border-radius: var(--radius-lg); background: var(--color-canvas); }
.plan__head { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
.plan h3 { margin: 0; font-size: 16px; }
.price { color: var(--color-accent-active); font-weight: 600; white-space: nowrap; }
.plan p { margin: 0; }
.desc { font-size: 13px; color: var(--color-text-secondary); line-height: 1.6; white-space: pre-wrap; }
.buy { align-self: flex-start; margin-top: auto; }
.link { padding: 0; margin-top: 8px; text-decoration: underline; text-underline-offset: 3px; }
.logs { margin: 8px 0 0; padding-left: 18px; font-size: 13px; line-height: 1.8; }
@media (max-width: 640px) {
  .row { grid-template-columns: minmax(0, 1fr) auto; gap: 8px 16px; }
  .bar { grid-column: 1 / -1; grid-row: 2; }
}
</style>
