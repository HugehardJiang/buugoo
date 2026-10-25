<script setup lang="ts">
// 设计系统 Modal：白色面板浮在 color-backdrop 上；手机上停靠在底部成为抽屉。
import { onBeforeUnmount, onMounted, ref } from 'vue'

defineProps<{ title: string; subtitle?: string; wide?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const panel = ref<HTMLElement | null>(null)

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

onMounted(() => {
  document.addEventListener('keydown', onKey)
  // 打开后把焦点放进对话框，便于键盘操作。
  const first = panel.value?.querySelector<HTMLElement>('input, select, textarea, button:not(.modal__close)')
  first?.focus()
})
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <div class="modal-backdrop" @mousedown.self="emit('close')">
      <div ref="panel" class="modal" :class="{ 'modal--wide': wide }" role="dialog" aria-modal="true" :aria-label="title">
        <header class="modal__header">
          <div>
            <h2 class="modal__title">{{ title }}</h2>
            <p v-if="subtitle" class="modal__subtitle">{{ subtitle }}</p>
          </div>
          <button class="icon-btn modal__close" type="button" aria-label="关闭" @click="emit('close')">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </header>
        <div class="modal__body">
          <slot />
        </div>
      </div>
    </div>
  </Teleport>
</template>
