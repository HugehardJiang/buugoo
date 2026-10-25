<script setup lang="ts">
// 设置 → 提醒与主动消息：是否允许布谷主动找自己聊天，以及还没到时间的提醒（可以取消）。
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type Reminder } from '../api'
import { dateTime } from '../format'
import { session } from '../session'
import { toast } from '../toast'

const name = computed(() => session.meta?.name ?? '布谷')
const reminders = ref<Reminder[]>([])
const enabled = ref(true)
const care = ref(true)
const loaded = ref(false)
const saving = ref(false)
const proactive = computed(() => session.user?.proactive ?? true)
const repeatLabel: Record<string, string> = { '': '', daily: '每天', weekdays: '工作日', weekly: '每周' }

onMounted(async () => {
  try {
    const r = await api.reminders()
    reminders.value = r.reminders
    enabled.value = r.enabled
    care.value = r.care
  } catch {
    // 读取失败时只是不显示提醒列表。
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

async function cancel(r: Reminder) {
  try {
    await api.cancelReminder(r.id)
    reminders.value = reminders.value.filter((x) => x.id !== r.id)
    toast('提醒已取消')
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '取消失败')
  }
}
</script>

<template>
  <section id="proactive" class="panel panel--inset">
    <h2>提醒与主动消息</h2>
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
      <p class="row__title">提醒</p>
      <p class="caption">
        在聊天里直接说就行，比如「半小时后叫我起来活动」「每天晚上十点提醒我吃药」。到时间{{ name }}会主动发消息，绑定了微信或 QQ 时会发到那里。
      </p>
      <TransitionGroup v-if="reminders.length" tag="ul" name="list" class="reminder-list">
        <li v-for="r in reminders" :key="r.id" class="reminder">
          <span class="reminder__when">{{ dateTime(r.due_at) }}</span>
          <span v-if="r.repeat" class="badge-pill">{{ repeatLabel[r.repeat] }}</span>
          <span class="reminder__text">{{ r.content }}</span>
          <button type="button" class="btn btn-ghost btn-sm" @click="cancel(r)">取消</button>
        </li>
      </TransitionGroup>
      <p v-else-if="loaded" class="caption empty">现在没有提醒</p>
    </div>
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
.row + .reminders { margin-top: 0; }
.reminder-list { display: grid; gap: 2px; margin: 12px 0 0; padding: 0; list-style: none; }
.reminder { display: flex; align-items: center; gap: 8px; min-height: 40px; padding: 4px 0; border-top: 1px solid var(--color-divider); font-size: 14px; }
.reminder__when { flex-shrink: 0; color: var(--color-text-secondary); font-variant-numeric: tabular-nums; }
.reminder__text { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.btn-sm { min-height: 28px; padding: 0 10px; }
.empty { margin-top: 10px; }
</style>
