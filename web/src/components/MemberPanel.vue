<script setup lang="ts">
// 个人中心 → 会员：现在的套餐、余额和消费、这个周期用了多少，可以买的套餐（带购买链接），兑换卡密。
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type BillingAccount, type Plan, type Quota } from '../api'
import { addonPerms, dateTime, mtok, planSummary, yuan } from '../format'
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

      <template v-if="account.addons?.length">
        <h3 class="sec">附加包</h3>
        <div v-for="a in account.addons" :key="a.id" class="line">
          <div class="row__label">
            <span>{{ a.plan_name }}</span>
            <small>{{ addonPerms(a) }}</small>
          </div>
          <span class="caption">{{ a.expires_at ? `${dateTime(a.expires_at)} 到期` : '永久有效' }}</span>
        </div>
      </template>

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
        <p class="caption">买到卡密后在上面兑换。续费同一个时长套餐或附加包会接着现在的到期时间往后加；附加包只开通功能、不影响额度，可以和时长套餐同时有；充值的钱一直有效。</p>
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
/* 用量、余额与套餐卡片：统一 24px 外卡 + 18px 暖米子卡 + 极细分割线 */
.usage__head { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; }
.usage__head h2 { margin: 0; font-family: var(--font-sans); font-size: 20px; font-weight: 500; letter-spacing: -.02em; }
.usage__plan {
  display: inline-flex; align-items: center; padding: 2px 12px; border-radius: 9999px;
  background: var(--md-primary-container); color: var(--md-on-primary-container); font-size: 12.5px; font-weight: 500;
}
.usage__summary {
  margin: 0 0 8px; padding-bottom: 16px;
  border-bottom: 1px solid var(--color-hairline-subtle, rgba(20, 20, 19, .08));
  color: var(--color-ink); font-size: 15.5px; line-height: 1.6;
}
.skel { height: 140px; border-radius: 18px; }
.rows { display: grid; }
.row { display: grid; grid-template-columns: minmax(0, 240px) minmax(0, 1fr) 96px; align-items: center; gap: 24px; padding: 15px 0; }
.row__label { display: grid; gap: 3px; min-width: 0; }
.row__label span { color: var(--color-ink); font-size: 14.5px; font-weight: 500; }
.row__label small { color: var(--color-muted); font-size: 12.5px; }
.bar { height: 7px; overflow: hidden; border-radius: var(--radius-pill); background: var(--md-surface-container-high); }
.bar i { display: block; height: 100%; border-radius: inherit; background: var(--color-accent); transition: width .5s var(--ease-editorial); }
.bar i.is-high { background: var(--color-warning); }
.row__pct { color: var(--color-text-secondary); font-size: 13.5px; font-variant-numeric: tabular-nums; text-align: right; white-space: nowrap; }
.sec { margin: 26px 0 6px; font-size: 15px; font-weight: 500; letter-spacing: -.01em; color: var(--color-ink); }
.line {
  display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 0 16px;
  border-bottom: 1px solid var(--color-hairline-subtle, rgba(20, 20, 19, .08));
}
.balance { color: var(--color-ink); font-size: 20px; font-weight: 500; letter-spacing: -.02em; font-variant-numeric: tabular-nums; }
.neg { color: var(--color-danger) !important; }
.redeem { padding: 24px 0 18px; }
.redeem__row { display: flex; gap: 8px; margin-top: 8px; max-width: 540px; }
.redeem__row .text-input { flex: 1; min-width: 0; }
.mono { font-family: var(--font-mono); letter-spacing: .04em; }
.plans { padding-top: 6px; }
.plan-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(248px, 1fr)); gap: 12px; margin: 10px 0 12px; }
.plan {
  display: flex; flex-direction: column; gap: 8px; padding: 18px 20px;
  border: 0; border-radius: 20px; background: var(--md-surface-container-lowest);
  transition: box-shadow var(--motion-fast) var(--md-ease), transform var(--motion-fast) var(--md-ease);
}
.plan:hover { box-shadow: var(--md-elev-1); transform: translateY(-2px); }
.plan__head { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
.plan h3 { margin: 0; font-size: 16px; font-weight: 500; letter-spacing: -.015em; }
.price { color: var(--color-accent-active); font-weight: 600; font-variant-numeric: tabular-nums; white-space: nowrap; }
.plan p { margin: 0; }
.desc { font-size: 13px; color: var(--color-text-secondary); line-height: 1.6; white-space: pre-wrap; }
.buy { align-self: flex-start; margin-top: auto; padding-top: 0; }
.link { padding: 0; margin-top: 10px; text-decoration: underline; text-underline-offset: 3px; }
.logs { margin: 10px 0 0; padding-left: 18px; font-size: 13px; line-height: 1.85; }
@media (max-width: 640px) {
  .row { grid-template-columns: minmax(0, 1fr) auto; gap: 8px 16px; }
  .bar { grid-column: 1 / -1; grid-row: 2; }
  .redeem__row { max-width: none; }
}
</style>
