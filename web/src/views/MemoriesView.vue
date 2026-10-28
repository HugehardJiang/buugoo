<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, ApiError, type Memory } from '../api'
import EmotionPanel from '../components/EmotionPanel.vue'
import { dateTime } from '../format'
import { memoriesUnlocked, setMemoriesUnlocked } from '../memoryLock'
import { session } from '../session'
import { toast } from '../toast'
import { useRouter } from 'vue-router'

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
const tab = ref<'emotion' | 'memory'>('emotion')

const typeOptions = [
  { v: '', l: '全部' },
  { v: 'semantic', l: '事实' },
  { v: 'episodic', l: '事件' },
]
const sourceOptions = [
  { v: '', l: '全部' },
  { v: 'stated', l: '你说过' },
  { v: 'inferred', l: '推测' },
]

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
    toast('已删除这条记忆')
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '删除失败'
  }
}

const router = useRouter()

function hideAgain() {
  setMemoriesUnlocked(false)
  toast(`大脑皮层合上了。${name.value}的记忆和心情都还在，只是不再摊开给你看`)
  router.push('/')
}

onMounted(() => {
  if (memoriesUnlocked.value) load()
  else router.replace('/settings#memory')
})
</script>

<template>
  <main class="main">
    <header class="topbar">
      <div class="topbar__titles">
        <h1>{{ name }}的大脑皮层</h1>
      </div>
      <div class="head-actions">
        <button class="btn btn-ghost" @click="hideAgain">重新藏起来</button>
      </div>
    </header>

    <div class="page">
      <div class="page__inner">
        <div class="type-switch tabs" role="tablist" aria-label="大脑皮层">
          <button type="button" role="tab" :aria-selected="tab === 'emotion'" :class="{ 'is-active': tab === 'emotion' }" @click="tab = 'emotion'">心情</button>
          <button type="button" role="tab" :aria-selected="tab === 'memory'" :class="{ 'is-active': tab === 'memory' }" @click="tab = 'memory'">记忆 · {{ total }}</button>
        </div>
        <EmotionPanel v-if="tab === 'emotion'" />
        <template v-else>
        <p class="muted intro">
          {{ name }}从聊天里记下的事都在这里。「你说过」是你亲口提到的，「推测」是{{ name }}自己的判断，可能不准。删除后内容会立刻清除，无法恢复。
        </p>

        <div class="filters">
          <input v-model="q" class="text-input search" placeholder="搜索记忆" aria-label="搜索记忆" />
          <div class="type-switch" role="group" aria-label="类型">
            <button v-for="o in typeOptions" :key="o.v" type="button" :class="{ 'is-active': type === o.v }" @click="type = o.v">{{ o.l }}</button>
          </div>
          <div class="type-switch" role="group" aria-label="来源">
            <button v-for="o in sourceOptions" :key="o.v" type="button" :class="{ 'is-active': source === o.v }" @click="source = o.v">{{ o.l }}</button>
          </div>
        </div>

        <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>

        <section class="panel records">
          <p v-if="!loading && !memories.length" class="empty muted">
            {{ q || type || source ? '没有符合条件的记忆。' : `还没有记忆。多聊几句，${name}会慢慢记住你。` }}
          </p>
          <article v-for="m in memories" :key="m.id" class="record">
            <p class="content">{{ m.content }}</p>
            <div class="facts">
              <span class="badge-pill">{{ m.source === 'stated' ? '你说过' : '推测' }}</span>
              <span class="badge-pill">{{ m.type === 'semantic' ? '事实' : '事件' }}</span>
              <span class="caption">重要性 {{ m.importance }} · 记于 {{ dateTime(m.created_at) }}<template v-if="m.access_count"> · 想起过 {{ m.access_count }} 次</template></span>
              <span class="spacer" />
              <template v-if="confirming === m.id">
                <button class="btn btn-ghost" @click="confirming = null">取消</button>
                <button class="btn btn-danger" @click="remove(m.id)">删除</button>
              </template>
              <button v-else class="btn btn-danger-lite" @click="confirming = m.id">删除</button>
            </div>
          </article>
        </section>

        <div v-if="pages > 1" class="pager">
          <button class="btn" :disabled="page === 0" @click="page--">上一页</button>
          <span class="caption">{{ page + 1 }} / {{ pages }}</span>
          <button class="btn" :disabled="page + 1 >= pages" @click="page++">下一页</button>
        </div>
        </template>
      </div>
    </div>
  </main>
</template>

<style scoped>
.head-actions { display: flex; align-items: center; gap: 8px; }
@media (max-width: 767px) { .head-actions .chip-meta { display: none; } }
.tabs { max-width: 280px; margin-bottom: 20px; }
.intro { margin: 0 0 20px; font-size: 15px; line-height: 1.7; }
.filters { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; gap: 10px; margin-bottom: 20px; }
.filters .type-switch { min-width: 220px; }
@media (max-width: 767px) {
  .filters { grid-template-columns: 1fr; }
  .filters .type-switch { min-width: 0; }
}
.records { overflow: hidden; }
.record { padding: var(--surface-inset-md); }
.record + .record { border-top: 1px solid var(--color-divider); }
.content { margin: 0 0 10px; font-size: 16px; line-height: 1.65; }
.facts { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.spacer { flex: 1; }
.empty { margin: 0; padding: 40px var(--surface-inset-md); text-align: center; }
.pager { display: flex; justify-content: center; align-items: center; gap: 12px; margin-top: 20px; }
</style>
