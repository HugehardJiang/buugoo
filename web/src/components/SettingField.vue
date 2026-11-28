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
  scale?: number
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

// 带 scale 的数字（例如每日 token 以百万为单位）：显示时除以 scale，保存时乘回去。
function scaled(v: unknown) {
  const n = Number(v)
  return Number.isFinite(n) ? String(Math.round((n / f.value.scale!) * 1000) / 1000) : String(v ?? '')
}
function editScaled(raw: string) {
  const v = parseFloat(raw)
  emit('edit', raw.trim() === '' || !Number.isFinite(v) ? 0 : Math.round(v * f.value.scale!))
}

function display(v: unknown) {
  if (f.value.type === 'secret') return v ? '已设置' : '未设置'
  if (Array.isArray(v)) return v.join(', ') || '（空）'
  if (v === '' || v === null || v === undefined) return '（空）'
  if (f.value.type === 'bool') return v ? '开启' : '关闭'
  if (f.value.type === 'context') return Number(v) > 0 ? `${Number(v) / 100000} × 10 万` : '自动识别'
  if (f.value.scale) return `${scaled(v)} ${f.value.unit ?? ''}`.trim()
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
          :step="field.type === 'float' || field.scale ? 'any' : '1'"
          :value="field.scale ? scaled(value) : value"
          :disabled="disabled"
          @input="field.scale ? editScaled(($event.target as HTMLInputElement).value) : emit('edit', ($event.target as HTMLInputElement).value)"
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
.sf {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(260px, 420px);
  gap: 10px 36px;
  align-items: start;
  padding: 16px 0;
  border-top: 1px solid rgba(20, 20, 19, .08);
}
.sf:first-child {
  border-top: 0;
}
.sf.is-bool {
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
}
.sf.is-bool .sf__control {
  justify-self: end;
}
.sf__text {
  min-width: 0;
  max-width: 640px;
}
.sf__head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px 8px;
}
.sf__label {
  color: var(--color-ink);
  font-size: 14.5px;
  font-weight: 500;
  letter-spacing: -.01em;
}
.sf.is-dirty .sf__label {
  color: var(--color-accent-active);
}
.sf.is-dirty .sf__label::after {
  content: " ·";
}
.sf__help {
  margin: 5px 0 0;
  color: var(--color-text-secondary);
  font-size: 13px;
  line-height: 1.62;
}
.sf__meta {
  margin: 6px 0 0;
  color: var(--color-muted);
  font-size: 12px;
  line-height: 1.5;
}
.sf__meta code {
  padding: 2px 7px;
  border: 0;
  border-radius: 8px;
  background: var(--md-surface-container);
  font-size: 11.5px;
}
.sf__pending {
  border: 0;
  background: rgba(160, 112, 28, .12);
  color: var(--color-warning);
}
.sf__control {
  min-width: 0;
}
.sf__control .text-input {
  border-radius: 14px;
}
.with-unit {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px 8px;
}
.with-unit .text-input {
  flex: 1;
  min-width: 96px;
}
.unit {
  flex-shrink: 0;
  color: var(--color-text-secondary);
  font-size: 13px;
}
.unit-hint {
  flex-basis: 100%;
  margin: 0;
}
.text-btn {
  margin-left: 8px;
  padding: 0;
  color: var(--color-accent-active);
  font-size: inherit;
  text-decoration: underline;
  text-underline-offset: 3px;
}
@media (max-width: 720px) {
  .sf {
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
  }
  .sf.is-bool {
    grid-template-columns: minmax(0, 1fr) auto;
  }
}
</style>
