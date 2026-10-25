<script setup lang="ts">
// 管理后台的一个设置项：按类型显示开关、下拉框、带单位的数字、时长、上下文长度（以十万 token 为单位）等。
import { computed } from 'vue'

export interface Field {
  key: string
  group: string
  section: string
  advanced?: boolean
  order: number
  unit?: string
  label: string
  help?: string
  type: 'string' | 'bool' | 'int' | 'float' | 'duration' | 'list' | 'select' | 'secret' | 'context'
  options?: string[]
  value: unknown
  saved?: unknown
  pending: boolean
  locked: boolean
  env: boolean
}

const props = defineProps<{ field: Field; value: unknown; dirty: boolean }>()
const emit = defineEmits<{ edit: [v: unknown]; discard: [] }>()

const f = computed(() => props.field)
const disabled = computed(() => f.value.locked || f.value.env)
const id = computed(() => `s-${f.value.key}`)

// 上下文长度在网页上以十万 token 为单位：保存时换算成 token。
const contextShown = computed(() => {
  const n = Number(props.value)
  return n > 0 ? String(Math.round((n / 100000) * 1000) / 1000) : ''
})
const contextHint = computed(() => {
  const n = Number(props.value)
  if (!(n > 0)) return '自动识别'
  return n >= 10000 ? `= ${Math.round(n / 1000) / 10} 万 token` : `= ${n} token`
})
function editContext(raw: string) {
  const v = parseFloat(raw)
  emit('edit', raw.trim() === '' || !(v > 0) ? 0 : Math.round(v * 100000))
}

function display(v: unknown) {
  if (f.value.type === 'secret') return v ? '已设置' : '未设置'
  if (Array.isArray(v)) return v.join(', ') || '（空）'
  if (v === '' || v === null || v === undefined) return '（空）'
  if (f.value.type === 'bool') return v ? '开启' : '关闭'
  if (f.value.type === 'context') return Number(v) > 0 ? `${Number(v) / 100000} × 10 万` : '自动识别'
  return String(v) + (f.value.unit ? ` ${f.value.unit}` : '')
}
const hasSaved = computed(() => f.value.saved !== undefined && f.value.saved !== null)
</script>

<template>
  <div class="sf" :class="{ 'is-dirty': dirty, 'is-bool': f.type === 'bool' }">
    <div class="sf__text">
      <div class="sf__head">
        <label class="sf__label" :for="id">{{ field.label }}</label>
        <span v-if="field.pending" class="badge-pill sf__pending">待重启生效</span>
        <span v-if="field.env" class="badge-pill">由环境变量设置</span>
        <span v-if="field.locked" class="badge-pill">只能在配置文件中修改</span>
      </div>
      <p v-if="field.help" class="sf__help">{{ field.help }}</p>
      <p class="sf__meta">
        <code>{{ field.key }}</code>
        <template v-if="field.pending"> · 正在运行：{{ display(field.value) }}</template>
        <button v-if="hasSaved" type="button" class="text-btn" @click="emit('discard')">恢复为配置文件中的设置</button>
      </p>
    </div>

    <div class="sf__control">
      <button
        v-if="field.type === 'bool'"
        :id="id"
        type="button"
        class="switch"
        role="switch"
        :aria-checked="Boolean(value)"
        :class="{ 'is-on': Boolean(value) }"
        :disabled="disabled"
        @click="emit('edit', !value)"
      >
        <span class="switch__knob" />
      </button>

      <select
        v-else-if="field.type === 'select'"
        :id="id"
        class="text-input"
        :value="value"
        :disabled="disabled"
        @change="emit('edit', ($event.target as HTMLSelectElement).value)"
      >
        <option v-for="o in field.options" :key="o" :value="o">{{ o || '（沿用聊天模型）' }}</option>
      </select>

      <div v-else-if="field.type === 'context'" class="with-unit">
        <input
          :id="id"
          class="text-input"
          type="number"
          min="0"
          step="0.01"
          placeholder="自动"
          :value="contextShown"
          :disabled="disabled"
          @input="editContext(($event.target as HTMLInputElement).value)"
        />
        <span class="unit">× 10 万 token</span>
        <span class="caption unit-hint">{{ contextHint }}</span>
      </div>

      <div v-else-if="field.type === 'int' || field.type === 'float'" class="with-unit">
        <input
          :id="id"
          class="text-input"
          type="number"
          :step="field.type === 'float' ? 'any' : '1'"
          :value="value"
          :disabled="disabled"
          @input="emit('edit', ($event.target as HTMLInputElement).value)"
        />
        <span v-if="field.unit" class="unit">{{ field.unit }}</span>
      </div>

      <input
        v-else
        :id="id"
        class="text-input"
        :type="field.type === 'secret' ? 'password' : 'text'"
        autocomplete="off"
        :value="value"
        :placeholder="
          field.type === 'secret' ? (field.value ? '已设置，留空表示不修改' : '未设置')
          : field.type === 'duration' ? '例如 30s、10m、2h'
          : field.type === 'list' ? '多个用逗号分隔' : ''
        "
        :disabled="disabled"
        @input="emit('edit', ($event.target as HTMLInputElement).value)"
      />
    </div>
  </div>
</template>

<style scoped>
.sf { display: grid; grid-template-columns: minmax(0, 1fr) minmax(220px, 300px); gap: 8px 24px; align-items: start; padding: 14px 0; border-top: 1px solid var(--color-divider); }
.sf:first-child { border-top: 0; }
.sf.is-bool .sf__control { justify-self: end; }
.sf__head { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 8px; }
.sf__label { color: var(--color-ink); font-size: 14px; font-weight: 500; }
.sf.is-dirty .sf__label { color: var(--color-accent-active); }
.sf.is-dirty .sf__label::after { content: " ·"; }
.sf__help { margin: 4px 0 0; color: var(--color-text-secondary); font-size: 13px; line-height: 1.6; }
.sf__meta { margin: 4px 0 0; color: var(--color-muted); font-size: 12px; }
.sf__meta code { font-size: 11.5px; }
.sf__pending { border-color: var(--color-warning); color: var(--color-warning); }
.sf__control { min-width: 0; }
.with-unit { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 8px; }
.with-unit .text-input { flex: 1; min-width: 90px; }
.unit { flex-shrink: 0; color: var(--color-text-secondary); font-size: 13px; }
.unit-hint { flex-basis: 100%; margin: 0; }
.text-btn { margin-left: 8px; padding: 0; color: var(--color-accent-active); font-size: inherit; text-decoration: underline; text-underline-offset: 3px; }
.switch {
  position: relative; flex-shrink: 0; width: 44px; height: 26px; border-radius: 999px; background: var(--color-hairline-strong);
  transition: background-color 220ms var(--ease-editorial);
}
.switch:disabled { opacity: .5; }
.switch.is-on { background: var(--color-accent); }
.switch__knob {
  position: absolute; top: 3px; left: 3px; width: 20px; height: 20px; border-radius: 50%; background: #fff;
  box-shadow: 0 1px 3px rgba(20, 20, 19, .25); transition: transform 260ms var(--ease-editorial);
}
.switch.is-on .switch__knob { transform: translateX(18px); }
@media (max-width: 640px) {
  .sf { grid-template-columns: minmax(0, 1fr); }
  .sf.is-bool { grid-template-columns: minmax(0, 1fr) auto; }
}
</style>
