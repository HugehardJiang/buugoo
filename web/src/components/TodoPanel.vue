<script setup lang="ts">
// 待办清单：一件件要持续去做的事，做完打勾；可以有截止时间和备注。在聊天里说「帮我记一下要写实验报告」也会加到这里，
// 有截止时间的快到期还没做完时，布谷会主动问一句。
import { computed, onMounted, reactive, ref } from 'vue'
import { api, ApiError, type Todo } from '../api'
import { session } from '../session'
import { toast } from '../toast'
import Modal from './Modal.vue'

const name = computed(() => session.meta?.name ?? '布谷')
const items = ref<Todo[]>([])
const loaded = ref(false)
const loadError = ref('')
const showDone = ref(false)
const busy = ref<Record<number, boolean>>({})

const open = computed(() => items.value.filter((t) => !t.done))
const done = computed(() => items.value.filter((t) => t.done))

// 快速添加。
const quick = reactive({ title: '', date: '', time: '' })
const adding = ref(false)

// 编辑框。
const editing = ref(false)
const editingItem = ref<Todo | null>(null)
const form = reactive({ title: '', date: '', time: '', note: '' })
const formError = ref('')
const saving = ref(false)

onMounted(load)

async function load() {
  try {
    items.value = (await api.todos()).items
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  } finally {
    loaded.value = true
  }
}

function due(date: string, time: string) {
  return date ? (time ? `${date} ${time}` : date) : ''
}

async function add() {
  if (!quick.title.trim() || adding.value) return
  adding.value = true
  try {
    await api.createTodo({ title: quick.title, due: due(quick.date, quick.time) })
    Object.assign(quick, { title: '', date: '', time: '' })
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '添加失败')
  } finally {
    adding.value = false
  }
}

async function toggle(t: Todo) {
  if (busy.value[t.id]) return
  busy.value[t.id] = true
  const was = t.done
  t.done = !was // 先在界面上打勾，失败再改回来
  try {
    await api.updateTodo(t.id, { done: !was })
    await load()
  } catch (e) {
    t.done = was
    toast(e instanceof ApiError ? e.message : '操作失败')
  } finally {
    busy.value[t.id] = false
  }
}

function openEdit(t: Todo) {
  const [date = '', time = ''] = t.due.split(' ')
  Object.assign(form, { title: t.title, date, time, note: t.note })
  editingItem.value = t
  formError.value = ''
  editing.value = true
}

async function save() {
  const t = editingItem.value
  if (!t || saving.value) return
  saving.value = true
  formError.value = ''
  try {
    await api.updateTodo(t.id, { title: form.title, note: form.note, due: due(form.date, form.time) })
    editing.value = false
    await load()
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function remove() {
  const t = editingItem.value
  if (!t || !confirm(`删除「${t.title}」？`)) return
  try {
    await api.deleteTodo(t.id)
    editing.value = false
    await load()
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : '删除失败'
  }
}

async function clearDone() {
  if (!confirm(`删掉全部 ${done.value.length} 项做完的？`)) return
  try {
    await api.clearDoneTodos()
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  }
}

// 截止时间：今年的省略年份；今天、明天直接说。
function dueText(t: Todo) {
  if (!t.due) return ''
  const [date, time] = t.due.split(' ')
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  const key = (x: Date) => `${x.getFullYear()}-${p(x.getMonth() + 1)}-${p(x.getDate())}`
  const tomorrow = new Date(d.getTime() + 86400000)
  let day = date.startsWith(`${d.getFullYear()}-`) ? date.slice(5) : date
  if (date === key(d)) day = '今天'
  else if (date === key(tomorrow)) day = '明天'
  return time ? `${day} ${time}` : day
}

function overdue(t: Todo) {
  if (!t.due_at || t.done) return false
  const end = new Date(t.due_at).getTime() + (t.all_day ? 86400000 : 0)
  return end < Date.now()
}

// 还剩不到一天。
function soon(t: Todo) {
  if (!t.due_at || t.done || overdue(t)) return false
  const end = new Date(t.due_at).getTime() + (t.all_day ? 86400000 : 0)
  return end - Date.now() < 86400000
}
</script>

<template>
  <section id="todos" class="panel panel--inset">
    <div class="head">
      <h2>待办</h2>
      <span v-if="open.length" class="caption">{{ open.length }} 项没做完</span>
    </div>
    <p class="caption intro">
      要持续去做的事记在这里，做完打个勾。在聊天里说「帮我记一下要写大物实验报告」「我还有哪些没做」也行；有截止时间的事快到了还没做完，{{ name }}会来问问你。
    </p>

    <form class="quick" @submit.prevent="add">
      <input v-model="quick.title" class="text-input quick__title" placeholder="加一件要做的事" maxlength="100" aria-label="要做的事" />
      <input v-model="quick.date" class="text-input quick__date" type="date" aria-label="截止日期（可选）" />
      <input v-if="quick.date" v-model="quick.time" class="text-input quick__time" type="time" aria-label="截止时间（可选）" />
      <button class="btn btn-primary btn-sm" :disabled="!quick.title.trim() || adding">添加</button>
    </form>

    <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
    <TransitionGroup v-if="open.length" tag="ul" name="list" class="todo-list">
      <li v-for="t in open" :key="t.id" class="todo">
        <button type="button" class="check" role="checkbox" :aria-checked="t.done" :aria-label="`完成「${t.title}」`" @click="toggle(t)">
          <svg v-if="t.done" viewBox="0 0 16 16" aria-hidden="true"><path d="M3.5 8.5l3 3 6-7" /></svg>
        </button>
        <button type="button" class="todo__main" @click="openEdit(t)">
          <span class="todo__title">{{ t.title }}</span>
          <span v-if="t.due || t.note" class="caption todo__meta">
            <span v-if="t.due" class="due" :class="{ 'is-overdue': overdue(t), 'is-soon': soon(t) }">{{ overdue(t) ? '已过期 · ' : '' }}{{ dueText(t) }}</span>
            <span v-if="t.note" class="note">{{ t.note }}</span>
          </span>
        </button>
      </li>
    </TransitionGroup>
    <p v-else-if="loaded && !loadError" class="caption empty">{{ done.length ? '都做完了，真棒' : '还没有待办' }}</p>

    <div v-if="done.length" class="done">
      <div class="done__head">
        <button type="button" class="link caption" @click="showDone = !showDone">{{ showDone ? '收起' : '查看' }}做完的（{{ done.length }}）</button>
        <button v-if="showDone" type="button" class="link caption" @click="clearDone">全部删掉</button>
      </div>
      <TransitionGroup v-if="showDone" tag="ul" name="list" class="todo-list">
        <li v-for="t in done" :key="t.id" class="todo is-done">
          <button type="button" class="check is-on" role="checkbox" :aria-checked="true" :aria-label="`取消完成「${t.title}」`" @click="toggle(t)">
            <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3.5 8.5l3 3 6-7" /></svg>
          </button>
          <button type="button" class="todo__main" @click="openEdit(t)">
            <span class="todo__title">{{ t.title }}</span>
          </button>
        </li>
      </TransitionGroup>
    </div>

    <Modal :open="editing" title="修改待办" @close="editing = false">
      <form @submit.prevent="save">
        <label class="field">
          <span class="field__label">要做的事</span>
          <input v-model="form.title" class="text-input" maxlength="100" />
        </label>
        <div class="field">
          <span class="field__label">截止时间（可以不填，也可以只填日期）</span>
          <div class="inline">
            <input v-model="form.date" class="text-input" type="date" aria-label="截止日期" />
            <input v-model="form.time" class="text-input" type="time" :disabled="!form.date" aria-label="截止时间" />
            <button v-if="form.date" type="button" class="btn btn-ghost btn-sm" @click="form.date = ''; form.time = ''">清除</button>
          </div>
        </div>
        <label class="field">
          <span class="field__label">备注</span>
          <textarea v-model="form.note" class="textarea" rows="4" maxlength="2000" placeholder="要求、地点、链接……" />
        </label>
        <p v-if="formError" class="form-flash form-flash--error">{{ formError }}</p>
        <div class="modal-actions">
          <button type="button" class="btn btn-danger-lite delete" @click="remove">删除</button>
          <button type="button" class="btn btn-ghost" @click="editing = false">取消</button>
          <button type="submit" class="btn btn-primary" :disabled="saving || !form.title.trim()">{{ saving ? '保存中' : '保存' }}</button>
        </div>
      </form>
    </Modal>
  </section>
</template>

<style scoped>
.head { display: flex; align-items: baseline; gap: 10px; }
h2 { margin: 0 0 6px; }
.intro { margin: 0 0 14px; }
.quick { display: flex; gap: 8px; flex-wrap: wrap; }
.quick__title { flex: 1 1 220px; min-width: 0; }
.quick__date { flex: 0 0 150px; }
.quick__time { flex: 0 0 110px; }
.quick .btn { min-height: 40px; padding: 0 16px; }
.todo-list { display: grid; margin: 12px 0 0; padding: 0; list-style: none; }
.todo { display: flex; align-items: flex-start; gap: 10px; padding: 9px 0; border-top: 1px solid var(--color-divider); transition: opacity .2s ease; }
.check {
  flex-shrink: 0; display: grid; place-items: center; width: 20px; height: 20px; margin-top: 1px; border: 1.5px solid var(--color-hairline-strong);
  border-radius: 6px; background: var(--color-canvas); transition: background-color .18s ease, border-color .18s ease;
}
.check:hover { border-color: var(--color-accent); }
.check.is-on, .check[aria-checked='true'] { border-color: var(--color-accent); background: var(--color-accent); }
.check svg { width: 14px; height: 14px; fill: none; stroke: #fff; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
.todo__main { flex: 1; min-width: 0; display: grid; gap: 2px; padding: 0; border: 0; background: none; text-align: left; cursor: pointer; }
.todo__title { color: var(--color-ink); font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; }
.todo__meta { display: flex; gap: 8px; min-width: 0; }
.due { flex-shrink: 0; font-variant-numeric: tabular-nums; }
.due.is-soon { color: var(--color-accent-active); }
.due.is-overdue { color: var(--color-danger); }
.note { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.is-done .todo__title { color: var(--color-text-secondary); text-decoration: line-through; }
.empty { margin: 12px 0 0; }
.done { margin-top: 12px; }
.done__head { display: flex; justify-content: space-between; }
.link { padding: 0; text-decoration: underline; text-underline-offset: 3px; }
.btn-sm { min-height: 28px; padding: 0 10px; }
.inline { display: flex; gap: 8px; align-items: center; }
.inline .text-input { flex: 1; min-width: 0; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
.delete { margin-right: auto; }
@media (max-width: 560px) {
  .quick__date { flex: 1 1 140px; }
}
</style>
