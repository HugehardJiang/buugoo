<script setup lang="ts">
// 定时任务页：是否允许布谷主动找自己聊天；提醒（到点提醒一句）与定时任务（到点去做一件事，再把结果发过来），
// 可以新建、修改、暂停、删除。
import { computed, onMounted, reactive, ref } from 'vue'
import { api, ApiError, type Reminder, type ReminderInput, type Repeat } from '../api'
import { session } from '../session'
import { toast } from '../toast'
import Modal from './Modal.vue'

const name = computed(() => session.meta?.name ?? '布谷')
const reminders = ref<Reminder[]>([])
const enabled = ref(true)
const care = ref(true)
const timezone = ref('')
const loaded = ref(false)
const saving = ref(false)
const proactive = computed(() => session.user?.proactive ?? true)

const repeatOptions: { value: Repeat; label: string }[] = [
  { value: '', label: '不重复' },
  { value: 'daily', label: '每天' },
  { value: 'weekdays', label: '工作日' },
  { value: 'weekly', label: '每周' },
  { value: 'monthly', label: '每月' },
  { value: 'interval', label: '每隔一段时间' },
  { value: 'cron', label: '自定义（cron）' },
]
const units = [
  { value: 1, label: '分钟' },
  { value: 60, label: '小时' },
  { value: 1440, label: '天' },
]

// 编辑框。
const editing = ref(false)
const editingId = ref<number | null>(null)
const submitting = ref(false)
const formError = ref('')
const form = reactive({ kind: 'remind' as 'remind' | 'task', content: '', time: '', repeat: '' as Repeat, every: 1, unit: 60, cron: '' })

function localInput(d: Date) {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

function openNew(kind: 'remind' | 'task') {
  const d = new Date(Date.now() + 60 * 60 * 1000)
  d.setMinutes(0, 0, 0)
  Object.assign(form, { kind, content: '', time: localInput(d), repeat: kind === 'task' ? 'daily' : '', every: 1, unit: 60, cron: '' })
  editingId.value = null
  formError.value = ''
  editing.value = true
}

function openEdit(r: Reminder) {
  const unit = r.every_minutes % 1440 === 0 && r.every_minutes ? 1440 : r.every_minutes % 60 === 0 && r.every_minutes ? 60 : 1
  Object.assign(form, {
    kind: r.kind, content: r.content, time: r.time.replace(' ', 'T'), repeat: r.repeat,
    every: r.every_minutes ? r.every_minutes / unit : 1, unit, cron: r.cron,
  })
  editingId.value = r.id
  formError.value = ''
  editing.value = true
}

// 下次时间按服务器时区显示（和编辑框一致）；今年的省略年份。
function nextText(r: Reminder) {
  const year = String(new Date().getFullYear())
  return r.time.startsWith(year + '-') ? r.time.slice(5) : r.time
}

const contentHint = computed(() =>
  form.kind === 'task'
    ? `写清楚要做什么，例如「搜一下今天的科技新闻，挑 3 条最重要的说说」。到时间${name.value}会去做（可以联网搜索、运行代码），再把结果发给你。`
    : '到时间要提醒你的事，例如「起来活动一下」「九点的组会」。',
)

async function submit() {
  if (submitting.value) return
  const input: ReminderInput = {
    kind: form.kind,
    content: form.content.trim(),
    repeat: form.repeat === '' ? 'none' : form.repeat,
  }
  if (!input.content) {
    formError.value = '内容不能为空'
    return
  }
  // cron 不填时间时，从下一次触发开始。
  if (form.repeat !== 'cron' || form.time) input.time = form.time.replace('T', ' ')
  if (form.repeat === 'interval') input.every_minutes = Math.round(form.every * form.unit)
  if (form.repeat === 'cron') input.cron = form.cron.trim()
  submitting.value = true
  formError.value = ''
  try {
    if (editingId.value) {
      const r = await api.updateReminder(editingId.value, input)
      reminders.value = reminders.value.map((x) => (x.id === r.id ? r : x))
      toast('已保存')
    } else {
      const r = await api.createReminder(input)
      reminders.value = [...reminders.value, r]
      toast(form.kind === 'task' ? '定时任务设好了' : '提醒设好了')
    }
    reminders.value.sort((a, b) => a.due_at.localeCompare(b.due_at))
    editing.value = false
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  try {
    const r = await api.reminders()
    reminders.value = r.reminders
    enabled.value = r.enabled
    care.value = r.care
    timezone.value = r.timezone
  } catch {
    // 读取失败时只是不显示列表。
  } finally {
    loaded.value = true
  }
})

async function toggle() {
  if (!session.user || saving.value) return
  const on = !proactive.value
  saving.value = true
  try {
    await api.setProactive(on)
    session.user.proactive = on
    toast(on ? `${name.value}偶尔会主动找你聊聊` : `${name.value}不会再主动找你了，提醒照常`)
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

async function pause(r: Reminder) {
  try {
    const n = await api.pauseReminder(r.id, !r.paused)
    reminders.value = reminders.value.map((x) => (x.id === n.id ? n : x))
    toast(n.paused ? '已暂停' : '已继续')
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  }
}

async function remove(r: Reminder) {
  try {
    await api.cancelReminder(r.id)
    reminders.value = reminders.value.filter((x) => x.id !== r.id)
    toast(r.kind === 'task' ? '定时任务已删除' : '提醒已删除')
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '删除失败')
  }
}
</script>

<template>
  <section id="proactive" class="panel panel--inset">
    <div v-if="care" class="row">
      <div class="row__text">
        <p class="row__title">允许{{ name }}主动找我</p>
        <p class="caption">很久没聊、或者你之前提到的事可能有结果了，{{ name }}会主动发消息问问。夜里不打扰，没回复时不会一直追着发。</p>
      </div>
      <button
        type="button"
        class="switch"
        role="switch"
        :aria-checked="proactive"
        :class="{ 'is-on': proactive }"
        :disabled="saving"
        @click="toggle"
      >
        <span class="switch__knob" />
      </button>
    </div>
    <div v-if="enabled" class="reminders">
      <p class="caption">
        在聊天里直接说也行：「半小时后叫我起来活动」是提醒；「每天早上 8 点搜一下科技新闻发我」是定时任务，到时间{{ name }}会真的去做，再把结果发给你。
        连接了微信、QQ 时会发到那里。
      </p>
      <div class="new">
        <button type="button" class="btn btn-sm" @click="openNew('remind')">新建提醒</button>
        <button type="button" class="btn btn-sm" @click="openNew('task')">新建定时任务</button>
      </div>
      <TransitionGroup v-if="reminders.length" tag="ul" name="list" class="reminder-list">
        <li v-for="r in reminders" :key="r.id" class="reminder" :class="{ 'is-paused': r.paused }">
          <div class="reminder__main">
            <p class="reminder__text">
              <span class="badge-pill" :class="{ 'badge-pill--task': r.kind === 'task' }">{{ r.kind === 'task' ? '定时任务' : '提醒' }}</span>
              {{ r.content }}
            </p>
            <p class="caption reminder__when">
              {{ r.paused ? '已暂停' : `下次 ${nextText(r)}` }}<template v-if="r.repeat"> · {{ r.repeat_label }}</template>
            </p>
          </div>
          <div class="reminder__actions">
            <button type="button" class="btn btn-ghost btn-sm" @click="openEdit(r)">修改</button>
            <button v-if="r.repeat" type="button" class="btn btn-ghost btn-sm" @click="pause(r)">{{ r.paused ? '继续' : '暂停' }}</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="remove(r)">删除</button>
          </div>
        </li>
      </TransitionGroup>
      <p v-else-if="loaded" class="caption empty">现在没有提醒和定时任务</p>
    </div>

    <Modal :open="editing" :title="editingId ? '修改' : form.kind === 'task' ? '新建定时任务' : '新建提醒'" @close="editing = false">
      <form @submit.prevent="submit">
        <div v-if="!editingId" class="field">
          <div class="type-switch" role="group" aria-label="类型">
            <button type="button" :class="{ 'is-active': form.kind === 'remind' }" @click="form.kind = 'remind'">提醒</button>
            <button type="button" :class="{ 'is-active': form.kind === 'task' }" @click="form.kind = 'task'">定时任务</button>
          </div>
        </div>
        <label class="field">
          <span class="field__label">{{ form.kind === 'task' ? '要做什么' : '提醒什么' }}</span>
          <textarea v-model="form.content" class="textarea" rows="3" :maxlength="form.kind === 'task' ? 500 : 200" />
          <span class="field__hint">{{ contentHint }}</span>
        </label>
        <label class="field">
          <span class="field__label">重复</span>
          <select v-model="form.repeat" class="text-input">
            <option v-for="o in repeatOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </label>
        <div v-if="form.repeat === 'interval'" class="field">
          <span class="field__label">每隔</span>
          <div class="inline">
            <input v-model.number="form.every" class="text-input" type="number" min="1" step="1" />
            <select v-model.number="form.unit" class="text-input">
              <option v-for="u in units" :key="u.value" :value="u.value">{{ u.label }}</option>
            </select>
          </div>
          <span class="field__hint">至少 30 分钟</span>
        </div>
        <label v-if="form.repeat === 'cron'" class="field">
          <span class="field__label">cron 表达式</span>
          <input v-model="form.cron" class="text-input mono" placeholder="0 9 * * 1,3,5" spellcheck="false" autocomplete="off" />
          <span class="field__hint">5 段：分 时 日 月 周。例如「0 9 * * 1,3,5」是每周一三五早上 9 点，「30 8 1 * *」是每月 1 号 8:30。两次之间至少隔 30 分钟。</span>
        </label>
        <label class="field">
          <span class="field__label">{{ form.repeat ? (form.repeat === 'cron' ? '从什么时候开始（可以不填）' : '第一次的时间') : '时间' }}</span>
          <input v-model="form.time" class="text-input" type="datetime-local" />
          <span v-if="timezone" class="field__hint">按服务器时区（{{ timezone }}）计算</span>
        </label>
        <p v-if="formError" class="form-flash form-flash--error">{{ formError }}</p>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="editing = false">取消</button>
          <button type="submit" class="btn btn-primary" :disabled="submitting">{{ submitting ? '保存中' : '保存' }}</button>
        </div>
      </form>
    </Modal>
  </section>
</template>

<style scoped>
.row { display: flex; align-items: flex-start; gap: 16px; padding-bottom: 16px; }
.row__text { flex: 1; min-width: 0; }
.row__title { margin: 0 0 4px; color: var(--color-ink); font-size: 14px; font-weight: 500; }
.caption { margin: 0; }
.switch {
  position: relative; flex-shrink: 0; width: 44px; height: 26px; margin-top: 2px; border-radius: 999px; background: var(--color-hairline-strong);
  transition: background-color 220ms var(--ease-editorial);
}
.switch.is-on { background: var(--color-accent); }
.switch__knob {
  position: absolute; top: 3px; left: 3px; width: 20px; height: 20px; border-radius: 50%; background: #fff;
  box-shadow: 0 1px 3px rgba(20, 20, 19, .25); transition: transform 260ms var(--ease-editorial);
}
.switch.is-on .switch__knob { transform: translateX(18px); }
.reminders { padding-top: 16px; border-top: 1px solid var(--color-divider); }
.new { display: flex; flex-wrap: wrap; gap: 8px; margin: 12px 0 4px; }
.reminder-list { display: grid; margin: 12px 0 0; padding: 0; list-style: none; }
.reminder { display: flex; align-items: center; gap: 8px; padding: 10px 0; border-top: 1px solid var(--color-divider); font-size: 14px; transition: opacity .2s ease; }
.reminder.is-paused { opacity: .6; }
.reminder__main { flex: 1; min-width: 0; }
.reminder__text { margin: 0 0 3px; color: var(--color-ink); overflow-wrap: anywhere; }
.reminder__text .badge-pill { margin-right: 4px; vertical-align: 1px; }
.badge-pill--task { background: var(--color-accent-tint, rgba(91, 122, 74, .12)); color: var(--color-accent-active); }
.reminder__when { font-variant-numeric: tabular-nums; }
.reminder__actions { display: flex; flex-shrink: 0; gap: 2px; }
.btn-sm { min-height: 28px; padding: 0 10px; }
.empty { margin-top: 10px; }
.inline { display: flex; gap: 8px; }
.inline .text-input:first-child { width: 110px; }
.inline select { flex: 1; }
.mono { font-family: var(--font-mono); }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
@media (max-width: 560px) {
  .reminder { flex-wrap: wrap; }
  .reminder__actions { width: 100%; justify-content: flex-end; }
}
</style>
