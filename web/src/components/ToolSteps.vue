<script setup lang="ts">
// 一轮回复中的工具调用过程：联网搜索、读取网页、运行代码。每一步可以展开查看详情。
import { ref } from 'vue'
import type { ToolEvent } from '../api'
import Icon from '../Icon.vue'
import Collapse from './Collapse.vue'

defineProps<{ steps: ToolEvent[] }>()
const open = ref<Set<string>>(new Set())

function toggle(id: string) {
  const s = new Set(open.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  open.value = s
}

function icon(name: string) {
  if (name === 'web_search') return 'search' as const
  if (name === 'web_fetch') return 'globe' as const
  if (name === 'install_package') return 'download' as const
  if (name === 'send_file') return 'file' as const
  if (['set_reminder', 'schedule_task', 'list_reminders', 'cancel_reminder'].includes(name)) return 'clock' as const
  if (name === 'use_skill' || name === 'read_skill_file') return 'book' as const
  if (name.startsWith('mcp_')) return 'link' as const
  if (name.startsWith('memo_')) return 'note' as const
  return 'code' as const
}

// 搜索词与网址直接显示在标题里；代码太长，放进详情。
function inlineInput(ev: ToolEvent) {
  if (!ev.input || ev.name === 'run_python') return ''
  return ev.input
}
</script>

<template>
  <ol class="steps">
    <li v-for="ev in steps" :key="ev.id" class="step" :class="`is-${ev.status}`">
      <button type="button" class="step__head" :aria-expanded="open.has(ev.id)" @click="toggle(ev.id)">
        <span class="step__icon"><Icon :name="icon(ev.name)" :size="15" /></span>
        <span class="step__label">{{ ev.label }}</span>
        <span v-if="inlineInput(ev)" class="step__input">{{ inlineInput(ev) }}</span>
        <span class="step__status">
          <template v-if="ev.status === 'running'">进行中…</template>
          <template v-else>{{ ev.summary }}</template>
        </span>
        <Icon name="chevron" :size="13" class="chev" :class="{ 'is-open': open.has(ev.id) }" />
      </button>
      <Collapse :open="open.has(ev.id)">
        <div class="step__body">
          <pre v-if="ev.name === 'run_python' && ev.input" class="step__code">{{ ev.input }}</pre>
          <pre v-if="ev.detail" class="step__detail">{{ ev.detail }}</pre>
          <p v-if="!ev.detail && ev.name !== 'run_python'" class="caption">{{ ev.input }}</p>
        </div>
      </Collapse>
    </li>
  </ol>
</template>

<style scoped>
/* 列宽用 minmax(0, 1fr)：长的搜索词、提醒内容省略显示，不会把消息撑宽（手机上会左右滑动）。 */
.steps { display: grid; grid-template-columns: minmax(0, 1fr); gap: 4px; max-width: 640px; min-width: 0; margin: 0 0 10px; padding: 0; list-style: none; }
.step { min-width: 0; }
.step__head {
  display: flex; width: 100%; align-items: center; gap: 8px; min-height: 32px; padding: 4px 8px 4px 6px; margin-left: -6px;
  border-radius: var(--radius-sm); color: var(--color-text-secondary); font-size: 13px; text-align: left;
}
.step__head:hover { background: var(--color-hover-wash); }
.step__icon {
  display: inline-flex; width: 24px; height: 24px; flex: 0 0 24px; align-items: center; justify-content: center;
  border-radius: 50%; background: var(--color-canvas-soft); color: var(--color-text-secondary);
}
.is-running .step__icon { animation: pulse 1.2s infinite ease-in-out; }
.is-done .step__icon { background: var(--color-accent-tint); color: var(--color-accent-active); }
.is-error .step__icon { background: var(--color-danger-tint); color: var(--color-danger); }
@keyframes pulse { 50% { opacity: .45; } }
.step__label { flex-shrink: 0; color: var(--color-ink); font-weight: 500; }
.step__input { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.step__status { flex-shrink: 0; margin-left: auto; color: var(--color-muted); font-size: 12px; }
.step { animation: step-in 280ms var(--ease-editorial) both; }
@keyframes step-in { from { opacity: 0; transform: translateX(-4px); } }
.step__icon { transition: background-color 240ms ease, color 240ms ease; }
.is-error .step__status { color: var(--color-danger); }
.chev { flex-shrink: 0; color: var(--color-muted); transition: transform var(--motion-fast) var(--ease-editorial); }
.chev.is-open { transform: rotate(90deg); }
.step__body { margin: 4px 0 6px 26px; }
.step__body pre {
  margin: 0 0 6px; padding: 10px 12px; overflow-x: auto; border-radius: var(--radius-md); background: var(--color-canvas-soft);
  color: var(--color-text-secondary); font-family: var(--font-mono); font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-word;
}
.step__body .caption { margin: 0; overflow-wrap: anywhere; }
@media (max-width: 767px) { .step__status { display: none; } }
</style>
