<script setup lang="ts">
// 思考等级滑块：拖动时把手跟着手指走，松手后带一点回弹地吸到最近的档位；
// 也可以点档位文字、点轨道，或用方向键 / Home / End 调。
import { computed, ref } from 'vue'

export interface ThinkingStop {
  v: string
  l: string
  /** 选中时显示在下面的一句说明 */
  d?: string
}

const props = defineProps<{ stops: ThinkingStop[]; modelValue: string; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [v: string] }>()

const track = ref<HTMLElement | null>(null)
// 拖动中的位置（0~1）；不在拖动时为 null，显示选中的档位。
const drag = ref<number | null>(null)

const last = computed(() => Math.max(1, props.stops.length - 1))
const index = computed(() => Math.max(0, props.stops.findIndex((s) => s.v === props.modelValue)))
// 拖动时显示离手指最近的档位，松手前就能看到会选中哪一档。
const shown = computed(() => (drag.value == null ? index.value : Math.round(drag.value * last.value)))
const pos = computed(() => (drag.value ?? index.value / last.value) * 100)
const current = computed(() => props.stops[shown.value])

function at(i: number) {
  return `${(i / last.value) * 100}%`
}

function fraction(e: PointerEvent) {
  const r = track.value!.getBoundingClientRect()
  return Math.min(1, Math.max(0, (e.clientX - r.left) / r.width))
}

function choose(i: number) {
  const s = props.stops[Math.min(props.stops.length - 1, Math.max(0, i))]
  if (s && s.v !== props.modelValue) emit('update:modelValue', s.v)
}

function onDown(e: PointerEvent) {
  if (props.disabled || e.button !== 0) return
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  ;(e.currentTarget as HTMLElement).focus({ preventScroll: true })
  drag.value = fraction(e)
}
function onMove(e: PointerEvent) {
  if (drag.value != null) drag.value = fraction(e)
}
function onUp() {
  if (drag.value == null) return
  const i = shown.value
  drag.value = null
  choose(i)
}

function onKey(e: KeyboardEvent) {
  const map: Record<string, number> = {
    ArrowLeft: index.value - 1, ArrowDown: index.value - 1, ArrowRight: index.value + 1, ArrowUp: index.value + 1,
    Home: 0, End: props.stops.length - 1,
  }
  if (!(e.key in map)) return
  e.preventDefault()
  choose(map[e.key])
}
</script>

<template>
  <div class="ts" :class="{ 'is-dragging': drag != null, 'is-disabled': disabled }">
    <div
      ref="track"
      class="ts__rail"
      role="slider"
      tabindex="0"
      aria-label="思考等级"
      :aria-valuemin="0"
      :aria-valuemax="stops.length - 1"
      :aria-valuenow="shown"
      :aria-valuetext="current?.l"
      :aria-disabled="disabled || undefined"
      @pointerdown="onDown"
      @pointermove="onMove"
      @pointerup="onUp"
      @pointercancel="onUp"
      @keydown="onKey"
    >
      <span class="ts__track" />
      <span class="ts__fill" :style="{ width: `calc(${pos}% + var(--r))` }" />
      <span
        v-for="(s, i) in stops"
        :key="s.v"
        class="ts__dot"
        :class="{ 'is-on': (i / last) * 100 <= pos + 0.5 }"
        :style="{ left: at(i) }"
      />
      <span class="ts__thumb" :style="{ left: `${pos}%` }" />
    </div>
    <div class="ts__labels">
      <button
        v-for="(s, i) in stops"
        :key="s.v"
        type="button"
        tabindex="-1"
        :class="{ 'is-active': i === shown }"
        :style="{ left: at(i) }"
        :disabled="disabled"
        @click="choose(i)"
      >
        {{ s.l }}
      </button>
    </div>
    <Transition name="ts-fade" mode="out-in">
      <p v-if="current?.d" :key="current.v" class="ts__desc">{{ current.d }}</p>
    </Transition>
  </div>
</template>

<style scoped>
/* 两端各留出把手的半径，档位和文字都对准把手中心。 */
.ts { --r: 9px; --spring: cubic-bezier(.34, 1.56, .64, 1); user-select: none; -webkit-user-select: none; }
.ts__rail {
  position: relative; height: 26px; margin: 0 var(--r); cursor: pointer; touch-action: none; outline: none;
}
.ts__rail:focus-visible .ts__thumb { box-shadow: 0 0 0 4px rgba(91, 122, 74, .22), 0 1px 4px rgba(20, 20, 19, .22); }
.ts__track {
  position: absolute; top: 50%; left: calc(-1 * var(--r)); right: calc(-1 * var(--r)); height: 6px; margin-top: -3px;
  border-radius: 999px; background: var(--color-hairline-strong);
}
/* 填充条从轨道最左端开始，宽度要补上左边留出的半径。 */
.ts__fill {
  position: absolute; top: 50%; left: calc(-1 * var(--r)); height: 6px; margin-top: -3px; border-radius: 999px;
  background: linear-gradient(90deg, rgba(91, 122, 74, .5), var(--color-accent));
  transition: width .38s var(--spring);
}
.ts__dot {
  position: absolute; top: 50%; width: 4px; height: 4px; margin: -2px 0 0 -2px; border-radius: 50%;
  background: var(--color-muted); opacity: .55; transition: background-color .2s ease, opacity .2s ease; pointer-events: none;
}
.ts__dot.is-on { background: #fff; opacity: .9; }
.ts__thumb {
  position: absolute; top: 50%; width: 18px; height: 18px; margin: -9px 0 0 -9px; border-radius: 50%;
  background: var(--color-surface); border: 2px solid var(--color-accent);
  box-shadow: 0 1px 4px rgba(20, 20, 19, .22);
  transition: left .38s var(--spring), transform .18s ease, box-shadow .18s ease;
  pointer-events: none;
}
.ts__rail:hover .ts__thumb { transform: scale(1.08); }
.is-dragging .ts__fill, .is-dragging .ts__thumb { transition-property: transform, box-shadow; }
.is-dragging .ts__thumb { transform: scale(1.18); box-shadow: 0 0 0 6px rgba(91, 122, 74, .16), 0 2px 8px rgba(20, 20, 19, .24); }
.is-disabled { opacity: .5; }
.is-disabled .ts__rail { cursor: default; }

.ts__labels { position: relative; height: 24px; margin: 2px var(--r) 0; }
.ts__labels button {
  position: absolute; top: 0; transform: translateX(-50%); padding: 3px 4px; border-radius: 6px;
  color: var(--color-muted); font-size: 12px; white-space: nowrap; transition: color .2s ease, transform .2s var(--spring);
}
.ts__labels button:hover:not(:disabled) { color: var(--color-text-secondary); }
.ts__labels button.is-active { color: var(--color-ink); font-weight: 600; transform: translateX(-50%) translateY(-1px); }
.ts__desc { margin: 2px 0 0; color: var(--color-muted); font-size: 11.5px; line-height: 1.5; text-align: center; }
.ts-fade-enter-active, .ts-fade-leave-active { transition: opacity .16s ease, transform .16s ease; }
.ts-fade-enter-from { opacity: 0; transform: translateY(3px); }
.ts-fade-leave-to { opacity: 0; transform: translateY(-3px); }

@media (prefers-reduced-motion: reduce) {
  .ts__fill, .ts__thumb, .ts__labels button { transition: none; }
}
</style>
