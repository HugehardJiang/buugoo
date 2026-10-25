<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, ApiError, type Memory } from '../api'
import { dateTime } from '../format'
import { session } from '../session'

const PAGE = 30
const memories = ref<Memory[]>([])
const total = ref(0)
const page = ref(0)
const type = ref('')
const source = ref('')
const q = ref('')
const loading = ref(false)
const error = ref('')
const confirming = ref<number | null>(null)

const pages = computed(() => Math.max(1, Math.ceil(total.value / PAGE)))
const name = computed(() => session.meta?.name ?? '布谷')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const r = await api.memories({ type: type.value, source: source.value, q: q.value, limit: PAGE, offset: page.value * PAGE })
    memories.value = r.memories
    total.value = r.total
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

let timer: ReturnType<typeof setTimeout> | undefined
watch([type, source], () => {
  page.value = 0
  load()
})
watch(q, () => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    page.value = 0
    load()
  }, 300)
})
watch(page, load)

async function remove(id: number) {
  try {
    await api.deleteMemory(id)
    confirming.value = null
    memories.value = memories.value.filter((m) => m.id !== id)
    total.value -= 1
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '删除失败'
  }
}

onMounted(load)
</script>

<template>
  <main class="page">
    <h1>{{ name }}记得的事 <span class="muted count">{{ total }} 条</span></h1>
    <p class="muted intro">
      这些是{{ name }}从你们的聊天里记下的事。「你说过」是你亲口说的，「推测」是{{ name }}自己的推断。
      删除后会立即清除内容，无法恢复。
    </p>

    <div class="filters">
      <input v-model="q" class="input" placeholder="搜索记忆…" />
      <select v-model="type" class="input">
        <option value="">全部类型</option>
        <option value="semantic">事实</option>
        <option value="episodic">事件</option>
      </select>
      <select v-model="source" class="input">
        <option value="">全部来源</option>
        <option value="stated">你说过</option>
        <option value="inferred">推测</option>
      </select>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="!loading && !memories.length" class="muted empty">还没有记忆。多聊聊天，{{ name }}会慢慢记住你。</p>

    <ul class="list">
      <li v-for="m in memories" :key="m.id" class="card item">
        <div class="head">
          <div class="content">{{ m.content }}</div>
          <div class="ops">
            <template v-if="confirming === m.id">
              <button class="btn danger small" @click="remove(m.id)">确认删除</button>
              <button class="btn ghost small" @click="confirming = null">取消</button>
            </template>
            <button v-else class="btn ghost small" @click="confirming = m.id">删除</button>
          </div>
        </div>
        <div class="tags">
          <span class="tag" :class="m.source">{{ m.source === 'stated' ? '你说过' : '推测' }}</span>
          <span class="tag">{{ m.type === 'semantic' ? '事实' : '事件' }}</span>
          <span class="tag" title="重要性">重要性 {{ m.importance }}</span>
          <span class="muted small">记于 {{ dateTime(m.created_at) }}</span>
          <span v-if="m.access_count" class="muted small">· 想起过 {{ m.access_count }} 次</span>
        </div>
      </li>
    </ul>

    <div v-if="pages > 1" class="pager">
      <button class="btn ghost small" :disabled="page === 0" @click="page--">上一页</button>
      <span class="muted">{{ page + 1 }} / {{ pages }}</span>
      <button class="btn ghost small" :disabled="page + 1 >= pages" @click="page++">下一页</button>
    </div>
  </main>
</template>

<style scoped>
.count { font-size: 14px; font-weight: 400; margin-left: 6px; }
.intro { font-size: 14px; margin: -8px 0 16px; }
.filters { display: grid; grid-template-columns: 1fr 120px 120px; gap: 8px; margin-bottom: 16px; }
@media (max-width: 520px) { .filters { grid-template-columns: 1fr 1fr; } .filters input { grid-column: 1 / -1; } }
.list { list-style: none; padding: 0; margin: 0; }
.item { padding: 12px 14px; }
.item + .item { margin-top: 10px; }
.head { display: flex; gap: 10px; align-items: flex-start; margin-bottom: 8px; }
.content { flex: 1; }
.ops { display: flex; gap: 6px; flex-shrink: 0; }
.tags { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.small { font-size: 12px; }
.empty { text-align: center; margin-top: 40px; }
.pager { display: flex; justify-content: center; align-items: center; gap: 12px; margin-top: 16px; }
</style>
