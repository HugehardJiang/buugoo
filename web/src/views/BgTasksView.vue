<script setup lang="ts">
// 后台任务：布谷在后台慢慢做的大任务。左边是列表，右边是选中任务的每一步、用量和结果；进行中的每几秒刷新一次。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api, ApiError, type BgTask } from '../api'
import ToolSteps from '../components/ToolSteps.vue'
import TopBlur from '../components/TopBlur.vue'
import { shortWhen } from '../format'
import { renderMarkdown } from '../markdown'
import { session } from '../session'
import { toast } from '../toast'

const name = computed(() => session.meta?.name ?? '布谷')
const tasks = ref<BgTask[] | null>(null)
const allowed = ref(true)
const loadError = ref('')
const current = ref<BgTask | null>(null)
const busy = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

const statusText: Record<string, string> = {
  pending: '等你确认', queued: '排队中', running: '进行中', done: '已完成', failed: '没做完', canceled: '已取消', interrupted: '被打断了',
}
const isLive = (t: BgTask) => t.status === 'queued' || t.status === 'running'
const anyLive = computed(() => (tasks.value ?? []).some(isLive))

function wan(n: number) {
  return n >= 10000 ? `${Math.round(n / 1000) / 10} 万` : String(n)
}
function pct(t: BgTask) {
  return t.budget_tokens ? Math.min(100, Math.round((t.used_tokens / t.budget_tokens) * 100)) : 0
}

async function load() {
  try {
    const r = await api.bgTasks()
    tasks.value = r.tasks
    allowed.value = r.allowed
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function open(id: number) {
  try {
    current.value = await api.bgTask(id)
    history.replaceState(history.state, '', `?id=${id}`)
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '读取任务失败')
  }
}

function back() {
  current.value = null
  history.replaceState(history.state, '', location.pathname)
}

async function tick() {
  if (document.visibilityState !== 'visible') return
  if (anyLive.value) await load()
  if (current.value && isLive(current.value)) await open(current.value.id)
}

onMounted(async () => {
  await load()
  const id = Number(new URLSearchParams(location.search).get('id'))
  if (id) await open(id)
  timer = setInterval(tick, 3000)
})
onBeforeUnmount(() => clearInterval(timer))

async function act(fn: () => Promise<BgTask>, msg: string) {
  if (busy.value || !current.value) return
  busy.value = true
  try {
    await fn()
    toast(msg)
    await Promise.all([load(), open(current.value.id)])
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  } finally {
    busy.value = false
  }
}
const cur = () => current.value!
const cancel = () => act(() => api.cancelBgTask(cur().id), '已停下')
const confirm = () => act(() => api.confirmBgTask(cur().id, cur().budget_tokens), '已经开始了')
const resume = () => act(() => api.resumeBgTask(cur().id), '接着做了')
</script>

<template>
  <main class="main">
    <header class="topbar">
      <TopBlur />
      <div class="topbar__titles">
        <h1>后台任务</h1>
      </div>
    </header>
    <div class="page">
      <div class="bg" :class="{ 'has-page': !!current }">
        <aside class="bg__list">
          <p class="caption intro">
            查资料写报告、处理一大堆文件这类要做很久的事，和{{ name }}说「放到后台慢慢做」。确认后它会自己一步步做，做完把结果发回原来的对话。
          </p>
          <p v-if="!allowed" class="form-flash">当前套餐不含后台任务，可以在「设置 → 会员」里看看别的套餐。</p>
          <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
          <div v-else-if="tasks === null" class="skeleton skel" aria-hidden="true" />
          <p v-else-if="!tasks.length" class="caption empty">还没有后台任务。</p>
          <ul v-else class="items">
            <li v-for="t in tasks" :key="t.id">
              <button type="button" class="item" :class="{ 'is-active': current?.id === t.id }" @click="open(t.id)">
                <span class="item__title">{{ t.title }}</span>
                <span class="item__meta">
                  <span class="badge" :class="`is-${t.status}`">{{ statusText[t.status] }}</span>
                  {{ shortWhen(t.created_at) }} · {{ wan(t.used_tokens) }} / {{ wan(t.budget_tokens) }} token
                </span>
                <span v-if="isLive(t)" class="bar"><span :style="{ width: `${pct(t)}%` }" /></span>
              </button>
            </li>
          </ul>
        </aside>

        <section v-if="current" class="bg__body panel panel--inset">
          <div class="body-head">
            <button type="button" class="btn btn-sm btn-ghost back" @click="back">‹ 全部</button>
            <h2 class="title">{{ current.title }}</h2>
            <span class="badge" :class="`is-${current.status}`">{{ statusText[current.status] }}</span>
          </div>
          <dl class="facts">
            <div><dt>用量</dt><dd>{{ wan(current.used_tokens) }} / {{ wan(current.budget_tokens) }} token</dd></div>
            <div><dt>轮数</dt><dd>{{ current.rounds }}</dd></div>
            <div><dt>沙箱联网</dt><dd>{{ current.network ? '可以' : '不能' }}</dd></div>
            <div><dt>提出时间</dt><dd>{{ shortWhen(current.created_at) }}</dd></div>
            <div v-if="current.finished_at"><dt>结束时间</dt><dd>{{ shortWhen(current.finished_at) }}</dd></div>
          </dl>
          <div v-if="isLive(current)" class="bar bar--big"><span :style="{ width: `${pct(current)}%` }" /></div>
          <p v-if="current.status === 'queued'" class="caption">前面还有 {{ current.position ?? 0 }} 个任务，轮到了自动开始。</p>
          <p v-if="current.error" class="form-flash" :class="{ 'form-flash--error': current.status === 'failed' }">{{ current.error }}</p>

          <h3 class="sub">要做什么</h3>
          <p class="goal">{{ current.goal }}</p>

          <div class="actions">
            <RouterLink class="btn btn-sm btn-ghost" :to="`/?c=${current.conversation_id}`">回到对话</RouterLink>
            <button v-if="current.status === 'pending' || isLive(current)" class="btn btn-sm" :disabled="busy" @click="cancel">
              {{ current.status === 'pending' ? '不用了' : '停止' }}
            </button>
            <button v-if="current.status === 'pending'" class="btn btn-sm btn-primary" :disabled="busy" @click="confirm">开始</button>
            <button v-if="current.status === 'interrupted'" class="btn btn-sm btn-primary" :disabled="busy" @click="resume">接着做</button>
          </div>

          <template v-if="current.steps?.length">
            <h3 class="sub">每一步</h3>
            <ToolSteps :steps="current.steps" />
          </template>
          <p v-else-if="isLive(current)" class="caption">还没开始用工具……</p>

          <template v-if="current.result">
            <h3 class="sub">结果</h3>
            <div class="md" v-html="renderMarkdown(current.result)" />
          </template>
        </section>
        <section v-else class="bg__placeholder">
          <p class="caption">选一个任务查看每一步在做什么。</p>
        </section>
      </div>
    </div>
  </main>
</template>

<style scoped>
.bg { display: grid; grid-template-columns: 320px minmax(0, 1fr); gap: 28px; max-width: 1240px; margin: 0 auto; align-items: start; }
.bg__list { position: sticky; top: 72px; display: grid; gap: 12px; min-width: 0; }
.intro, .empty { margin: 0; line-height: 1.65; }
.skel { height: 120px; border-radius: var(--radius-xl); }
.items { display: grid; gap: 6px; margin: 0; padding: 0; list-style: none; }
.item {
  display: grid; gap: 6px; width: 100%; padding: 12px 16px; border: 0; border-radius: 18px; text-align: left;
  transition: background-color var(--motion-fast) var(--md-ease), box-shadow var(--motion-fast) var(--md-ease);
}
.item:hover { background: var(--md-surface-container-low); }
.item.is-active {
  background: var(--md-primary-container); color: var(--md-on-primary-container); box-shadow: none;
}
.item__title { color: var(--color-ink); font-size: 14px; font-weight: 500; overflow-wrap: anywhere; }
.item__meta { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; color: var(--color-muted); font-size: 12px; font-variant-numeric: tabular-nums; }
.item.is-active .item__title { color: var(--md-on-primary-container); font-weight: 600; }
.item.is-active .item__meta { color: var(--md-on-primary-container); opacity: .86; }
.badge {
  display: inline-flex; align-items: center; padding: 2px 10px; border: 0; border-radius: 9999px;
  background: var(--md-surface-container); color: var(--color-text-secondary); font-size: 12px; font-weight: 500; white-space: nowrap;
}
.badge.is-running, .badge.is-queued, .badge.is-pending { background: var(--md-primary-container); color: var(--md-on-primary-container); }
.item.is-active .badge { background: var(--md-surface-container-lowest); color: var(--md-on-primary-container); }
.badge.is-failed { background: var(--color-danger-tint); color: var(--color-danger); }
.bar { display: block; height: 4px; border-radius: var(--radius-pill); background: var(--md-surface-container-high); overflow: hidden; }
.bar span { display: block; height: 100%; border-radius: inherit; background: var(--color-accent); transition: width .6s var(--md-ease); }
.bar--big { height: 6px; margin-bottom: 14px; }
.bg__body {
  min-width: 0; border: 0; border-radius: var(--radius-xxl);
}
.bg__placeholder {
  display: grid; place-items: center; min-height: 300px;
  border: 0; border-radius: var(--radius-xxl); background: var(--md-surface-container-low);
}
.body-head {
  display: flex; align-items: center; gap: 10px; margin-bottom: 16px; padding-bottom: 14px;
  border-bottom: 1px solid var(--color-hairline-subtle, rgba(20, 20, 19, .08));
}
.back { display: none; }
.title { flex: 1; min-width: 0; margin: 0; font-size: 21px; font-weight: 500; letter-spacing: -.02em; overflow-wrap: anywhere; word-break: normal; }
.facts {
  display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 10px 16px;
  margin: 0 0 16px; padding: 14px 16px; border: 0;
  border-radius: 18px; background: var(--md-surface-container-lowest);
}
.facts div { display: grid; gap: 3px; min-width: 0; }
.facts dt { color: var(--color-muted); font-size: 12px; }
.facts dd { margin: 0; color: var(--color-ink); font-size: 14px; font-weight: 500; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.sub { margin: 22px 0 8px; font-size: 15px; font-weight: 500; letter-spacing: -.01em; color: var(--color-ink); }
.goal { margin: 0; color: var(--color-text-secondary); font-size: 14px; line-height: 1.75; white-space: pre-wrap; overflow-wrap: anywhere; }
.actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; margin-top: 16px; }
.md { font-size: 15px; line-height: 1.8; overflow-wrap: anywhere; }
.md :deep(h1), .md :deep(h2), .md :deep(h3) { margin: 18px 0 8px; font-size: 17px; font-weight: 500; letter-spacing: -.015em; }
.md :deep(p) { margin: 0 0 10px; }
.md :deep(ul), .md :deep(ol) { margin: 0 0 10px; padding-left: 22px; }
.md :deep(table) { display: block; overflow-x: auto; border-collapse: collapse; }
.md :deep(th), .md :deep(td) { padding: 6px 10px; border: 1px solid var(--color-hairline-subtle, rgba(20, 20, 19, .12)); }
.md :deep(pre) { overflow-x: auto; padding: 12px 14px; border-radius: var(--radius-lg); background: var(--md-surface-container-lowest); border: 0; }
@media (max-width: 840px) {
  .bg { grid-template-columns: minmax(0, 1fr); }
  .bg__list { position: static; }
  .bg.has-page .bg__list, .bg:not(.has-page) .bg__body, .bg__placeholder { display: none; }
  .back { display: inline-flex; margin-left: -8px; }
  .body-head { flex-wrap: wrap; }
}
</style>
