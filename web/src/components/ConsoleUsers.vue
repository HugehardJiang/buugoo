<script setup lang="ts">
// 管理后台 → 用户管理：搜索、筛选、查看用量和会员，开通套餐、调整余额、重置密码、设管理员、停用、删除。
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ApiError, type BillingAccount, type Plan, type Quota } from '../api'
import { amount, dateTime, mtok, shortWhen, TOKEN_UNIT } from '../format'
import Modal from './Modal.vue'
import { toast } from '../toast'

interface Row {
  id: number
  username: string
  display_name: string
  is_admin: boolean
  disabled_at?: string
  disabled_reason?: string
  created_at: string
  plan_name?: string
  plan_expires?: string
  pack_messages: number
  pack_tokens: number
  today_messages: number
  today_tokens: number
  total_messages: number
  total_tokens: number
  last_active?: string
}
interface Detail {
  user: { id: number; username: string; display_name: string; is_admin: boolean; created_at: string }
  disabled_at?: string
  disabled_reason?: string
  account: BillingAccount
  quota: Quota
}

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

const rows = ref<Row[] | null>(null)
const total = ref(0)
const error = ref('')
const q = ref('')
const status = ref('')
const page = ref(0)
const pageSize = 30
const plans = ref<Plan[]>([])

const detail = ref<Detail | null>(null)
const busy = ref(false)
const grant = reactive({ plan_id: 0, note: '' })
const adjust = reactive({ unit: 'messages', amount: 0, note: '' })
const newPassword = ref('')
const disableReason = ref('')
const confirmDelete = ref('')
const showDelete = ref(false)

onMounted(async () => {
  await load()
  try {
    plans.value = (await props.call<{ plans: Plan[] }>('GET', '/plans')).plans
    grant.plan_id = plans.value[0]?.id ?? 0
  } catch {
    // 套餐加载失败不影响用户列表
  }
})

async function load() {
  try {
    const qs = new URLSearchParams({ limit: String(pageSize), offset: String(page.value * pageSize) })
    if (q.value.trim()) qs.set('q', q.value.trim())
    if (status.value) qs.set('status', status.value)
    const r = await props.call<{ users: Row[]; total: number }>('GET', `/users?${qs}`)
    rows.value = r.users
    total.value = r.total
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

let timer: ReturnType<typeof setTimeout> | undefined
watch([q, status], () => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    page.value = 0
    load()
  }, 300)
})
const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

async function open(id: number) {
  try {
    detail.value = await props.call<Detail>('GET', `/users/${id}`)
    newPassword.value = ''
    disableReason.value = ''
    adjust.amount = 0
    adjust.note = ''
    grant.note = ''
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '加载失败')
  }
}

async function act(path: string, body: unknown, ok: string) {
  const d = detail.value
  if (!d || busy.value) return
  busy.value = true
  try {
    const r = await props.call<{ message?: string } | undefined>('POST', `/users/${d.user.id}/${path}`, body)
    toast(r?.message ? `${ok}：${r.message}` : ok)
    await open(d.user.id)
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  } finally {
    busy.value = false
  }
}

async function remove() {
  const d = detail.value
  if (!d) return
  try {
    await props.call('POST', `/users/${d.user.id}/delete`, { confirm: confirmDelete.value })
    toast(`已删除 ${d.user.username}`)
    showDelete.value = false
    detail.value = null
    confirmDelete.value = ''
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '删除失败')
  }
}

function usageText(r: { messages: number; tokens: number; messages_limit: number; tokens_limit: number }) {
  const m = r.messages_limit ? `${r.messages}/${r.messages_limit} 条` : `${r.messages} 条`
  const t = r.tokens_limit ? `${mtok(r.tokens)}/${mtok(r.tokens_limit)} 百万 token` : `${mtok(r.tokens)} 百万 token`
  return `${m} · ${t}`
}
</script>

<template>
  <section class="panel panel--inset">
    <h2>用户管理</h2>
    <div class="filters">
      <input v-model="q" class="text-input" type="search" placeholder="搜用户名或昵称" aria-label="搜索用户" />
      <div class="type-switch" role="group" aria-label="筛选">
        <button v-for="o in [['', '全部'], ['member', '会员'], ['disabled', '已停用'], ['admin', '管理员']]" :key="o[0]" type="button"
          :class="{ 'is-active': status === o[0] }" @click="status = o[0]">{{ o[1] }}</button>
      </div>
    </div>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <div v-else-if="rows === null" class="skeleton skel" aria-hidden="true" />
    <p v-else-if="!rows.length" class="caption">没有找到用户。</p>
    <div v-else class="table-wrap">
      <table class="table">
        <thead>
          <tr><th>用户</th><th>会员</th><th>余额</th><th>今天</th><th>累计</th><th>最近活跃</th><th>注册</th></tr>
        </thead>
        <tbody>
          <tr v-for="r in rows" :key="r.id" class="row" tabindex="0" @click="open(r.id)" @keydown.enter="open(r.id)">
            <td>
              <span class="name">{{ r.username }}</span>
              <span v-if="r.is_admin" class="badge-pill">管理员</span>
              <span v-if="r.disabled_at" class="badge-pill badge-pill--off">已停用</span>
              <span v-if="r.display_name" class="caption"><br />{{ r.display_name }}</span>
            </td>
            <td><template v-if="r.plan_name">{{ r.plan_name }}<span class="caption"><br />{{ shortWhen(r.plan_expires!) }} 到期</span></template><span v-else class="caption">免费</span></td>
            <td class="caption">
              <template v-if="r.pack_messages">{{ amount('messages', r.pack_messages) }}<br /></template>
              <template v-if="r.pack_tokens">{{ amount('tokens', r.pack_tokens) }}</template>
              <template v-if="!r.pack_messages && !r.pack_tokens">—</template>
            </td>
            <td class="caption">{{ r.today_messages }} 条<br />{{ mtok(r.today_tokens) }} 百万 token</td>
            <td class="caption">{{ r.total_messages }} 条<br />{{ mtok(r.total_tokens) }} 百万 token</td>
            <td class="caption">{{ r.last_active || '—' }}</td>
            <td class="caption">{{ shortWhen(r.created_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="pages > 1" class="pager">
      <button type="button" class="btn btn-sm" :disabled="page === 0" @click="page--; load()">上一页</button>
      <span class="caption">{{ page + 1 }} / {{ pages }}（共 {{ total }} 人）</span>
      <button type="button" class="btn btn-sm" :disabled="page + 1 >= pages" @click="page++; load()">下一页</button>
    </div>

    <Modal :open="!!detail" :title="detail?.user.username ?? ''" :subtitle="detail?.user.display_name" wide @close="detail = null">
      <template v-if="detail">
        <div class="grid">
          <div class="box">
            <h3>概况</h3>
            <p class="caption">注册于 {{ dateTime(detail.user.created_at) }}<template v-if="detail.user.is_admin"> · 管理员</template></p>
            <p v-if="detail.disabled_at" class="form-flash form-flash--error">已停用（{{ dateTime(detail.disabled_at) }}）{{ detail.disabled_reason ? `：${detail.disabled_reason}` : '' }}</p>
            <p>当前周期用量：{{ usageText(detail.quota) }}<span class="caption">（{{ detail.quota.period === 'month' ? '每月' : '每天' }}重置）</span></p>
            <p v-if="detail.account.subscription">会员：<strong>{{ detail.account.subscription.plan_name }}</strong>，{{ dateTime(detail.account.subscription.expires_at) }} 到期</p>
            <p v-else class="caption">没有开通时长套餐</p>
            <ul v-if="detail.account.packs.length" class="packs">
              <li v-for="p in detail.account.packs" :key="p.id">{{ p.name }}：剩 {{ amount(p.unit, p.remaining) }} / {{ amount(p.unit, p.total) }}<span class="caption">{{ p.expires_at ? `，${dateTime(p.expires_at)} 过期` : '' }}</span></li>
            </ul>
          </div>
          <div class="box">
            <h3>开通套餐</h3>
            <div class="inline">
              <select v-model.number="grant.plan_id" class="text-input" aria-label="套餐">
                <option v-for="p in plans" :key="p.id" :value="p.id">{{ p.name }}</option>
              </select>
              <input v-model="grant.note" class="text-input" placeholder="备注（可选）" maxlength="50" aria-label="备注" />
              <button type="button" class="btn btn-sm btn-primary" :disabled="busy || !grant.plan_id" @click="act('grant', { ...grant }, '已开通')">开通</button>
            </div>
            <h3>调整余额</h3>
            <div class="inline">
              <select v-model="adjust.unit" class="text-input" aria-label="单位"><option value="messages">条消息</option><option value="tokens">百万 token</option></select>
              <input v-model.number="adjust.amount" class="text-input num" type="number" step="any" :placeholder="adjust.unit === 'tokens' ? '+2 或 -0.5' : '+100 或 -50'" aria-label="数量" />
              <input v-model="adjust.note" class="text-input" placeholder="备注（可选）" maxlength="50" aria-label="备注" />
              <button type="button" class="btn btn-sm" :disabled="busy || !adjust.amount" @click="act('adjust', { ...adjust, amount: adjust.unit === 'tokens' ? Math.round(adjust.amount * TOKEN_UNIT) : adjust.amount }, '已调整')">调整</button>
            </div>
          </div>
        </div>

        <div class="box">
          <h3>账号</h3>
          <div class="inline">
            <input v-model="newPassword" class="text-input" type="text" autocomplete="off" placeholder="新密码（至少 8 位）" aria-label="新密码" />
            <button type="button" class="btn btn-sm" :disabled="busy || newPassword.length < 8" @click="act('password', { password: newPassword }, '密码已重置，他之前的登录都已失效')">重置密码</button>
            <button type="button" class="btn btn-sm" :disabled="busy" @click="act('admin', { admin: !detail.user.is_admin }, detail.user.is_admin ? '已取消管理员' : '已设为管理员')">
              {{ detail.user.is_admin ? '取消管理员' : '设为管理员' }}
            </button>
          </div>
          <div class="inline">
            <template v-if="!detail.disabled_at">
              <input v-model="disableReason" class="text-input" placeholder="停用原因（可选，用户看不到）" maxlength="100" aria-label="停用原因" />
              <button type="button" class="btn btn-sm btn-danger-lite" :disabled="busy" @click="act('disable', { disabled: true, reason: disableReason }, '已停用')">停用账号</button>
            </template>
            <button v-else type="button" class="btn btn-sm" :disabled="busy" @click="act('disable', { disabled: false }, '已恢复')">恢复账号</button>
            <span class="spacer" />
            <button type="button" class="btn btn-sm btn-danger" :disabled="busy" @click="showDelete = true; confirmDelete = ''">删除用户</button>
          </div>
        </div>

        <div v-if="detail.account.logs.length" class="box">
          <h3>会员记录</h3>
          <ul class="logs">
            <li v-for="l in detail.account.logs" :key="l.id"><span class="caption">{{ dateTime(l.created_at) }}</span> {{ l.detail }}</li>
          </ul>
        </div>
      </template>
    </Modal>

    <Modal :open="showDelete" :title="`删除 ${detail?.user.username}？`" subtitle="对话、记忆、文件、备忘录、会员记录都会删除，不能恢复。用过的卡密会保留（只是不再关联这个用户）。" @close="showDelete = false">
      <form @submit.prevent="remove">
        <div class="field">
          <label class="field__label" for="del-confirm">输入用户名 <strong>{{ detail?.user.username }}</strong> 确认</label>
          <input id="del-confirm" v-model="confirmDelete" class="text-input" autocomplete="off" />
        </div>
        <div class="modal-actions">
          <button type="button" class="btn" @click="showDelete = false">取消</button>
          <button class="btn btn-danger" :disabled="confirmDelete !== detail?.user.username">永久删除</button>
        </div>
      </form>
    </Modal>
  </section>
</template>

<style scoped>
h2 { margin: 0 0 12px; }
.filters { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 12px; }
.filters .text-input { flex: 1; min-width: 180px; min-height: 36px; }
.skel { height: 160px; border-radius: var(--radius-md); }
.table-wrap { overflow-x: auto; }
.table { width: 100%; border-collapse: collapse; font-size: 13px; }
.table th { padding: 8px; border-bottom: 1px solid var(--color-divider); color: var(--color-muted); font-weight: 500; text-align: left; white-space: nowrap; }
.table td { padding: 9px 8px; border-bottom: 1px solid var(--color-divider); vertical-align: top; }
.row { cursor: pointer; transition: background-color .15s ease; }
.row:hover, .row:focus-visible { background: var(--color-hover-wash); outline: none; }
.name { margin-right: 6px; color: var(--color-ink); font-weight: 500; }
.badge-pill--off { background: var(--color-danger-tint); color: var(--color-danger); }
.pager { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 12px; }
.grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.box { margin-bottom: 12px; padding: 14px 16px; border: 1px solid var(--color-divider); border-radius: var(--radius-lg); }
.box h3 { margin: 0 0 8px; font-size: 14px; }
.box h3 + .inline { margin-bottom: 14px; }
.box p { margin: 0 0 6px; font-size: 14px; }
.inline { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 8px; }
.inline .text-input { width: auto; flex: 1; min-width: 120px; min-height: 34px; }
.inline .num { flex: 0 0 120px; }
.packs, .logs { margin: 6px 0 0; padding-left: 18px; font-size: 13px; line-height: 1.8; }
.spacer { flex: 1; }
@media (max-width: 760px) { .grid { grid-template-columns: minmax(0, 1fr); } }
</style>
