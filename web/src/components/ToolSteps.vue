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
  if (name === 'start_task') return 'grid' as const
  if (['set_reminder', 'schedule_task', 'list_reminders', 'cancel_reminder'].includes(name)) return 'clock' as const
  if (name === 'use_skill' || name === 'read_skill_file') return 'book' as const
  if (name.startsWith('mcp_')) return 'link' as const
  if (name.startsWith('memo_')) return 'note' as const
  return 'code' as const
}

// 搜索词与网址直接显示在标题里；代码太长，放进详情。
function inlineInput(ev: ToolEvent) {
  if (!ev.input || ev.name === 'run_python' || ev.name === 'run_shell') return ''
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
          <pre v-if="(ev.name === 'run_python' || ev.name === 'run_shell') && ev.input" class="step__code">{{ ev.input }}</pre>
          <pre v-if="ev.detail" class="step__detail">{{ ev.detail }}</pre>
          <p v-if="!ev.detail && ev.name !== 'run_python' && ev.name !== 'run_shell'" class="caption">{{ ev.input }}</p>
        </div>
      </Collapse>
    </li>
  </ol>
</template>

<style scoped>
/* 列宽用 minmax(0, 1fr)：长的搜索词、提醒内容省略显示，不会把消息撑宽（手机上会左右滑动）。 */
.steps {
  display: grid; grid-template-columns: minmax(0, 1fr); gap: 2px; max-width: min(680px, 100%); min-width: 0;
  margin: 0 0 12px; padding: 6px; list-style: none;
  border: 1px solid rgba(20, 20, 19, .07); border-radius: 18px; background: var(--color-card);
}
.step { min-width: 0; animation: step-in 260ms var(--ease-editorial) both; }
.step__head {
  display: flex; width: 100%; align-items: center; gap: 9px; min-height: 36px; padding: 5px 10px 5px 7px;
  border-radius: 12px; color: var(--color-text-secondary); font-size: 13px; text-align: left;
  transition: background-color .16s var(--ease-editorial), color .16s ease;
}
.step__head:hover { background: rgba(20, 20, 19, .045); color: var(--color-ink); }
.step__icon {
  display: inline-flex; width: 26px; height: 26px; flex: 0 0 26px; align-items: center; justify-content: center;
  border-radius: 50%; background: rgba(20, 20, 19, .055); color: var(--color-text-secondary);
  transition: background-color 240ms ease, color 240ms ease;
}
.is-running .step__icon { background: rgba(91, 122, 74, .14); color: var(--color-accent-active); animation: pulse 1.2s infinite ease-in-out; }
.is-done .step__icon { background: var(--color-accent-tint); color: var(--color-accent-active); }
.is-error .step__icon { background: var(--color-danger-tint); color: var(--color-danger); }
@keyframes pulse { 50% { opacity: .45; } }
.step__label { flex-shrink: 0; color: var(--color-ink); font-weight: 500; letter-spacing: -.01em; }
.step__input { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--color-text-secondary); }
.step__status {
  flex-shrink: 0; margin-left: auto; display: inline-flex; align-items: center; min-height: 22px; padding: 0 9px;
  border-radius: 9999px; background: rgba(20, 20, 19, .05); color: var(--color-text-secondary);
  font-size: 11.5px; font-weight: 500; line-height: 1;
}
.step__status:empty { display: none; }
.is-running .step__status { background: rgba(91, 122, 74, .12); color: var(--color-accent-active); }
.is-done .step__status { background: rgba(220, 230, 211, .75); color: var(--color-accent-active); }
.is-error .step__status { background: var(--color-danger-tint); color: var(--color-danger); }
@keyframes step-in { from { opacity: 0; transform: translateY(4px); } }
.chev { flex-shrink: 0; color: var(--color-muted); transition: transform var(--motion-fast) var(--ease-editorial); }
.chev.is-open { transform: rotate(90deg); }
.step__body { margin: 4px 8px 8px 38px; }
.step__body pre {
  margin: 0 0 6px; padding: 10px 13px; overflow-x: auto;
  border: 1px solid rgba(20, 20, 19, .06); border-radius: 12px; background: var(--color-canvas);
  color: var(--color-text-secondary); font-family: var(--font-mono); font-size: 12px; line-height: 1.62;
  white-space: pre-wrap; word-break: break-word;
}
.step__body pre:last-child { margin-bottom: 0; }
.step__body .caption { margin: 0; overflow-wrap: anywhere; }
@media (max-width: 767px) {
  .step__status { display: none; }
  .step__body { margin: 4px 6px 8px 12px; }
}
@media (prefers-reduced-motion: reduce) {
  .step, .is-running .step__icon { animation: none; }
  .step__head, .step__icon, .chev { transition: none; }
}
</style>
