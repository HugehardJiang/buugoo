<script setup lang="ts">
// 设置 → 通知：管理员发过的所有通知（弹过一次就不再弹，可以在这里再看）。
import { onMounted, ref } from 'vue'
import { api, ApiError, type Notice } from '../api'
import { renderMarkdown } from '../markdown'
import Icon from '../Icon.vue'

const list = ref<Notice[] | null>(null)
const error = ref('')
const open = ref<number | null>(null)

onMounted(async () => {
  try {
    list.value = (await api.notices()).notices
    open.value = list.value[0]?.id ?? null
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
})
</script>

<template>
  <section id="notices" class="panel panel--inset">
    <h2>通知</h2>
    <p class="muted intro">管理员发布的通知。每条只会自动弹出一次，之后可以在这里翻看。</p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <div v-else-if="list === null" class="skeleton skel" aria-hidden="true" />
    <p v-else-if="!list.length" class="caption">还没有通知。</p>
    <ul v-else class="list">
      <li v-for="n in list" :key="n.id" class="item" :class="{ 'is-open': open === n.id }">
        <button type="button" class="item__head" :aria-expanded="open === n.id" @click="open = open === n.id ? null : n.id">
          <span class="item__title">{{ n.title }}</span>
          <span class="caption item__date">{{ new Date(n.created_at).toLocaleDateString() }}</span>
          <Icon name="chevron" :size="14" class="item__chev" />
        </button>
        <!-- eslint-disable-next-line vue/no-v-html -->
        <div v-if="open === n.id && n.content" class="md item__body" v-html="renderMarkdown(n.content)" />
      </li>
    </ul>
  </section>
</template>

<style scoped>
.intro { margin: 0 0 16px; }
.skel { height: 84px; border-radius: 18px; }
.list { display: grid; gap: 10px; margin: 0; padding: 0; list-style: none; }
.item {
  border: 0; border-radius: 20px;
  background: var(--md-surface-container-low); overflow: hidden;
  transition: background-color var(--motion-fast) var(--md-ease), box-shadow var(--motion-fast) var(--md-ease);
}
.item:hover { background: var(--md-surface-container); }
.item.is-open { background: var(--md-surface-container-low); box-shadow: var(--md-elev-1); }
.item__head { display: flex; align-items: center; gap: 12px; width: 100%; padding: 15px 18px; text-align: left; }
.item__title { flex: 1; min-width: 0; color: var(--color-ink); font-size: 14.5px; font-weight: 500; overflow-wrap: anywhere; }
.item__date { flex-shrink: 0; font-variant-numeric: tabular-nums; }
.item__chev { flex-shrink: 0; color: var(--color-muted); transform: rotate(-90deg); transition: transform .22s var(--ease-editorial); }
.is-open .item__chev { transform: none; color: var(--color-ink); }
.item__body {
  padding: 14px 18px 18px; border-top: 1px solid var(--color-hairline-subtle, rgba(20, 20, 19, .07));
  font-size: 14px; line-height: 1.75; overflow-wrap: anywhere;
}
.item__body :deep(p) { margin: 0 0 8px; }
.item__body :deep(ul), .item__body :deep(ol) { margin: 0 0 8px; padding-left: 20px; }
</style>
