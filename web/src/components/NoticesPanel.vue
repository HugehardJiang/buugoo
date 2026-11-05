<script setup lang="ts">
// 设置 → 通知：管理员发过的所有通知（弹过一次就不再弹，可以在这里再看）。
import { onMounted, ref } from 'vue'
import { api, ApiError, type Notice } from '../api'
import { renderMarkdown } from '../format'
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
.intro { margin: 0 0 12px; }
.skel { height: 80px; border-radius: var(--radius-md); }
.list { margin: 0; padding: 0; list-style: none; }
.item { border-top: 1px solid var(--color-divider); }
.item:first-child { border-top: 0; }
.item__head { display: flex; align-items: center; gap: 10px; width: 100%; padding: 12px 0; text-align: left; }
.item__title { flex: 1; min-width: 0; color: var(--color-ink); font-size: 14px; font-weight: 500; overflow-wrap: anywhere; }
.item__date { flex-shrink: 0; }
.item__chev { flex-shrink: 0; color: var(--color-muted); transform: rotate(-90deg); transition: transform .2s ease; }
.is-open .item__chev { transform: none; }
.item__body { padding: 0 0 14px; font-size: 14px; line-height: 1.7; overflow-wrap: anywhere; }
.item__body :deep(p) { margin: 0 0 8px; }
.item__body :deep(ul), .item__body :deep(ol) { margin: 0 0 8px; padding-left: 20px; }
</style>
