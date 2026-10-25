<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import Icon from './Icon.vue'
import { session } from './session'
import { toasts } from './toast'

const route = useRoute()
const inWorkspace = computed(() => route.name !== 'login' && session.loaded && !!session.meta)
const name = computed(() => session.meta?.name ?? '布谷')

const items = [
  { to: '/', route: 'chat', label: '聊天', icon: 'message' as const },
  { to: '/memories', route: 'memories', label: '记忆', icon: 'book' as const },
  { to: '/settings', route: 'settings', label: '设置', icon: 'settings' as const },
]
</script>

<template>
  <div v-if="session.loaded && !session.meta" class="fatal">
    <img src="/logo.png" alt="BUUGOO" width="40" height="40" />
    <p>无法连接到{{ name }}的服务。请确认 buugoo serve 正在运行，然后刷新页面。</p>
  </div>
  <div v-else-if="inWorkspace" class="workspace">
    <nav class="app-rail" aria-label="主导航">
      <div class="app-rail__brand"><img src="/logo.png" :alt="name" /></div>
      <div class="app-rail__nav">
        <RouterLink
          v-for="it in items"
          :key="it.to"
          :to="it.to"
          class="app-rail__item"
          :class="{ 'is-active': route.name === it.route }"
          :title="it.label"
          :aria-label="it.label"
        >
          <Icon :name="it.icon" />
          <span class="app-rail__label">{{ it.label }}</span>
        </RouterLink>
      </div>
    </nav>
    <RouterView v-slot="{ Component }">
      <component :is="Component" :key="route.name" />
    </RouterView>
  </div>
  <RouterView v-else />

  <div class="toast-stack" aria-live="polite">
    <div v-for="t in toasts" :key="t.id" class="toast">{{ t.text }}</div>
  </div>
</template>

<style scoped>
.fatal { max-width: 420px; margin: 18vh auto 0; padding: 0 16px; text-align: center; color: var(--color-text-secondary); }
</style>
