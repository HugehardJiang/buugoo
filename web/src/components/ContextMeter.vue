<script setup lang="ts">
// 聊天页顶部的上下文用量环：这段对话大约用了模型上下文的多少；点开看详情、看前情提要、手动压缩。
import { computed, ref, watch } from 'vue'
import { api, ApiError, type ContextStatus } from '../api'
import { session } from '../session'
import { toast } from '../toast'
import Modal from './Modal.vue'

const props = defineProps<{ conversationId: number | null; busy?: boolean }>()
const emit = defineEmits<{ status: [s: ContextStatus | null] }>()

const name = computed(() => session.meta?.name ?? '布谷')
const st = ref<ContextStatus | null>(null)
const open = ref(false)
const compacting = ref(false)
const showSummary = ref(false)

async function refresh() {
  const id = props.conversationId
  if (!id) {
    st.value = null
    emit('status', null)
    return
  }
  try {
    const r = await api.context(id)
    if (props.conversationId !== id) return
    st.value = r
    emit('status', r)
  } catch {
    // 读不到用量不影响聊天。
  }
}
watch(() => props.conversationId, refresh, { immediate: true })
defineExpose({ refresh })

function wan(n: number) {
  if (n >= 10000) return `${Math.round(n / 1000) / 10} 万`
  return String(n)
}

// 环的颜色：接近压缩阈值时变成提醒色。
const level = computed(() => {
  if (!st.value) return 'ok'
  if (st.value.pending || st.value.percent >= st.value.compact_at) return 'full'
  if (st.value.percent >= st.value.compact_at * 0.75) return 'warn'
  return 'ok'
})
const R = 8
const C = 2 * Math.PI * R
const dash = computed(() => `${(C * Math.min(100, st.value?.percent ?? 0)) / 100} ${C}`)

async function compact() {
  if (!props.conversationId || compacting.value) return
  compacting.value = true
  try {
    st.value = await api.compact(props.conversationId)
    emit('status', st.value)
    toast(`整理好了，现在上下文用了大约 ${st.value.percent}%`)
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '压缩失败')
  } finally {
    compacting.value = false
  }
}
</script>

<template>
  <button
    v-if="st"
    type="button"
    class="meter"
    :class="`is-${level}`"
    :title="`上下文已用约 ${st.percent}%`"
    :aria-label="`上下文已用约 ${st.percent}%，查看详情`"
    @click="open = true"
  >
    <svg width="20" height="20" viewBox="0 0 20 20" aria-hidden="true">
      <circle cx="10" cy="10" :r="R" class="meter__track" />
      <circle cx="10" cy="10" :r="R" class="meter__bar" :stroke-dasharray="dash" transform="rotate(-90 10 10)" />
    </svg>
    <span class="meter__pct">{{ st.percent === 0 && st.used > 0 ? '<1' : st.percent }}%</span>
  </button>

  <Modal :open="open" title="上下文" :subtitle="st?.model ? `模型：${st.model}` : undefined" @close="open = false">
    <template v-if="st">
      <div class="bar" :style="{ '--p': `${Math.min(100, st.percent)}%`, '--t': `${st.compact_at}%` }">
        <span class="bar__fill" :class="`is-${level}`" />
        <span class="bar__mark" :title="`超过 ${st.compact_at}% 自动压缩`" />
      </div>
      <p class="usage">
        大约用了 <strong>{{ wan(st.used) }}</strong> / {{ wan(st.size) }} token（{{ st.percent }}%）
      </p>
      <p class="caption">
        <template v-if="st.enabled">
          超过 {{ st.compact_at }}% 时，这条回复照常发出，下一条消息处理前{{ name }}会先把较早的聊天整理成要点，
          最近约 {{ st.keep_recent }}% 的消息保留原文。
        </template>
        <template v-else>管理员没有开启长对话压缩，上下文放不下时最早的消息会被直接丢掉。</template>
      </p>
      <p v-if="st.pending" class="form-flash">已经超过阈值，下一条消息发出后会先整理之前的聊天，再回复。</p>

      <div v-if="st.summary" class="summary">
        <button type="button" class="summary__toggle" :aria-expanded="showSummary" @click="showSummary = !showSummary">
          {{ showSummary ? '收起前情提要' : '看看前情提要（较早的聊天整理成的要点）' }}
        </button>
        <pre v-if="showSummary" class="summary__text">{{ st.summary }}</pre>
      </div>

      <div class="actions">
        <button type="button" class="btn btn-ghost" @click="open = false">关闭</button>
        <button v-if="st.enabled" type="button" class="btn btn-primary" :disabled="compacting || busy" @click="compact">
          {{ compacting ? '正在整理…' : '立即压缩' }}
        </button>
      </div>
    </template>
  </Modal>
</template>

<style scoped>
.meter {
  display: inline-flex; align-items: center; gap: 4px; height: 32px; padding: 0 8px; border-radius: 999px;
  color: var(--color-text-secondary); font-size: 12px; font-variant-numeric: tabular-nums; transition: background-color .15s ease;
}
.meter:hover { background: var(--color-hover-wash); color: var(--color-ink); }
.meter__track { fill: none; stroke: var(--color-hairline-strong); stroke-width: 2.4; }
.meter__bar { fill: none; stroke: var(--color-accent); stroke-width: 2.4; stroke-linecap: round; transition: stroke-dasharray .5s var(--ease-editorial); }
.meter.is-warn .meter__bar { stroke: var(--color-warning); }
.meter.is-full .meter__bar { stroke: var(--color-danger); }
.bar { position: relative; height: 8px; margin: 4px 0 12px; border-radius: 999px; background: var(--color-canvas-soft); }
.bar__fill { position: absolute; inset: 0 auto 0 0; width: var(--p); border-radius: 999px; background: var(--color-accent); transition: width .5s var(--ease-editorial); }
.bar__fill.is-warn { background: var(--color-warning); }
.bar__fill.is-full { background: var(--color-danger); }
.bar__mark { position: absolute; top: -3px; bottom: -3px; left: var(--t); width: 2px; border-radius: 1px; background: var(--color-ink); opacity: .45; }
.usage { margin: 0 0 6px; color: var(--color-ink); font-size: 14px; }
.caption { margin: 0 0 12px; line-height: 1.65; }
.summary { margin: 4px 0 8px; }
.summary__toggle { padding: 4px 0; color: var(--color-accent-active); font-size: 13px; }
.summary__text {
  max-height: 260px; margin: 8px 0 0; padding: 12px; overflow: auto; border-radius: var(--radius-md); background: var(--color-canvas-soft);
  font-family: inherit; font-size: 13px; line-height: 1.7; white-space: pre-wrap;
}
.actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 12px; }
</style>
