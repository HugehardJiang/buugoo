<script setup lang="ts">
// 管理后台 → 会员套餐：新建、修改、显示或隐藏、删除套餐。改套餐只影响之后的兑换，已经开通的不变。
import { computed, onMounted, ref } from 'vue'
import { ApiError, type Plan } from '../api'
import { planSummary } from '../format'
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

const blank = (): Plan => ({ id: 0, name: '', description: '', price: '', kind: 'period', unit: 'messages', days: 30, cycle: 'day', quota: 200, valid_days: 0, buy_url: '', visible: true, sort: 0 })

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
  isNew.value = true
  formError.value = ''
}

function openEdit(p: Plan) {
  editing.value = { ...p }
  isNew.value = false
  formError.value = ''
}

const preview = computed(() => (editing.value ? planSummary(editing.value) : ''))

async function save() {
  const p = editing.value
  if (!p) return
  saving.value = true
  formError.value = ''
  try {
    const body = { ...p, days: Number(p.days), quota: Number(p.quota), valid_days: Number(p.valid_days), sort: Number(p.sort ?? 0) }
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
      时长套餐在有效期内每天（或每月）给固定额度，代替免费额度；余额包一次给一笔额度，周期额度用完后再用它。
      显示的套餐会出现在用户「设置 → 会员」里，可以附上购买链接（发卡平台、店铺等），用户买到卡密后在那里兑换。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <div v-else-if="plans === null" class="skeleton skel" aria-hidden="true" />
    <p v-else-if="!plans.length" class="caption">还没有套餐。</p>
    <ul v-else class="plans">
      <li v-for="p in plans" :key="p.id" class="plan" :class="{ 'is-hidden': !p.visible }">
        <div class="plan__main">
          <p class="plan__name">
            {{ p.name }}
            <span class="badge-pill">{{ p.kind === 'period' ? '时长套餐' : '余额包' }}</span>
            <span v-if="p.price" class="price">{{ p.price }}</span>
          </p>
          <p class="caption">{{ planSummary(p) }}<template v-if="p.buy_url"> · 有购买链接</template></p>
          <p v-if="p.description" class="caption desc">{{ p.description }}</p>
        </div>
        <div class="plan__ctl">
          <label class="check-row"><input type="checkbox" :checked="p.visible" @change="toggle(p)" /> 显示给用户</label>
          <button type="button" class="btn btn-sm" @click="openEdit(p)">修改</button>
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
            <button type="button" :class="{ 'is-active': editing.kind === 'pack' }" @click="editing.kind = 'pack'">余额包</button>
          </div>
        </div>
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="pl-u">计费单位</label>
            <select id="pl-u" v-model="editing.unit" class="text-input">
              <option value="messages">消息条数</option>
              <option value="tokens">token</option>
            </select>
          </div>
          <div class="field">
            <label class="field__label" for="pl-q">{{ editing.kind === 'period' ? '每周期额度' : '总额度' }}</label>
            <input id="pl-q" v-model.number="editing.quota" class="text-input" type="number" min="1" required />
          </div>
        </div>
        <div v-if="editing.kind === 'period'" class="form-row">
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
        <div v-else class="field">
          <label class="field__label" for="pl-v">有效期（天，0 表示永久）</label>
          <input id="pl-v" v-model.number="editing.valid_days" class="text-input" type="number" min="0" max="3660" />
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
.textarea.small { min-height: 70px; }
@media (max-width: 640px) { .plan { flex-direction: column; } .plan__ctl { justify-content: flex-start; } }
</style>
