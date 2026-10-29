<script setup lang="ts">
// 模型的厂商图标；认不出厂商时显示名字的首字母。
import { computed } from 'vue'
import { vendorOf } from '../vendors'

const props = withDefaults(defineProps<{ model: string; name?: string; size?: number }>(), { name: '', size: 18 })
const vendor = computed(() => vendorOf(props.model, props.name))
const letter = computed(() => (props.name || props.model).trim().charAt(0).toUpperCase() || '?')
</script>

<template>
  <span class="model-icon" :style="{ width: `${size}px`, height: `${size}px`, fontSize: `${size}px` }" :title="vendor?.name" aria-hidden="true">
    <!-- 图标是随程序发布的静态 SVG，不是用户内容。 -->
    <span v-if="vendor" class="model-icon__svg" v-html="vendor.svg" />
    <span v-else class="model-icon__letter" :style="{ fontSize: `${Math.round(size * 0.6)}px` }">{{ letter }}</span>
  </span>
</template>

<style scoped>
.model-icon { display: inline-grid; flex-shrink: 0; place-items: center; line-height: 1; color: var(--color-ink); }
.model-icon__svg { display: grid; place-items: center; width: 100%; height: 100%; }
.model-icon__svg :deep(svg) { width: 1em; height: 1em; }
.model-icon__letter {
  display: grid; place-items: center; width: 100%; height: 100%; border-radius: 30%;
  background: var(--color-canvas-soft); color: var(--color-text-secondary); font-weight: 600;
}
</style>
