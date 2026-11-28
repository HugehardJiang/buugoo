<script setup lang="ts">
// 管理后台 → 主动关心记录：「只观察」模式下，布谷本来会主动找谁、为什么。看着合适了再把模式改成「自动」。
import { onMounted, ref } from 'vue'
import { ApiError } from '../api'
import { shortWhen } from '../format'

type Observation = { user_id: number; username: string; reason: string; at: string }
const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()
const mode = ref('')
const care = ref(false)
const list = ref<Observation[] | null>(null)
const error = ref('')

onMounted(load)
async function load() {
  try {
    const r = await props.call<{ mode: string; care: boolean; observations: Observation[] | null }>('GET', '/proactive/observed')
    mode.value = r.mode
    care.value = r.care
    list.value = r.observations ?? []
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}
</script>

<template>
  <section class="panel panel--inset">
    <div class="head">
      <h2>主动关心记录</h2>
      <button class="btn btn-sm" @click="load">刷新</button>
    </div>
    <p class="caption">
      在「系统设置 → 主动关心」里把模式设成「只观察」后，布谷照常判断要不要主动找人，但不真的发出去，只记在这里（最近 200 条）。看着合适了再改成「自动」。
    </p>
    <p v-if="!care" class="form-flash">主动关心现在是关着的。</p>
    <p v-else-if="mode !== 'observe'" class="form-flash">当前模式是「自动」，会真的发消息，这里不再记新的。</p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <p v-else-if="list && !list.length" class="caption">还没有记录。</p>
    <ul v-else-if="list" class="obs">
      <li v-for="(o, i) in list" :key="i">
        <span class="obs__who">{{ o.username }}</span>
        <span class="caption">{{ shortWhen(o.at) }}</span>
        <p class="obs__why">{{ o.reason }}</p>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.panel {
  border-radius: 24px;
  border: 1px solid rgba(20, 20, 19, .05);
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}
.head h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 500;
  letter-spacing: -.02em;
}
.obs {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin: 14px 0 0;
  padding: 0;
  list-style: none;
}
.obs li {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 4px 10px;
  padding: 14px 16px;
  border: 1px solid rgba(20, 20, 19, .08);
  border-radius: 16px;
  background: var(--color-canvas);
}
.obs__who {
  color: var(--color-ink);
  font-weight: 500;
}
.obs__why {
  flex-basis: 100%;
  margin: 2px 0 0;
  color: var(--color-text-secondary);
  font-size: 14px;
  line-height: 1.65;
  overflow-wrap: anywhere;
}
@media (max-width: 960px) {
  .obs {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 767px) {
  .panel {
    border-radius: 20px;
  }
}
</style>
