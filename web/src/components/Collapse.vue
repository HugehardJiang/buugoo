<script setup lang="ts">
// 平滑展开 / 收起：用 grid 行高从 0fr 过渡到 1fr，不需要测量内容高度。收起后内容不渲染。
import { ref, watch } from 'vue'

const props = defineProps<{ open: boolean }>()
// 收起动画结束后才移除内容，展开时立即渲染。
const rendered = ref(props.open)
watch(
  () => props.open,
  (v) => {
    if (v) rendered.value = true
  },
)
function onEnd(e: TransitionEvent) {
  if (e.target === e.currentTarget && !props.open) rendered.value = false
}
</script>

<template>
  <div class="collapse" :class="{ 'is-open': open && rendered }" @transitionend="onEnd">
    <div class="collapse__inner">
      <slot v-if="rendered" />
    </div>
  </div>
</template>

<style scoped>
.collapse {
  display: grid; grid-template-rows: 0fr; opacity: 0;
  transition: grid-template-rows var(--motion-page) var(--ease-editorial), opacity var(--motion-fast) ease;
}
.collapse.is-open { grid-template-rows: 1fr; opacity: 1; }
.collapse__inner { min-height: 0; overflow: hidden; }
</style>
