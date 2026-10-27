<script setup lang="ts">
// 个人中心 → 会员：现在的套餐和余额、这个周期用了多少，可以买的套餐（带购买链接），兑换卡密。
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type BillingAccount, type Plan, type Quota } from '../api'
import { amount, dateTime, mtok, planSummary } from '../format'
import { toast } from '../toast'

const account = ref<BillingAccount | null>(null)
const quota = ref<Quota | null>(null)
const plans = ref<Plan[]>([])
const loadError = ref('')
const code = ref('')
const redeeming = ref(false)
const redeemError = ref('')
const showLogs = ref(false)

const usage = computed(() => {
  const q = quota.value
  if (!q) return []
  const out: { label: string; used: number; limit: number; unit: string }[] = []
  if (q.messages_limit) out.push({ label: '消息', used: q.messages, limit: q.messages_limit, unit: 'messages' })
  if (q.tokens_limit) out.push({ label: '百万 token', used: q.tokens, limit: q.tokens_limit, unit: 'tokens' })
  return out
})

onMounted(load)

async function load() {
  try {
    const r = await api.billing()
    account.value = r.account
    quota.value = r.quota
    plans.value = r.plans
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
</script>

<template>
  <section id="member" class="panel panel--inset">
    <h2>会员</h2>
    <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
    <template v-else-if="account && quota">
      <div class="status">
        <div class="status__plan">
          <p class="caption">当前</p>
          <p v-if="account.subscription" class="plan-name">{{ account.subscription.plan_name }}</p>
          <p v-else class="plan-name">免费用户</p>
          <p v-if="account.subscription" class="caption">{{ dateTime(account.subscription.expires_at) }} 到期</p>
          <p v-if="quota.exempt" class="caption">管理员不受限额限制</p>
        </div>
        <div class="status__usage">
          <p class="caption">{{ quota.period === 'month' ? '本月' : '今天' }}已用</p>
          <template v-if="usage.length">
            <div v-for="u in usage" :key="u.label" class="meter">
              <div class="meter__label"><span>{{ u.label }}</span><span>{{ u.unit === 'tokens' ? `${mtok(u.used)} / ${mtok(u.limit)}` : `${u.used} / ${u.limit}` }}</span></div>
              <div class="workspace-progress"><i :style="{ width: `${pct(u.used, u.limit)}%` }" /></div>
            </div>
          </template>
          <p v-else class="caption">不限</p>
          <p v-if="quota.reset_at" class="caption">{{ dateTime(quota.reset_at) }} 重置</p>
        </div>
      </div>
      <div v-if="account.packs.length" class="packs">
        <p class="caption">余额包（周期额度用完后再用）</p>
        <ul>
          <li v-for="p in account.packs" :key="p.id">
            <span>{{ p.name }}</span>
            <span>剩 {{ amount(p.unit, p.remaining) }}<span class="caption">{{ p.expires_at ? ` · ${dateTime(p.expires_at)} 过期` : '' }}</span></span>
          </li>
        </ul>
      </div>

      <form class="redeem" @submit.prevent="redeem">
        <label class="field__label" for="redeem-code">兑换卡密</label>
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
        <p class="caption">买到卡密后在上面兑换。续费同一个套餐会接着现在的到期时间往后加。</p>
      </div>

      <button v-if="account.logs.length" type="button" class="link caption" @click="showLogs = !showLogs">{{ showLogs ? '收起记录' : '查看兑换记录' }}</button>
      <ul v-if="showLogs" class="logs">
        <li v-for="l in account.logs" :key="l.id"><span class="caption">{{ dateTime(l.created_at) }}</span> {{ l.detail }}</li>
      </ul>
    </template>
    <div v-else class="skeleton skel" aria-hidden="true" />
  </section>
</template>

<style scoped>
h2 { margin: 0 0 14px; }
.skel { height: 140px; border-radius: var(--radius-md); }
.status { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr); gap: 20px; padding-bottom: 16px; border-bottom: 1px solid var(--color-divider); }
.status p { margin: 0 0 4px; }
.plan-name { font-family: var(--font-display); font-size: 22px; color: var(--color-ink); }
.meter { margin-bottom: 8px; }
.meter__label { display: flex; justify-content: space-between; font-size: 13px; margin-bottom: 4px; }
.packs { padding: 12px 0; border-bottom: 1px solid var(--color-divider); }
.packs ul { margin: 6px 0 0; padding: 0; list-style: none; }
.packs li { display: flex; justify-content: space-between; gap: 12px; padding: 4px 0; font-size: 14px; }
.redeem { padding: 16px 0; }
.redeem__row { display: flex; gap: 8px; margin-top: 6px; }
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
@media (max-width: 560px) { .status { grid-template-columns: minmax(0, 1fr); } }
</style>
