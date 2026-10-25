<script setup lang="ts">
// 设计系统 Modal：白色面板浮在 color-backdrop 上；手机上停靠在底部成为抽屉。
// 用 :open 控制显示，打开和关闭都有过渡动画；也可以直接 v-if（只有打开动画）。
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(defineProps<{ title: string; subtitle?: string; wide?: boolean; open?: boolean }>(), { open: true })
const emit = defineEmits<{ close: [] }>()
const panel = ref<HTMLElement | null>(null)

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open) emit('close')
}

// 打开后把焦点放进对话框，便于键盘操作。
async function focusFirst() {
  await nextTick()
  const first = panel.value?.querySelector<HTMLElement>('input, select, textarea, button:not(.modal__close)')
  first?.focus({ preventScroll: true })
}

watch(
  () => props.open,
  (v) => v && focusFirst(),
)
onMounted(() => {
  document.addEventListener('keydown', onKey)
  if (props.open) focusFirst()
})
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <Transition name="modal" appear>
      <div v-if="open" class="modal-backdrop" @mousedown.self="emit('close')">
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
    </Transition>
  </Teleport>
</template>
