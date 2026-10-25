<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { useRoute } from 'vue-router'
import Icon from './Icon.vue'
import { connectedCount, refreshChannels } from './channelState'
import Lightbox from './components/Lightbox.vue'
import { memoriesUnlocked } from './memoryLock'
import { needsLogin, session } from './session'
import { toasts } from './toast'

const route = useRoute()
const inWorkspace = computed(
  () => route.name !== 'login' && session.loaded && !!session.meta && !(route.name === 'console' && needsLogin()),
)
const name = computed(() => session.meta?.name ?? '布谷')

// 「大脑皮层」（记忆与情绪）默认藏起来，用户在设置里确认翻开后才出现在导航中。设置固定在最下面（手机上在最右边）。
const items = computed(() => [
  { to: '/', route: 'chat', label: '聊天', icon: 'message' as const },
  ...(session.meta?.reminders ? [{ to: '/tasks', route: 'tasks', label: '定时任务', icon: 'clock' as const }] : []),
  ...(memoriesUnlocked.value ? [{ to: '/memories', route: 'memories', label: '大脑皮层', icon: 'brain' as const }] : []),
  ...(session.meta?.uploads ? [{ to: '/files', route: 'files', label: '文件', icon: 'folder' as const }] : []),
  ...(session.meta?.channels ? [{ to: '/channels', route: 'channels', label: '聊天软件', icon: 'link' as const }] : []),
])

// 聊天软件入口上的绿点：自己连上了几个聊天软件。登录后读取，之后每分钟刷新一次。
const linked = computed(() => connectedCount())
let channelTimer: ReturnType<typeof setInterval> | undefined
watch(
  () => inWorkspace.value && !!session.meta?.channels && !needsLogin(),
  (on) => {
    clearInterval(channelTimer)
    if (!on) return
    refreshChannels()
    channelTimer = setInterval(() => document.visibilityState === 'visible' && refreshChannels(), 60000)
  },
  { immediate: true },
)
onBeforeUnmount(() => clearInterval(channelTimer))
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
          <span class="app-rail__icon">
            <Icon :name="it.icon" />
            <span
              v-if="it.route === 'channels'"
              class="app-rail__dot"
              :class="{ 'is-on': linked > 0 }"
              :title="linked ? `已连接 ${linked} 个` : '还没有连接'"
            />
          </span>
          <span class="app-rail__label">{{ it.label }}</span>
        </RouterLink>
        <span class="app-rail__spacer" />
        <RouterLink
          to="/settings"
          class="app-rail__item"
          :class="{ 'is-active': route.name === 'settings' }"
          title="设置"
          aria-label="设置"
        >
          <span class="app-rail__icon"><Icon name="settings" /></span>
          <span class="app-rail__label">设置</span>
        </RouterLink>
      </div>
    </nav>
    <RouterView v-slot="{ Component }">
      <component :is="Component" :key="route.name" />
    </RouterView>
  </div>
  <RouterView v-else />

  <Lightbox />
  <TransitionGroup tag="div" name="toast" class="toast-stack" aria-live="polite">
    <div v-for="t in toasts" :key="t.id" class="toast">{{ t.text }}</div>
  </TransitionGroup>
</template>

<style scoped>
.fatal { max-width: 420px; margin: 18vh auto 0; padding: 0 16px; text-align: center; color: var(--color-text-secondary); }
</style>
