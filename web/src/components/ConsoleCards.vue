<script setup lang="ts">
// 管理后台 → 卡密：批量生成（原文只显示这一次，马上下载保存）、按套餐/状态/批次查看、停用、删除、导出。
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ApiError, type Plan } from '../api'
import { dateTime } from '../format'
import Modal from './Modal.vue'
import { toast } from '../toast'

interface Card {
  id: number
  hint: string
  plan_id: number
  plan_name: string
  batch: string
  note: string
  status: 'unused' | 'used' | 'disabled'
  expires_at: string | null
  used_by: number | null
  used_name?: string
  used_at: string | null
  created_at: string
}

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T>; token: string }>()

const plans = ref<Plan[]>([])
const cards = ref<Card[] | null>(null)
const total = ref(0)
const error = ref('')
const filter = reactive({ plan: '', status: '', batch: '', q: '' })
const page = ref(0)
const pageSize = 50
const selected = ref<Set<number>>(new Set())

const showGen = ref(false)
const gen = reactive({ plan_id: 0, count: 10, prefix: '', length: 16, expires: '', note: '' })
const generating = ref(false)
const genError = ref('')
const result = ref<{ batch: string; plan: string; codes: string[] } | null>(null)
const saved = ref(false)

const statusLabel = { unused: '未使用', used: '已使用', disabled: '已停用' }

onMounted(async () => {
  try {
    plans.value = (await props.call<{ plans: Plan[] }>('GET', '/plans')).plans.filter((p) => p.kind !== 'pack')
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
    return
  }
  await load()
})

function query() {
  const qs = new URLSearchParams()
  if (filter.plan) qs.set('plan', filter.plan)
  if (filter.status) qs.set('status', filter.status)
  if (filter.batch) qs.set('batch', filter.batch)
  if (filter.q.trim()) qs.set('q', filter.q.trim())
  return qs
}

async function load() {
  try {
    const qs = query()
    qs.set('limit', String(pageSize))
    qs.set('offset', String(page.value * pageSize))
    const r = await props.call<{ cards: Card[]; total: number }>('GET', `/cards?${qs}`)
    cards.value = r.cards
    total.value = r.total
    selected.value = new Set()
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

let timer: ReturnType<typeof setTimeout> | undefined
watch(filter, () => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    page.value = 0
    load()
  }, 300)
})

const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const allChecked = computed(() => !!cards.value?.length && cards.value.every((c) => selected.value.has(c.id)))

function toggleAll() {
  selected.value = allChecked.value ? new Set() : new Set(cards.value?.map((c) => c.id))
}
function toggleOne(id: number) {
  const s = new Set(selected.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  selected.value = s
}

function openGen() {
  gen.plan_id = plans.value[0]?.id ?? 0
  genError.value = ''
  showGen.value = true
}

async function generate() {
  generating.value = true
  genError.value = ''
  try {
    const body: Record<string, unknown> = { plan_id: gen.plan_id, count: Number(gen.count), prefix: gen.prefix, length: Number(gen.length), note: gen.note }
    if (gen.expires) body.expires_at = new Date(`${gen.expires}T23:59:59`).toISOString()
    result.value = await props.call('POST', '/cards/generate', body)
    saved.value = false
    showGen.value = false
    await load()
  } catch (e) {
    genError.value = e instanceof ApiError ? e.message : '生成失败'
  } finally {
    generating.value = false
  }
}

function download(name: string, text: string, type: string) {
  const url = URL.createObjectURL(new Blob([text], { type }))
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
  URL.revokeObjectURL(url)
  saved.value = true
}

function downloadTxt() {
  const r = result.value!
  download(`卡密-${r.plan}-${r.batch}.txt`, r.codes.join('\n') + '\n', 'text/plain;charset=utf-8')
}
function downloadCsv() {
  const r = result.value!
  download(`卡密-${r.plan}-${r.batch}.csv`, '\ufeff卡密,套餐,批次\n' + r.codes.map((c) => `${c},${r.plan},${r.batch}`).join('\n') + '\n', 'text/csv;charset=utf-8')
}
async function copyAll() {
  try {
    await navigator.clipboard.writeText(result.value!.codes.join('\n'))
    saved.value = true
    toast('已复制全部卡密')
  } catch {
    toast('复制失败，请用下载')
  }
}
function closeResult() {
  if (!saved.value && !confirm('还没有下载或复制这些卡密，关掉后就再也看不到原文了。确定关闭吗？')) return
  result.value = null
}

async function bulk(action: 'disable' | 'enable' | 'delete') {
  const ids = [...selected.value]
  if (!ids.length) return
  if (action === 'delete' && !confirm(`删除选中的 ${ids.length} 张卡密？已经兑换的会保留。`)) return
  try {
    const r = await props.call<{ changed?: number; deleted?: number }>('POST', `/cards/${action}`, { ids })
    toast(action === 'delete' ? `删除了 ${r.deleted} 张` : `${action === 'disable' ? '停用' : '恢复'}了 ${r.changed} 张`)
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  }
}

async function exportCsv() {
  try {
    const resp = await fetch(`/api/v1/console/cards/export?${query()}`, { headers: { 'X-Console-Token': props.token } })
    if (!resp.ok) throw new Error(`导出失败（HTTP ${resp.status}）`)
    const url = URL.createObjectURL(await resp.blob())
    const a = document.createElement('a')
    a.href = url
    a.download = `卡密列表-${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    toast((e as Error).message)
  }
}

function filterBatch(b: string) {
  filter.batch = b
}
</script>

<template>
  <section class="panel panel--inset">
    <div class="head">
      <h2>卡密</h2>
      <div class="head__btns">
        <button type="button" class="btn btn-sm" @click="exportCsv">导出列表</button>
        <button type="button" class="btn btn-sm btn-primary" :disabled="!plans.length" @click="openGen">批量生成</button>
      </div>
    </div>
    <p class="muted intro">
      每张卡密绑定一个套餐，用户在「个人中心 → 会员」里兑换。为了安全，卡密原文只在生成时显示一次（请马上下载保存），
      服务器只存加密后的指纹，列表和导出里只有打码后的样子。
    </p>
    <p v-if="!plans.length && !error" class="form-flash">先在「会员套餐」里新建一个套餐，才能生成卡密。</p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>

    <div class="filters">
      <select v-model="filter.plan" class="text-input" aria-label="套餐">
        <option value="">全部套餐</option>
        <option v-for="p in plans" :key="p.id" :value="String(p.id)">{{ p.name }}</option>
      </select>
      <select v-model="filter.status" class="text-input" aria-label="状态">
        <option value="">全部状态</option>
        <option value="unused">未使用</option>
        <option value="used">已使用</option>
        <option value="disabled">已停用</option>
      </select>
      <input v-model="filter.q" class="text-input" type="search" placeholder="搜备注或卡密末尾" aria-label="搜索" />
      <button v-if="filter.batch" type="button" class="btn btn-sm btn-ghost" @click="filter.batch = ''">批次 {{ filter.batch }} ✕</button>
    </div>

    <div v-if="selected.size" class="bulk">
      <span class="caption">选了 {{ selected.size }} 张</span>
      <button type="button" class="btn btn-sm" @click="bulk('disable')">停用</button>
      <button type="button" class="btn btn-sm" @click="bulk('enable')">恢复</button>
      <button type="button" class="btn btn-sm btn-danger-lite" @click="bulk('delete')">删除</button>
    </div>

    <div v-if="cards === null && !error" class="skeleton skel" aria-hidden="true" />
    <p v-else-if="cards && !cards.length" class="caption empty">没有卡密。</p>
    <div v-else-if="cards" class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th><input type="checkbox" :checked="allChecked" aria-label="全选" @change="toggleAll" /></th>
            <th>卡密</th>
            <th>套餐</th>
            <th>状态</th>
            <th>批次 / 备注</th>
            <th>兑换</th>
            <th>生成时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in cards" :key="c.id">
            <td><input type="checkbox" :checked="selected.has(c.id)" :aria-label="`选择 ${c.hint}`" @change="toggleOne(c.id)" /></td>
            <td class="mono nowrap">{{ c.hint }}</td>
            <td class="nowrap">{{ c.plan_name || '（已删除）' }}</td>
            <td class="nowrap"><span class="status" :class="`is-${c.status}`">{{ statusLabel[c.status] }}</span>
              <span v-if="c.status === 'unused' && c.expires_at" class="caption"><br />{{ dateTime(c.expires_at) }} 前</span></td>
            <td><button type="button" class="link mono" @click="filterBatch(c.batch)">{{ c.batch }}</button><span v-if="c.note" class="caption"><br />{{ c.note }}</span></td>
            <td><template v-if="c.used_at">{{ c.used_name || `#${c.used_by ?? '已删除'}` }}<span class="caption"><br />{{ dateTime(c.used_at) }}</span></template></td>
            <td class="caption nowrap">{{ dateTime(c.created_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="pages > 1" class="pager">
      <button type="button" class="btn btn-sm" :disabled="page === 0" @click="page--; load()">上一页</button>
      <span class="caption">{{ page + 1 }} / {{ pages }}（共 {{ total }} 张）</span>
      <button type="button" class="btn btn-sm" :disabled="page + 1 >= pages" @click="page++; load()">下一页</button>
    </div>

    <Modal :open="showGen" title="批量生成卡密" subtitle="生成后原文只显示一次，请马上下载或复制保存。" @close="showGen = false">
      <form @submit.prevent="generate">
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="g-plan">套餐</label>
            <select id="g-plan" v-model.number="gen.plan_id" class="text-input">
              <option v-for="p in plans" :key="p.id" :value="p.id">{{ p.name }}{{ p.visible ? '' : '（隐藏）' }}</option>
            </select>
          </div>
          <div class="field">
            <label class="field__label" for="g-count">数量（1-1000）</label>
            <input id="g-count" v-model.number="gen.count" class="text-input" type="number" min="1" max="1000" required />
          </div>
        </div>
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="g-prefix">前缀（可选，最多 8 个字母数字）</label>
            <input id="g-prefix" v-model="gen.prefix" class="text-input mono" maxlength="8" placeholder="BG" />
          </div>
          <div class="field">
            <label class="field__label" for="g-len">随机部分长度（12-32）</label>
            <input id="g-len" v-model.number="gen.length" class="text-input" type="number" min="12" max="32" />
          </div>
        </div>
        <div class="form-row">
          <div class="field">
            <label class="field__label" for="g-exp">兑换截止日期（可选）</label>
            <input id="g-exp" v-model="gen.expires" class="text-input" type="date" />
          </div>
          <div class="field">
            <label class="field__label" for="g-note">备注（可选，例如渠道）</label>
            <input id="g-note" v-model="gen.note" class="text-input" maxlength="100" placeholder="闲鱼 10 月" />
          </div>
        </div>
        <p v-if="genError" class="form-flash form-flash--error">{{ genError }}</p>
        <div class="modal-actions">
          <button type="button" class="btn" @click="showGen = false">取消</button>
          <button class="btn btn-primary" :disabled="generating">{{ generating ? '正在生成' : '生成' }}</button>
        </div>
      </form>
    </Modal>

    <Modal :open="!!result" :title="`生成了 ${result?.codes.length} 张「${result?.plan}」卡密`" wide @close="closeResult">
      <template v-if="result">
        <p class="form-flash form-flash--error warn">这是唯一一次能看到卡密原文的机会，服务器上不保存原文。请下载或复制后妥善保管。</p>
        <textarea class="textarea mono codes" readonly :value="result.codes.join('\n')" aria-label="卡密" />
        <p class="caption">批次 {{ result.batch }}，之后可以在列表里按批次筛选、停用或导出（打码的）。</p>
        <div class="modal-actions">
          <button type="button" class="btn" @click="copyAll">复制全部</button>
          <button type="button" class="btn" @click="downloadCsv">下载 CSV</button>
          <button type="button" class="btn btn-primary" @click="downloadTxt">下载 TXT</button>
          <button type="button" class="btn btn-ghost" @click="closeResult">关闭</button>
        </div>
      </template>
    </Modal>
  </section>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; gap: 8px; flex-wrap: wrap; }
.head h2 { margin: 0; }
.head__btns { display: flex; gap: 8px; }
.intro { margin: 8px 0 12px; font-size: 14px; line-height: 1.7; }
.filters { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 12px; }
.filters .text-input { width: auto; min-height: 34px; padding-top: 0; padding-bottom: 0; }
.filters input { flex: 1; min-width: 160px; }
.bulk { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; padding: 8px 12px; border-radius: var(--radius-md); background: var(--color-canvas-soft); }
.skel { height: 120px; border-radius: var(--radius-md); }
.empty { margin: 12px 0 0; }
.table-wrap { overflow-x: auto; }
.table { width: 100%; border-collapse: collapse; font-size: 13px; }
.table th { padding: 8px; border-bottom: 1px solid var(--color-divider); color: var(--color-muted); font-weight: 500; text-align: left; white-space: nowrap; }
.table td { padding: 9px 8px; border-bottom: 1px solid var(--color-divider); vertical-align: top; }
.nowrap { white-space: nowrap; }
.mono { font-family: var(--font-mono); font-size: 12px; }
.link { padding: 0; color: var(--color-accent-active); text-decoration: underline; text-underline-offset: 2px; }
.status { white-space: nowrap; }
.status.is-unused { color: var(--color-success); }
.status.is-used { color: var(--color-muted); }
.status.is-disabled { color: var(--color-danger); }
.pager { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 12px; }
.warn { margin-bottom: 10px; }
.codes { width: 100%; min-height: 260px; white-space: pre; }
</style>
