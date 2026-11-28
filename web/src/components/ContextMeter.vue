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
    :title="`上下文已用约 ${st.percent}%` + (st.cache_rate != null ? `，最近一条回复的缓存命中率 ${st.cache_rate}%` : '')"
    :aria-label="`上下文已用约 ${st.percent}%` + (st.cache_rate != null ? `，缓存命中 ${st.cache_rate}%` : '') + '，查看详情'"
    @click="open = true"
  >
    <svg width="20" height="20" viewBox="0 0 20 20" aria-hidden="true">
      <circle cx="10" cy="10" :r="R" class="meter__track" />
      <circle cx="10" cy="10" :r="R" class="meter__bar" :stroke-dasharray="dash" transform="rotate(-90 10 10)" />
    </svg>
    <span class="meter__pct">{{ st.percent === 0 && st.used > 0 ? '<1' : st.percent }}%</span>
    <span v-if="st.cache_rate != null" class="meter__cache">缓存 {{ st.cache_rate }}%</span>
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
      <div v-if="st.cache_rate != null" class="cache">
        <p class="usage">
          缓存命中：最近一条回复 <strong>{{ st.cache_rate }}%</strong><template v-if="st.cache_total != null">，这段对话合计 {{ st.cache_total }}%</template>
        </p>
        <p class="caption">
          模型服务会把和上次一样的开头部分缓存起来，命中的部分算得快、也便宜很多。聊得越久命中率一般越高；
          刚压缩过、换了模型或者隔了很久再聊时会低一些。
        </p>
      </div>
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
  display: inline-flex; align-items: center; gap: 6px; height: 32px; padding: 0 11px 0 7px;
  border: 1px solid rgba(20, 20, 19, .09); border-radius: 9999px; background: rgba(255, 255, 255, .72);
  color: var(--color-text-secondary); font-size: 12px; font-weight: 500; font-variant-numeric: tabular-nums;
  transition: background-color .18s var(--ease-editorial), border-color .18s var(--ease-editorial), color .18s ease, box-shadow .18s ease;
}
.meter:hover { background: var(--color-surface); border-color: rgba(20, 20, 19, .18); color: var(--color-ink); box-shadow: 0 2px 8px rgba(20, 20, 19, .05); }
.meter.is-warn { border-color: rgba(160, 112, 28, .28); }
.meter.is-full { border-color: rgba(181, 51, 51, .28); background: rgba(255, 240, 240, .75); }
.meter__pct { letter-spacing: -.01em; }
.meter__cache {
  margin-left: 2px; padding-left: 8px; border-left: 1px solid rgba(20, 20, 19, .1);
  color: var(--color-accent-active); font-weight: 500;
}
@media (max-width: 560px) {
  .meter { gap: 4px; padding: 0 8px 0 6px; }
  .meter__cache { margin-left: 1px; padding-left: 6px; }
}
.cache {
  margin-top: 12px; padding: 14px 16px; border: 1px solid rgba(20, 20, 19, .06);
  border-radius: 16px; background: var(--color-card);
}
.cache .caption { margin-bottom: 0; }
.meter__track { fill: none; stroke: rgba(20, 20, 19, .12); stroke-width: 2.3; }
.meter__bar { fill: none; stroke: var(--color-accent); stroke-width: 2.3; stroke-linecap: round; transition: stroke-dasharray .5s var(--ease-editorial); }
.meter.is-warn .meter__bar { stroke: var(--color-warning); }
.meter.is-full .meter__bar { stroke: var(--color-danger); }
.bar { position: relative; height: 6px; margin: 6px 0 14px; border-radius: 9999px; background: rgba(20, 20, 19, .08); }
.bar__fill { position: absolute; inset: 0 auto 0 0; width: var(--p); border-radius: 9999px; background: var(--color-accent); transition: width .5s var(--ease-editorial); }
.bar__fill.is-warn { background: var(--color-warning); }
.bar__fill.is-full { background: var(--color-danger); }
.bar__mark { position: absolute; top: -4px; bottom: -4px; left: var(--t); width: 2px; border-radius: 2px; background: var(--color-ink); opacity: .45; }
.usage { margin: 0 0 6px; color: var(--color-ink); font-size: 14px; letter-spacing: -.01em; }
.caption { margin: 0 0 12px; line-height: 1.65; }
.summary { margin: 12px 0 8px; }
.summary__toggle {
  display: inline-flex; align-items: center; min-height: 30px; padding: 0 12px;
  border: 1px solid rgba(91, 122, 74, .22); border-radius: 9999px; background: rgba(220, 230, 211, .45);
  color: var(--color-accent-active); font-size: 12.5px; font-weight: 500;
  transition: background-color .16s ease, border-color .16s ease;
}
.summary__toggle:hover { background: var(--color-accent-tint); border-color: rgba(91, 122, 74, .36); }
.summary__text {
  max-height: 260px; margin: 10px 0 0; padding: 14px 16px; overflow: auto;
  border: 1px solid rgba(20, 20, 19, .06); border-radius: 16px; background: var(--color-card);
  font-family: inherit; font-size: 13px; line-height: 1.72; white-space: pre-wrap;
}
.actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
.actions .btn { border-radius: 9999px; }
@media (prefers-reduced-motion: reduce) {
  .meter, .meter__bar, .bar__fill, .summary__toggle { transition: none; }
}
</style>
