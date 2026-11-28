<script setup lang="ts">
// 聊天里的后台任务卡片：布谷提出任务后在这里确认（可以调预算），之后显示进度，做完给出结果的入口。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api, ApiError, type BgTask } from '../api'
import { toast } from '../toast'
import Icon from '../Icon.vue'

const props = defineProps<{ taskId: number }>()
// finished：看着它从进行中变成结束（结果消息刚发到对话里，聊天页要刷新一下）。
const emit = defineEmits<{ finished: [] }>()
const task = ref<BgTask | null>(null)
const loadError = ref('')
const busy = ref(false)
const budget = ref(0)
const limits = ref({ def: 1_000_000, max: 5_000_000 })
let timer: ReturnType<typeof setInterval> | undefined

const budgetOptions = computed(() => {
  const out = [200_000, 500_000, 1_000_000, 2_000_000, 5_000_000, limits.value.def].filter((v) => v <= limits.value.max)
  return [...new Set(out)].sort((a, b) => a - b)
})
const live = computed(() => task.value && ['queued', 'running'].includes(task.value.status))
const pct = computed(() => (task.value?.budget_tokens ? Math.min(100, Math.round((task.value.used_tokens / task.value.budget_tokens) * 100)) : 0))

function wan(n: number) {
  return n >= 10000 ? `${Math.round(n / 1000) / 10} 万` : String(n)
}

async function load() {
  const wasLive = live.value
  try {
    task.value = await api.bgTask(props.taskId)
    if (wasLive && !live.value) emit('finished')
    if (!budget.value) budget.value = task.value.budget_tokens
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '读取任务失败'
  }
  if (!live.value) clearInterval(timer)
}

function poll() {
  clearInterval(timer)
  timer = setInterval(() => document.visibilityState === 'visible' && load(), 3000)
}

onMounted(async () => {
  await load()
  if (task.value?.status === 'pending') {
    try {
      const r = await api.bgTasks()
      limits.value = { def: r.default_budget, max: r.max_budget }
    } catch {
      /* 用默认值 */
    }
  }
  if (live.value) poll()
})
onBeforeUnmount(() => clearInterval(timer))

async function act(fn: () => Promise<BgTask>, msg: string) {
  if (busy.value) return
  busy.value = true
  try {
    const wasLive = live.value
    task.value = await fn()
    if (wasLive && !live.value) emit('finished')
    if (msg) toast(msg)
    if (live.value) poll()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  } finally {
    busy.value = false
  }
}

const id = () => props.taskId
const cancel = (msg: string) => act(() => api.cancelBgTask(id()), msg)
const confirm = () => act(() => api.confirmBgTask(id(), budget.value), '已经开始了，做完会发到这里')
const resume = () => act(() => api.resumeBgTask(id()), '接着做了')

const statusText: Record<string, string> = {
  pending: '等你确认', queued: '排队中', running: '进行中', done: '已完成', failed: '没做完', canceled: '已取消', interrupted: '被打断了',
}
</script>

<template>
  <div class="tcard" :class="task ? `is-${task.status}` : ''">
    <p v-if="loadError" class="caption">{{ loadError }}</p>
    <template v-else-if="task">
      <div class="tcard__head">
        <span class="tcard__icon"><Icon name="grid" :size="16" /></span>
        <span class="tcard__title">后台任务 · {{ task.title }}</span>
        <span class="tcard__status">{{ statusText[task.status] }}</span>
      </div>

      <template v-if="task.status === 'pending'">
        <p class="tcard__goal">{{ task.goal }}</p>
        <div class="tcard__row">
          <label class="caption" :for="`budget-${task.id}`">最多用</label>
          <select :id="`budget-${task.id}`" v-model.number="budget" class="text-input tcard__select">
            <option v-for="b in budgetOptions" :key="b" :value="b">{{ wan(b) }} token</option>
          </select>
          <span class="caption">用完就停下，先交出已经做完的部分；按实际用量计费</span>
        </div>
        <div class="tcard__actions">
          <button class="btn btn-sm btn-ghost" :disabled="busy" @click="cancel('')">不用了</button>
          <button class="btn btn-sm btn-primary" :disabled="busy" @click="confirm">开始</button>
        </div>
      </template>

      <template v-else-if="task.status === 'queued' || task.status === 'running'">
        <div class="tcard__bar" :title="`已用 ${wan(task.used_tokens)} / ${wan(task.budget_tokens)} token`"><span :style="{ width: `${pct}%` }" /></div>
        <p class="caption">
          <template v-if="task.status === 'queued'">前面还有 {{ task.position ?? 0 }} 个任务，轮到了自动开始</template>
          <template v-else>已经做了 {{ task.rounds }} 轮 · 用了 {{ wan(task.used_tokens) }} / {{ wan(task.budget_tokens) }} token · 做完会发到这里</template>
        </p>
        <div class="tcard__actions">
          <RouterLink :to="`/bgtasks?id=${task.id}`" class="btn btn-sm btn-ghost">看进度</RouterLink>
          <button class="btn btn-sm" :disabled="busy" @click="cancel('已停下')">停止</button>
        </div>
      </template>

      <template v-else>
        <p v-if="task.error" class="caption">{{ task.error }}</p>
        <p v-else-if="task.status === 'done'" class="caption">结果在下面的消息里 · 用了 {{ wan(task.used_tokens) }} token</p>
        <div class="tcard__actions">
          <RouterLink :to="`/bgtasks?id=${task.id}`" class="btn btn-sm btn-ghost">查看过程</RouterLink>
          <button v-if="task.status === 'interrupted'" class="btn btn-sm btn-primary" :disabled="busy" @click="resume">接着做</button>
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.tcard {
  display: grid; gap: 11px; max-width: min(600px, 100%); margin: 8px 0 6px; padding: 16px 18px;
  border: 0; border-radius: 20px; background: var(--md-surface-container-low);
  transition: box-shadow .2s var(--md-ease);
}
.tcard.is-pending { box-shadow: 0 0 0 2px rgba(91, 122, 74, .34); }
.tcard__head { display: flex; align-items: center; gap: 10px; min-width: 0; }
.tcard__icon {
  display: grid; place-items: center; width: 32px; height: 32px; border-radius: 10px;
  background: var(--md-primary-container); color: var(--md-on-primary-container); flex-shrink: 0;
}
.tcard__title {
  flex: 1; min-width: 0; color: var(--color-ink); font-size: 14.5px; font-weight: 500;
  letter-spacing: -.01em; overflow-wrap: anywhere;
}
.tcard__status {
  flex-shrink: 0; display: inline-flex; align-items: center; height: 24px; padding: 0 11px;
  border-radius: 9999px; background: var(--md-surface-container); color: var(--color-text-secondary);
  font-size: 11.5px; font-weight: 500;
}
.is-pending .tcard__status,
.is-running .tcard__status,
.is-queued .tcard__status { background: var(--md-primary-container); color: var(--md-on-primary-container); }
.is-done .tcard__status { background: var(--md-primary-container); color: var(--md-on-primary-container); }
.is-failed .tcard__status { background: var(--color-danger-tint); color: var(--color-danger); }
.tcard__goal {
  margin: 0; color: var(--color-text-secondary); font-size: 13.5px; line-height: 1.65;
  white-space: pre-wrap; max-height: 160px; overflow-y: auto;
}
.tcard__row { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.tcard__select {
  width: auto; min-height: 34px; padding: 4px 14px; border: 1px solid var(--md-outline-variant);
  border-radius: 9999px; background: var(--md-surface-container-lowest); font-size: 13px;
}
.tcard__actions { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 8px; margin-top: 2px; }
.tcard__actions .btn { border-radius: 9999px; padding: 0 14px; }
.tcard__bar { height: 5px; border-radius: 9999px; background: rgba(20, 20, 19, .08); overflow: hidden; }
.tcard__bar span { display: block; height: 100%; border-radius: 9999px; background: var(--color-accent); transition: width .6s var(--ease-editorial); }
.caption { margin: 0; line-height: 1.6; }
@media (prefers-reduced-motion: reduce) {
  .tcard, .tcard__bar span { transition: none; }
}
</style>
