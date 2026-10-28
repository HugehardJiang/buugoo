<script setup lang="ts">
// 管理后台 → 会员套餐：新建、修改、显示或隐藏、删除套餐。改套餐只影响之后的兑换，已经开通的不变。
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, type Plan } from '../api'
import { planSummary, TOKEN_UNIT, YUAN } from '../format'
import Modal from './Modal.vue'
import { toast } from '../toast'

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

const plans = ref<Plan[] | null>(null)
const error = ref('')
const editing = ref<Plan | null>(null)
const isNew = ref(false)
const saving = ref(false)
const formError = ref('')
const removing = ref<Plan | null>(null)

const blank = (): Plan => ({
  id: 0, name: '', description: '', price: '', kind: 'period', days: 30, cycle: 'day',
  quota_messages: 200, quota_tokens: 0, quota_searches: 0, amount: 10 * YUAN, buy_url: '', visible: true, sort: 0,
})

// 表单里按好填的单位：token 以百万计，金额以元计；空着表示不限。
const form = reactive({ messages: '' as number | '', mtokens: '' as number | '', searches: '' as number | '', yuan: 10 as number | '' })

function fillForm(p: Plan) {
  form.messages = p.quota_messages || ''
  form.mtokens = p.quota_tokens ? +(p.quota_tokens / TOKEN_UNIT).toFixed(3) : ''
  form.searches = p.quota_searches || ''
  form.yuan = p.amount ? +(p.amount / YUAN).toFixed(4) : 10
}

// toPlan 把表单换算回接口用的单位。
function toPlan(p: Plan): Plan {
  return {
    ...p, days: Number(p.days), sort: Number(p.sort ?? 0),
    quota_messages: Math.round(Number(form.messages) || 0),
    quota_tokens: Math.round((Number(form.mtokens) || 0) * TOKEN_UNIT),
    quota_searches: Math.round(Number(form.searches) || 0),
    amount: Math.round((Number(form.yuan) || 0) * YUAN),
  }
}

onMounted(load)

async function load() {
  try {
    plans.value = (await props.call<{ plans: Plan[] }>('GET', '/plans')).plans
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

function openNew() {
  editing.value = blank()
  fillForm(editing.value)
  isNew.value = true
  formError.value = ''
}

function openEdit(p: Plan) {
  editing.value = { ...p }
  fillForm(p)
  isNew.value = false
  formError.value = ''
}

const preview = computed(() => (editing.value ? planSummary(toPlan(editing.value)) : ''))

async function save() {
  const p = editing.value
  if (!p) return
  saving.value = true
  formError.value = ''
  try {
    const body = toPlan(p)
    if (isNew.value) await props.call('POST', '/plans', body)
    else await props.call('PUT', `/plans/${p.id}`, body)
    toast(isNew.value ? `已新建「${p.name}」` : `已保存「${p.name}」`)
    editing.value = null
    await load()
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function toggle(p: Plan) {
  const on = !p.visible
  try {
    await props.call('POST', `/plans/${p.id}/visible`, { visible: on })
    p.visible = on
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  }
}

async function confirmRemove() {
  const p = removing.value
  removing.value = null
  if (!p) return
  try {
    await props.call('DELETE', `/plans/${p.id}`)
    toast(`已删除「${p.name}」`)
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '删除失败')
  }
}
</script>

<template>
  <section class="panel panel--inset">
    <div class="head">
      <h2>会员套餐</h2>
      <button type="button" class="btn btn-sm btn-primary" @click="openNew">新建套餐</button>
    </div>
    <p class="muted intro">
      时长套餐在有效期内每天（或每月）给固定额度：消息、token、联网搜索各自限，不填的不限；额度用完后按「计费」里的价格扣余额。
      充值套餐兑换后把钱加到余额里。显示的套餐会出现在用户「个人中心 → 会员」里，可以附上购买链接（发卡平台、店铺等），用户买到卡密后在那里兑换。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <div v-else-if="plans === null" class="skeleton skel" aria-hidden="true" />
    <p v-else-if="!plans.length" class="caption">还没有套餐。</p>
    <ul v-else class="plans">
      <li v-for="p in plans" :key="p.id" class="plan" :class="{ 'is-hidden': !p.visible }">
        <div class="plan__main">
          <p class="plan__name">
            {{ p.name }}
            <span class="badge-pill">{{ { period: '时长套餐', recharge: '充值', pack: '旧余额包 · 已停用' }[p.kind] }}</span>
            <span v-if="p.price" class="price">{{ p.price }}</span>
          </p>
          <p class="caption">{{ planSummary(p) }}<template v-if="p.buy_url"> · 有购买链接</template></p>
          <p v-if="p.description" class="caption desc">{{ p.description }}</p>
        </div>
        <div class="plan__ctl">
          <label v-if="p.kind !== 'pack'" class="check-row"><input type="checkbox" :checked="p.visible" @change="toggle(p)" /> 显示给用户</label>
          <button v-if="p.kind !== 'pack'" type="button" class="btn btn-sm" @click="openEdit(p)">修改</button>
          <button type="button" class="btn btn-sm btn-danger-lite" @click="removing = p">删除</button>
        </div>
      </li>
    </ul>

    <Modal :open="!!editing" :title="isNew ? '新建套餐' : '修改套餐'" subtitle="修改只影响之后的兑换和开通，已经开通的用户不变。" @close="editing = null">
      <form v-if="editing" @submit.prevent="save">
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="pl-n">名称</label>
            <input id="pl-n" v-model="editing.name" class="text-input" maxlength="30" placeholder="月卡" required />
          </div>
          <div class="field">
            <label class="field__label" for="pl-p">价格（只用来显示）</label>
            <input id="pl-p" v-model="editing.price" class="text-input" maxlength="30" placeholder="¥19.9 / 月" />
          </div>
        </div>
        <div class="field">
          <span class="field__label">种类</span>
          <div class="type-switch" role="group">
            <button type="button" :class="{ 'is-active': editing.kind === 'period' }" @click="editing.kind = 'period'">时长套餐</button>
            <button type="button" :class="{ 'is-active': editing.kind === 'recharge' }" @click="editing.kind = 'recharge'">充值</button>
          </div>
        </div>
        <template v-if="editing.kind === 'period'">
          <div class="form-row">
            <div class="field">
              <label class="field__label" for="pl-d">时长（天）</label>
              <input id="pl-d" v-model.number="editing.days" class="text-input" type="number" min="1" max="3660" required />
            </div>
            <div class="field">
              <label class="field__label" for="pl-c">额度周期</label>
              <select id="pl-c" v-model="editing.cycle" class="text-input">
                <option value="day">每天</option>
                <option value="month">每个自然月</option>
              </select>
            </div>
          </div>
          <p class="field__label">每周期的额度（空着表示不限）</p>
          <div class="quota-grid">
            <div class="with-unit">
              <input v-model.number="form.messages" class="text-input" type="number" min="0" step="1" placeholder="不限" aria-label="消息条数" />
              <span class="caption">条消息</span>
            </div>
            <div class="with-unit">
              <input v-model.number="form.mtokens" class="text-input" type="number" min="0" step="any" placeholder="不限" aria-label="token" />
              <span class="caption">百万 token</span>
            </div>
            <div class="with-unit">
              <input v-model.number="form.searches" class="text-input" type="number" min="0" step="1" placeholder="不限" aria-label="联网搜索次数" />
              <span class="caption">次搜索</span>
            </div>
          </div>
          <p class="caption hint">哪一项先用完就停在哪一项：消息或 token 用完后有余额就接着扣余额；搜索次数用完只是不能联网搜索，聊天照常。</p>
        </template>
        <div v-else class="field">
          <label class="field__label" for="pl-a">到账金额</label>
          <div class="with-unit">
            <input id="pl-a" v-model.number="form.yuan" class="text-input" type="number" min="0.01" step="any" required />
            <span class="caption">元</span>
          </div>
          <p class="field__hint">可以和价格不一样，例如卖 ¥10 到账 ¥11。</p>
        </div>
        <p class="caption preview">用户会看到：{{ preview }}</p>
        <div class="field">
          <label class="field__label" for="pl-desc">介绍（可选）</label>
          <textarea id="pl-desc" v-model="editing.description" class="textarea small" maxlength="2000" placeholder="适合每天都聊的你，支持联网搜索和运行代码" />
        </div>
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="pl-url">购买链接（可选）</label>
            <input id="pl-url" v-model="editing.buy_url" class="text-input" placeholder="https://" />
          </div>
          <div class="field">
            <label class="field__label" for="pl-s">排序（小的在前）</label>
            <input id="pl-s" v-model.number="editing.sort" class="text-input" type="number" />
          </div>
        </div>
        <label class="check-row"><input v-model="editing.visible" type="checkbox" /> 显示给用户</label>
        <p v-if="formError" class="form-flash form-flash--error">{{ formError }}</p>
        <div class="modal-actions">
          <button type="button" class="btn" @click="editing = null">取消</button>
          <button class="btn btn-primary" :disabled="saving">{{ saving ? '正在保存' : '保存' }}</button>
        </div>
      </form>
    </Modal>

    <Modal :open="!!removing" :title="`删除「${removing?.name}」？`" subtitle="已经开通的用户不受影响。还有没兑换的卡密时不能删，可以先把套餐隐藏起来。" @close="removing = null">
      <div class="modal-actions">
        <button class="btn" @click="removing = null">取消</button>
        <button class="btn btn-danger" @click="confirmRemove">删除</button>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
.with-unit { display: flex; align-items: center; gap: 8px; }
.with-unit .text-input { flex: 1; min-width: 0; }
.with-unit .caption { white-space: nowrap; }
.head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.head h2 { margin: 0; }
.intro { margin: 8px 0 12px; font-size: 14px; line-height: 1.7; }
.skel { height: 80px; border-radius: var(--radius-md); }
.plans { margin: 0; padding: 0; list-style: none; }
.plan { display: flex; align-items: flex-start; gap: 12px; padding: 14px 0; border-top: 1px solid var(--color-divider); }
.plan.is-hidden .plan__main { opacity: .55; }
.plan__main { flex: 1; min-width: 0; }
.plan__main p { margin: 0 0 3px; }
.plan__name { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; color: var(--color-ink); font-size: 15px; font-weight: 500; }
.price { color: var(--color-accent-active); font-size: 14px; }
.desc { overflow-wrap: anywhere; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.plan__ctl { display: flex; align-items: center; flex-wrap: wrap; justify-content: flex-end; gap: 6px 10px; }
.plan__ctl .check-row { margin: 0; white-space: nowrap; }
.preview { margin: -6px 0 14px; }
.quota-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; margin: 6px 0 8px; }
.hint { margin: 0 0 14px; }
@media (max-width: 560px) { .quota-grid { grid-template-columns: minmax(0, 1fr); } }
.textarea.small { min-height: 70px; }
@media (max-width: 640px) { .plan { flex-direction: column; } .plan__ctl { justify-content: flex-start; } }
</style>
