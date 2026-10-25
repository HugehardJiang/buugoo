<script setup lang="ts">
// 设计系统 Icons 组的线性图标：24px 网格、1.8 描边、圆角端点、无填充，颜色继承 currentColor。
defineProps<{ name: 'message' | 'book' | 'settings'; size?: number }>()
</script>

<template>
  <svg
    :width="size ?? 20"
    :height="size ?? 20"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <template v-if="name === 'message'">
      <path d="M21 15a4 4 0 0 1-4 4H8l-5 3V7a4 4 0 0 1 4-4h10a4 4 0 0 1 4 4z" />
      <path d="M8 9h8M8 13h5" />
    </template>
    <template v-else-if="name === 'book'">
      <path d="M2 4h6a4 4 0 0 1 4 4v13a4 4 0 0 0-4-4H2z" />
      <path d="M22 4h-6a4 4 0 0 0-4 4v13a4 4 0 0 1 4-4h6z" />
    </template>
    <template v-else-if="name === 'settings'">
      <path d="M4 6h4M12 6h8M4 12h9M17 12h3M4 18h2M10 18h10" />
      <circle cx="10" cy="6" r="2" />
      <circle cx="15" cy="12" r="2" />
      <circle cx="8" cy="18" r="2" />
    </template>
  </svg>
</template>
