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
      用户在聊天里遇到错误时，也可以点「复制诊断信息」，里面带着对方那次请求的编号。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <dl v-if="server" class="info">
      <template v-for="(v, k) in server" :key="k"><dt>{{ k }}</dt><dd>{{ Array.isArray(v) ? v.join('、') : v }}</dd></template>
    </dl>
    <label class="check-row"><input v-model="onlyErrors" type="checkbox" /> 只看错误</label>
    <p v-if="server && !shown.length" class="caption">最近没有{{ onlyErrors ? '错误' : '警告或错误' }}。</p>
    <ul v-if="shown.length" class="log">
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
.panel {
  border-radius: 24px;
  border: 0;
  background: var(--md-surface-container-low);
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}
.head h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 500;
  letter-spacing: -.02em;
}
.head__btns {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.intro {
  margin: 8px 0 16px;
  font-size: 14px;
  line-height: 1.7;
}
.info {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: 6px 20px;
  margin: 0 0 16px;
  padding: 16px 18px;
  border: 0;
  border-radius: 18px;
  background: var(--md-surface-container-lowest);
  font-size: 13px;
}
.info dt {
  color: var(--color-muted);
}
.info dd {
  margin: 0;
  overflow-wrap: anywhere;
  font-family: var(--font-mono);
}
.log {
  margin: 10px 0 0;
  padding: 0;
  list-style: none;
  border: 0;
  border-radius: 18px;
  background: var(--md-surface-container-lowest);
  overflow: hidden;
  font-family: var(--font-mono);
  font-size: 12.5px;
}
.log li {
  padding: 9px 14px;
  border-top: 1px solid rgba(20, 20, 19, .06);
  line-height: 1.65;
  overflow-wrap: anywhere;
}
.log li:first-child {
  border-top: 0;
}
.log .t {
  color: var(--color-muted);
  margin-right: 8px;
}
.log .lv {
  display: inline-block;
  margin-right: 8px;
  padding: 1px 7px;
  border-radius: 9999px;
  font-size: 11px;
  font-weight: 600;
}
.is-error .lv {
  background: var(--color-danger-tint);
  color: var(--color-danger);
}
.is-warn .lv {
  background: rgba(160, 112, 28, .12);
  color: var(--color-warning);
}
.log .msg {
  margin-right: 8px;
  color: var(--color-ink);
}
.log .attr {
  margin-right: 8px;
  color: var(--color-text-secondary);
}
@media (max-width: 767px) {
  .panel {
    border-radius: 20px;
  }
  .info {
    grid-template-columns: minmax(0, 1fr);
    gap: 2px;
  }
  .info dd {
    margin-bottom: 6px;
  }
}
</style>
