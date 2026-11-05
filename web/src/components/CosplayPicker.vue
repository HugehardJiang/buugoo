<script setup lang="ts">
// 新建 Cosplay 对话：选一张角色卡，再选这段对话和记忆怎么相处。
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api, ApiError, type CharacterCard, type CharacterMemory } from '../api'
import { session } from '../session'
import Icon from '../Icon.vue'
import Modal from './Modal.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; start: [card: CharacterCard, memory: CharacterMemory] }>()

const name = computed(() => session.meta?.name ?? '布谷')
const list = ref<CharacterCard[] | null>(null)
const loadError = ref('')
const query = ref('')
const picked = ref<number | null>(null)
const memory = ref<CharacterMemory>('read')

const memoryOptions = computed<{ v: CharacterMemory; l: string; d: string }[]>(() => [
  { v: 'read', l: '能看记忆，不写记忆', d: `角色知道${name.value}记得的关于你的事，但这里聊的不会写进记忆，也不影响${name.value}的心情` },
  { v: 'off', l: '完全隔离', d: '角色什么都不知道，就像第一次见面；这里聊的也不会被记住' },
  { v: 'full', l: '和普通对话一样', d: `能看也会写记忆、影响心情（角色说的话可能会混进${name.value}的记忆里）` },
])

const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  const all = list.value ?? []
  return q ? all.filter((c) => `${c.name} ${c.description}`.toLowerCase().includes(q)) : all
})
const pickedCard = computed(() => list.value?.find((c) => c.id === picked.value) ?? null)

watch(
  () => props.open,
  async (v) => {
    if (!v) return
    query.value = ''
    loadError.value = ''
    try {
      list.value = (await api.characters()).characters
      if (!list.value.some((c) => c.id === picked.value)) picked.value = list.value[0]?.id ?? null
    } catch (e) {
      loadError.value = e instanceof ApiError ? e.message : '加载失败'
    }
  },
  { immediate: true },
)

function wan(n: number) {
  return n >= 10000 ? `${Math.round(n / 1000) / 10} 万` : n.toLocaleString()
}

function start() {
  if (pickedCard.value) emit('start', pickedCard.value, memory.value)
}
</script>

<template>
  <Modal :open="open" title="Cosplay 对话" :subtitle="`选一张角色卡，这段对话里${name}就扮演它`" wide @close="emit('close')">
    <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
    <div v-else-if="list === null" class="skeleton skel" aria-hidden="true" />
    <div v-else-if="!list.length" class="empty">
      <span class="empty__icon"><Icon name="mask" :size="24" /></span>
      <p>还没有角色卡。先去导入几张，比如 aigengtu.com 上的大肥鱼人设卡。</p>
      <RouterLink to="/settings#characters" class="btn btn-primary" @click="emit('close')">去导入角色卡</RouterLink>
    </div>
    <template v-else>
      <input v-if="list.length > 6" v-model="query" class="text-input search" type="search" placeholder="搜索角色卡" aria-label="搜索角色卡" />
      <div class="cards" role="radiogroup" aria-label="角色卡">
        <button
          v-for="c in shown"
          :key="c.id"
          type="button"
          role="radio"
          class="card"
          :aria-checked="picked === c.id"
          :class="{ 'is-on': picked === c.id }"
          @click="picked = c.id"
          @dblclick="picked = c.id; start()"
        >
          <span class="card__name">{{ c.name }}<span v-if="c.public && !c.mine" class="card__tag">公共</span></span>
          <span v-if="c.description" class="card__desc">{{ c.description }}</span>
          <span class="card__meta">{{ wan(c.runes) }} 字 · 每轮约 {{ c.tokens.toLocaleString() }} token</span>
        </button>
        <p v-if="!shown.length" class="caption">没有找到</p>
      </div>

      <p class="label">记忆</p>
      <div class="mem" role="radiogroup" aria-label="记忆方式">
        <label v-for="o in memoryOptions" :key="o.v" class="mem__opt" :class="{ 'is-on': memory === o.v }">
          <input v-model="memory" type="radio" name="cos-memory" :value="o.v" />
          <span>
            <strong>{{ o.l }}</strong>
            <span class="caption">{{ o.d }}</span>
          </span>
        </label>
      </div>
      <p class="caption cost">角色卡每轮都会整张发给模型（重复的部分大多按缓存命中价计），卡越长越费 token。</p>
    </template>
    <div class="modal-actions">
      <RouterLink v-if="list?.length" to="/settings#characters" class="btn btn-ghost left" @click="emit('close')">管理角色卡</RouterLink>
      <button class="btn" @click="emit('close')">取消</button>
      <button class="btn btn-primary" :disabled="!pickedCard" @click="start">开始</button>
    </div>
  </Modal>
</template>

<style scoped>
.skel { height: 160px; border-radius: var(--radius-md); }
.empty { display: grid; justify-items: center; gap: 10px; padding: 18px 0 4px; text-align: center; color: var(--color-text-secondary); }
.empty p { margin: 0; }
.empty__icon { display: grid; place-items: center; width: 48px; height: 48px; border-radius: 14px; background: var(--color-accent-tint); color: var(--color-accent-active); }
.search { width: 100%; margin-bottom: 10px; }
.cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 8px; max-height: 40vh; overflow-y: auto; padding: 2px; }
.card {
  display: grid; align-content: start; gap: 4px; min-width: 0; padding: 10px 12px; border: 1px solid var(--color-hairline-strong);
  border-radius: var(--radius-md); background: var(--color-surface); text-align: left; transition: border-color .15s ease, box-shadow .15s ease, background-color .15s ease;
}
.card:hover { border-color: var(--color-muted); }
.card.is-on { border-color: var(--color-accent); box-shadow: 0 0 0 3px rgba(91, 122, 74, .14); background: var(--color-accent-tint); }
.card__name { color: var(--color-ink); font-size: 14px; font-weight: 500; overflow-wrap: anywhere; }
.card__tag { margin-left: 6px; padding: 1px 6px; border-radius: var(--radius-pill); background: var(--color-canvas-soft); color: var(--color-text-secondary); font-size: 11px; font-weight: 400; }
.card__desc { color: var(--color-text-secondary); font-size: 12.5px; line-height: 1.45; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.card__meta { color: var(--color-muted); font-size: 11.5px; }
.label { margin: 16px 0 6px; color: var(--color-ink); font-size: 13px; font-weight: 500; }
.mem { display: grid; gap: 6px; }
.mem__opt { display: flex; gap: 10px; align-items: flex-start; padding: 9px 12px; border: 1px solid var(--color-divider); border-radius: var(--radius-md); cursor: pointer; transition: border-color .15s ease, background-color .15s ease; }
.mem__opt:hover { border-color: var(--color-hairline-strong); }
.mem__opt.is-on { border-color: var(--color-accent); background: var(--color-accent-tint); }
.mem__opt input { margin-top: 3px; flex-shrink: 0; accent-color: var(--color-accent); }
.mem__opt > span { display: grid; gap: 2px; }
.mem__opt strong { color: var(--color-ink); font-size: 13.5px; font-weight: 500; }
.cost { margin: 10px 0 0; }
.left { margin-right: auto; }
@media (max-width: 600px) {
  .cards { grid-template-columns: minmax(0, 1fr); max-height: 34vh; }
}
</style>
