<script setup lang="ts">
// 管理后台 → 诊断：服务器信息和最近的警告、错误（只在内存里，重启后清空；密钥已打码），可以一键复制。
import { computed, onMounted, ref } from 'vue'
import { ApiError, type DiagEntry } from '../api'
import { toast } from '../toast'

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

const server = ref<Record<string, unknown> | null>(null)
const recent = ref<DiagEntry[]>([])
const error = ref('')
const onlyErrors = ref(false)

const shown = computed(() => (onlyErrors.value ? recent.value.filter((e) => e.level === 'ERROR') : recent.value).slice().reverse())

onMounted(load)

async function load() {
  try {
    const r = await props.call<{ server: Record<string, unknown>; recent: DiagEntry[] }>('GET', '/diagnostics')
    server.value = r.server
    recent.value = r.recent
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

function fmt(e: DiagEntry) {
  const attrs = Object.entries(e.attrs ?? {}).map(([k, v]) => `${k}=${v}`).join(' ')
  return `${e.time}  ${e.level}  ${e.msg}  ${attrs}`
}

async function copy() {
  const lines = ['==== 布谷服务器诊断 ====', ...Object.entries(server.value ?? {}).map(([k, v]) => `${k}: ${v}`), '', `-- 最近的警告和错误（${recent.value.length} 条）--`,
    ...recent.value.map(fmt), '==== 结束 ====']
  try {
    await navigator.clipboard.writeText(lines.join('\n'))
    toast('已复制')
  } catch {
    toast('复制失败')
  }
}
</script>

<template>
  <section class="panel panel--inset">
    <div class="head">
      <h2>诊断</h2>
      <div class="head__btns">
        <button type="button" class="btn btn-sm" @click="load">刷新</button>
        <button type="button" class="btn btn-sm btn-primary" :disabled="!server" @click="copy">复制诊断信息</button>
      </div>
    </div>
    <p class="muted intro">
      服务器最近的警告和错误（最多 400 条，只保存在内存里，重启后清空），API Key、令牌、密码已经自动打码，可以放心复制给别人看。
      用户在聊天里遇到错误时，也可以点「复制诊断信息」，里面带着他那次请求的编号。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <dl v-if="server" class="info">
      <template v-for="(v, k) in server" :key="k"><dt>{{ k }}</dt><dd>{{ Array.isArray(v) ? v.join('、') : v }}</dd></template>
    </dl>
    <label class="check-row"><input v-model="onlyErrors" type="checkbox" /> 只看错误</label>
    <p v-if="server && !shown.length" class="caption">最近没有{{ onlyErrors ? '错误' : '警告或错误' }}。</p>
    <ul class="log">
      <li v-for="(e, i) in shown" :key="i" :class="`is-${e.level.toLowerCase()}`">
        <span class="t">{{ e.time.slice(5, 19).replace('T', ' ') }}</span>
        <span class="lv">{{ e.level }}</span>
        <span class="msg">{{ e.msg }}</span>
        <span v-for="(v, k) in e.attrs" :key="k" class="attr">{{ k }}={{ v }}</span>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.head h2 { margin: 0; }
.head__btns { display: flex; gap: 8px; }
.intro { margin: 8px 0 12px; font-size: 14px; line-height: 1.7; }
.info { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 4px 16px; margin: 0 0 14px; font-size: 13px; }
.info dt { color: var(--color-muted); }
.info dd { margin: 0; overflow-wrap: anywhere; font-family: var(--font-mono); }
.log { margin: 8px 0 0; padding: 0; list-style: none; font-family: var(--font-mono); font-size: 12px; }
.log li { padding: 7px 0; border-top: 1px solid var(--color-divider); line-height: 1.6; overflow-wrap: anywhere; }
.log .t { color: var(--color-muted); margin-right: 8px; }
.log .lv { margin-right: 8px; font-weight: 600; }
.is-error .lv { color: var(--color-danger); }
.is-warn .lv { color: var(--color-warning); }
.log .msg { margin-right: 8px; color: var(--color-ink); }
.log .attr { margin-right: 8px; color: var(--color-text-secondary); }
</style>
