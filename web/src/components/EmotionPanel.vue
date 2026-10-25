<script setup lang="ts">
// 布谷对当前用户的情绪、心境与关系，以及最近的情绪变化。
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type EmotionView } from '../api'
import { dateTime } from '../format'
import { session } from '../session'
import { toast } from '../toast'
import Modal from './Modal.vue'

const view = ref<EmotionView | null>(null)
const error = ref('')
const resetting = ref(false)
const name = computed(() => session.meta?.name ?? '布谷')

const labels: Record<string, string> = {
  joy: '开心', sadness: '难过', anger: '生气', worry: '担心', surprise: '惊讶', impatience: '不耐烦', longing: '想念',
  mood: '心境', affinity: '亲近', trust: '信任',
}

async function load() {
  try {
    view.value = await api.emotion()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

function pct(v: number | undefined) {
  return `${Math.round((v ?? 0) * 100)}%`
}

// 心境是 -1 到 1，进度条从中间向两边延伸。
const moodStyle = computed(() => {
  const m = view.value?.mood ?? 0
  const w = Math.abs(m) * 50
  return m >= 0 ? { left: '50%', width: `${w}%` } : { left: `${50 - w}%`, width: `${w}%` }
})

function changeText(changes: Record<string, number>) {
  return Object.entries(changes)
    .filter(([, v]) => Math.abs(v) >= 0.02)
    .sort((a, b) => Math.abs(b[1]) - Math.abs(a[1]))
    .slice(0, 4)
    .map(([k, v]) => `${labels[k] ?? k} ${v > 0 ? '+' : '−'}${Math.round(Math.abs(v) * 100)}`)
    .join('　')
}

async function reset() {
  try {
    await api.resetEmotion()
    resetting.value = false
    toast(`${name.value}对你的情绪和关系已经从头开始`)
    await load()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '操作失败'
  }
}

onMounted(load)
</script>

<template>
  <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
  <section v-if="view && !view.enabled" class="panel panel--inset">
    <p class="muted plain">情绪系统没有开启。</p>
  </section>
  <template v-else-if="view">
    <section class="panel panel--inset now">
      <span class="eyebrow">此刻</span>
      <h2>{{ view.summary }}</h2>
      <p v-if="view.cause" class="muted cause">“{{ view.cause }}”</p>
      <div class="bars">
        <div v-for="e in view.emotions" :key="e.name" class="bar" :class="{ 'is-strong': e.intensity >= 0.25 }">
          <span class="bar__label">{{ e.label }}</span>
          <div class="workspace-progress" :class="`tone-${e.name}`"><i :style="{ width: pct(e.intensity) }" /></div>
          <span class="bar__value">{{ Math.round(e.intensity * 100) }}</span>
        </div>
      </div>
    </section>

    <section class="panel panel--inset">
      <h2>你们之间</h2>
      <p class="relation">{{ view.relation }}</p>
      <div class="bars">
        <div class="bar">
          <span class="bar__label">亲近</span>
          <div class="workspace-progress"><i :style="{ width: pct(view.affinity) }" /></div>
          <span class="bar__value">{{ Math.round((view.affinity ?? 0) * 100) }}</span>
        </div>
        <div class="bar">
          <span class="bar__label">信任</span>
          <div class="workspace-progress"><i :style="{ width: pct(view.trust) }" /></div>
          <span class="bar__value">{{ Math.round((view.trust ?? 0) * 100) }}</span>
        </div>
        <div class="bar">
          <span class="bar__label">心境</span>
          <div class="workspace-progress mood"><i :style="moodStyle" /></div>
          <span class="bar__value">{{ (view.mood ?? 0) > 0.15 ? '愉快' : (view.mood ?? 0) < -0.15 ? '低落' : '平稳' }}</span>
        </div>
      </div>
      <p class="caption">亲近与信任随相处慢慢积累；冒犯的分量比善意更重。心境是最近一段时间心情的底色，会影响{{ name }}对新事情的感受。</p>
    </section>

    <section class="panel panel--inset">
      <h2>最近的心情变化</h2>
      <p v-if="!view.events?.length" class="muted plain">还没有明显的情绪变化。</p>
      <ol v-else class="events">
        <li v-for="(ev, i) in view.events" :key="i" class="event">
          <span class="caption">{{ dateTime(ev.created_at) }}</span>
          <p class="event__cause">{{ ev.cause || (ev.kind === 'pace' ? '聊天节奏' : '一次对话') }}</p>
          <p class="caption">{{ changeText(ev.changes) }}</p>
        </li>
      </ol>
      <div class="actions">
        <button class="btn btn-danger-lite" @click="resetting = true">让{{ name }}重新认识你</button>
      </div>
    </section>

    <Modal
      :open="resetting"
      :title="`让${name}重新认识你？`"
      :subtitle="`${name}对你的情绪、心境、亲近与信任都会回到刚认识时的样子。记忆不受影响。`"
      @close="resetting = false"
    >
      <div class="modal-actions">
        <button class="btn" @click="resetting = false">取消</button>
        <button class="btn btn-danger" @click="reset">从头开始</button>
      </div>
    </Modal>
  </template>
</template>

<style scoped>
.now h2 { margin: 0 0 6px; font-size: 28px; }
.now .eyebrow { margin-bottom: 4px; }
.cause { margin: 0 0 16px; font-size: 15px; }
.plain { margin: 0; }
.relation { margin: -4px 0 14px; font-size: 16px; }
.bars { display: grid; gap: 10px; margin: 8px 0 14px; }
.bar { display: grid; grid-template-columns: 56px 1fr 36px; align-items: center; gap: 12px; font-size: 14px; color: var(--color-text-secondary); }
.bar.is-strong { color: var(--color-ink); }
.bar__value { text-align: right; font-variant-numeric: tabular-nums; color: var(--color-muted); font-size: 12px; }
.tone-anger > i, .tone-impatience > i { background: var(--color-danger); }
.tone-sadness > i, .tone-worry > i { background: var(--color-info); }
.tone-longing > i, .tone-surprise > i { background: var(--color-warning); }
.mood > i { position: absolute; top: 0; }
.events { display: grid; gap: 0; margin: 0 0 12px; padding: 0; list-style: none; }
.event { padding: 12px 0; border-top: 1px solid var(--color-divider); }
.event p { margin: 2px 0 0; }
.event__cause { color: var(--color-ink); font-size: 15px; }
.actions { display: flex; justify-content: flex-end; }
.panel .caption { margin: 0; }
</style>
