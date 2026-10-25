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
.intro { margin: 0 0 16px; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 10px; }
.card-skel { height: 104px; border-radius: var(--radius-lg); }
.card {
  position: relative; display: grid; align-content: start; gap: 6px; min-height: 104px; padding: 14px 14px 12px;
  border: 1px solid var(--color-divider); border-radius: var(--radius-lg); background: var(--color-surface); text-align: left;
  transition: border-color var(--motion-fast) ease, background-color var(--motion-fast) ease, box-shadow var(--motion-fast) ease, transform var(--motion-fast) var(--ease-editorial);
}
.card:hover { border-color: var(--color-hairline-strong); box-shadow: var(--shadow-float); }
.card:active { transform: scale(.985); }
.card:disabled { cursor: default; opacity: .6; }
.card.is-active { border-color: var(--color-accent); background: var(--color-success-tint); box-shadow: 0 0 0 3px rgba(91, 122, 74, .12); }
.card__head { display: flex; align-items: center; gap: 8px; }
.card__label { color: var(--color-ink); font-size: 15px; font-weight: 600; }
.card__check {
  display: inline-flex; width: 20px; height: 20px; margin-left: auto; align-items: center; justify-content: center;
  border: 1px solid var(--color-hairline-strong); border-radius: 50%; color: transparent;
  transition: background-color var(--motion-fast) ease, border-color var(--motion-fast) ease, color var(--motion-fast) ease, transform 240ms var(--ease-editorial);
}
.is-active .card__check { border-color: var(--color-accent); background: var(--color-accent); color: #fff; transform: scale(1.08); }
.card__summary { color: var(--color-text-secondary); font-size: 13px; line-height: 1.55; }
.card__quote { color: var(--color-muted); font-size: 12px; line-height: 1.5; }
@media (max-width: 767px) { .grid { grid-template-columns: 1fr 1fr; } .card { min-height: 0; } }
@media (max-width: 380px) { .grid { grid-template-columns: 1fr; } }
</style>
