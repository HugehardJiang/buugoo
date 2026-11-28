<script setup lang="ts">
// 设置 → 性格：为布谷选一种预设性格。点一下立即保存，下一条消息开始生效；记忆和心情不受影响。
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type PersonaPreset } from '../api'
import Icon from '../Icon.vue'
import { session } from '../session'
import { toast } from '../toast'

const name = computed(() => session.meta?.name ?? '布谷')
const presets = ref<PersonaPreset[]>([])
const current = ref<string | null>(null)
const saving = ref<string | null>(null)
const loadError = ref('')

onMounted(async () => {
  try {
    const r = await api.presets()
    presets.value = r.presets
    current.value = r.current
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  }
})

async function choose(p: PersonaPreset) {
  if (p.id === current.value || saving.value !== null) return
  const prev = current.value
  current.value = p.id
  saving.value = p.id
  try {
    await api.setPreset(p.id)
    toast(`${name.value}换成了「${p.label}」的性格，下一条消息开始生效`)
  } catch (e) {
    current.value = prev
    toast(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = null
  }
}
</script>

<template>
  <section id="persona" class="panel panel--inset">
    <h2>性格</h2>
    <p class="muted intro">选一种你喜欢的相处方式，随时可以换。换了性格，{{ name }}还是记得你们聊过的事。</p>
    <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
    <div v-if="current === null && !loadError" class="grid" aria-hidden="true">
      <div v-for="i in 5" :key="i" class="skeleton card-skel" />
    </div>
    <div v-else class="grid" role="radiogroup" :aria-label="`${name}的性格`">
      <button
        v-for="p in presets"
        :key="p.id"
        type="button"
        role="radio"
        class="card"
        :class="{ 'is-active': p.id === current }"
        :aria-checked="p.id === current"
        :disabled="saving !== null && saving !== p.id"
        @click="choose(p)"
      >
        <span class="card__head">
          <span class="card__label">{{ p.label }}</span>
          <span class="card__check"><Icon name="check" :size="14" /></span>
        </span>
        <span class="card__summary">{{ p.summary }}</span>
        <span v-if="p.catchphrases.length" class="card__quote">「{{ p.catchphrases[0] }}」</span>
      </button>
    </div>
  </section>
</template>

<style scoped>
.intro { margin: 0 0 18px; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(248px, 1fr)); gap: 12px; }
.card-skel { height: 118px; border-radius: 18px; }
.card {
  position: relative; display: grid; align-content: start; gap: 8px; min-height: 118px; padding: 18px 18px 16px;
  border: 0; border-radius: 20px; background: var(--md-surface-container-low); text-align: left;
  transition: background-color var(--motion-fast) var(--md-ease), box-shadow var(--motion-fast) var(--md-ease), transform var(--motion-fast) var(--md-ease);
}
.card:hover { background: var(--md-surface-container); box-shadow: var(--md-elev-1); transform: translateY(-2px); }
.card:active { transform: scale(.988); }
.card:disabled { cursor: default; opacity: .6; transform: none; }
.card.is-active {
  background: var(--md-primary-container); color: var(--md-on-primary-container);
  box-shadow: var(--md-elev-1);
}
.card__head { display: flex; align-items: center; gap: 8px; }
.card__label { color: var(--color-ink); font-size: 15.5px; font-weight: 500; letter-spacing: -.01em; }
.card.is-active .card__label { color: var(--md-on-primary-container); font-weight: 600; }
.card__check {
  display: inline-flex; width: 22px; height: 22px; margin-left: auto; align-items: center; justify-content: center;
  border: 0; border-radius: 50%; background: var(--md-surface-container-high); color: transparent;
  transition: background-color var(--motion-fast) var(--md-ease), color var(--motion-fast) ease, transform 240ms var(--md-ease);
}
.is-active .card__check { background: var(--color-accent); color: #fff; transform: scale(1.06); }
.card__summary { color: var(--color-text-secondary); font-size: 13.5px; line-height: 1.6; }
.card__quote { color: var(--color-muted); font-size: 12px; line-height: 1.5; }
.card.is-active .card__summary,
.card.is-active .card__quote { color: var(--md-on-primary-container); opacity: .86; }
@media (max-width: 767px) { .grid { grid-template-columns: 1fr 1fr; gap: 10px; } .card { min-height: 0; padding: 14px; border-radius: 16px; } }
@media (max-width: 440px) { .grid { grid-template-columns: 1fr; } }
</style>
