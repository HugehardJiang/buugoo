<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import Icon from './Icon.vue'
import { connectedCount, refreshChannels } from './channelState'
import Avatar from './components/Avatar.vue'
import Lightbox from './components/Lightbox.vue'
import { memoriesUnlocked } from './memoryLock'
import { needsLogin, session } from './session'
import { toasts } from './toast'

const route = useRoute()
const inWorkspace = computed(
  () => route.name !== 'login' && route.name !== 'welcome' && session.loaded && !!session.meta && !(route.name === 'console' && needsLogin()),
)
const name = computed(() => session.meta?.name ?? '布谷')

// 「大脑皮层」（记忆与情绪）默认藏起来，用户在设置里确认翻开后才出现在导航中。设置固定在最下面。
// 手机底栏只放常用的几个（聊天、待办、备忘录、我的），其余收进「更多」。short 是手机底栏上的短名字。
type NavItem = { to: string; route: string; label: string; short?: string; icon: 'message' | 'clock' | 'note' | 'brain' | 'folder' | 'link'; more?: boolean }
const items = computed<NavItem[]>(() => [
  { to: '/', route: 'chat', label: '聊天', icon: 'message' },
  { to: '/tasks', route: 'tasks', label: session.meta?.reminders ? '提醒与待办' : '待办', short: '待办', icon: 'clock' },
  { to: '/memos', route: 'memos', label: '备忘录', icon: 'note' },
  ...(memoriesUnlocked.value ? [{ to: '/memories', route: 'memories', label: '大脑皮层', icon: 'brain' as const, more: true }] : []),
  ...(session.meta?.uploads ? [{ to: '/files', route: 'files', label: '文件', icon: 'folder' as const, more: true }] : []),
  ...(session.meta?.channels ? [{ to: '/channels', route: 'channels', label: '聊天软件', icon: 'link' as const, more: true }] : []),
])
const moreItems = computed(() => items.value.filter((it) => it.more))
const moreOpen = ref(false)
// 现在的页面收在「更多」里时，「更多」显示为选中。
const moreActive = computed(() => route.name === 'settings' || moreItems.value.some((it) => it.route === route.name))
watch(() => route.fullPath, () => (moreOpen.value = false))

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
          :class="{ 'is-active': route.name === it.route, 'app-rail__item--more': it.more }"
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
          <span class="app-rail__label">{{ it.short ?? it.label }}</span>
        </RouterLink>
        <span class="app-rail__spacer" />
        <RouterLink
          to="/profile"
          class="app-rail__item"
          :class="{ 'is-active': route.name === 'profile' }"
          title="个人中心"
          aria-label="个人中心"
        >
          <span class="app-rail__icon app-rail__avatar"><Avatar :seed="session.user?.avatar_seed ?? ''" :size="24" label="" /></span>
          <span class="app-rail__label">我的</span>
        </RouterLink>
        <RouterLink
          to="/settings"
          class="app-rail__item app-rail__item--more"
          :class="{ 'is-active': route.name === 'settings' }"
          title="设置"
          aria-label="设置"
        >
          <span class="app-rail__icon"><Icon name="settings" /></span>
          <span class="app-rail__label">设置</span>
        </RouterLink>
        <button
          type="button"
          class="app-rail__item app-rail__more-btn"
          :class="{ 'is-active': moreActive || moreOpen }"
          :aria-expanded="moreOpen"
          aria-label="更多"
          @click="moreOpen = !moreOpen"
        >
          <span class="app-rail__icon">
            <Icon name="grid" />
            <span v-if="session.meta?.channels && linked > 0" class="app-rail__dot is-on" />
          </span>
          <span class="app-rail__label">更多</span>
        </button>
      </div>
    </nav>
    <Transition name="sheet">
      <div v-if="moreOpen" class="more-sheet" @click.self="moreOpen = false">
        <div class="more-sheet__panel" role="menu" aria-label="更多">
          <RouterLink v-for="it in moreItems" :key="it.to" :to="it.to" class="more-sheet__item" role="menuitem" :class="{ 'is-active': route.name === it.route }">
            <span class="app-rail__icon">
              <Icon :name="it.icon" />
              <span v-if="it.route === 'channels'" class="app-rail__dot" :class="{ 'is-on': linked > 0 }" />
            </span>
            <span>{{ it.label }}</span>
            <span v-if="it.route === 'channels'" class="caption">{{ linked ? `已连接 ${linked} 个` : '还没有连接' }}</span>
          </RouterLink>
          <RouterLink to="/settings" class="more-sheet__item" role="menuitem" :class="{ 'is-active': route.name === 'settings' }">
            <span class="app-rail__icon"><Icon name="settings" /></span>
            <span>设置</span>
          </RouterLink>
        </div>
      </div>
    </Transition>
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
.app-rail__avatar :deep(.avatar) { box-shadow: 0 0 0 1.5px var(--color-divider); }
.is-active .app-rail__avatar :deep(.avatar) { box-shadow: 0 0 0 2px var(--color-accent); }
.app-rail__more-btn { display: none; border: 0; background: none; font: inherit; cursor: pointer; }
.more-sheet { display: none; }
@media (max-width: 767px) {
  .app-rail__item--more { display: none !important; }
  .app-rail__more-btn { display: flex; }
  .more-sheet {
    display: block; position: fixed; inset: 0; z-index: 40; background: rgba(20, 20, 19, .18);
  }
  .more-sheet__panel {
    position: absolute; left: 10px; right: 10px; bottom: calc(58px + env(safe-area-inset-bottom) + 8px);
    display: grid; gap: 2px; padding: 8px; border-radius: var(--radius-lg); background: var(--color-surface);
    box-shadow: 0 12px 32px rgba(20, 20, 19, .16), 0 0 0 1px rgba(20, 20, 19, .05);
  }
  .more-sheet__item {
    display: flex; align-items: center; gap: 12px; min-height: 48px; padding: 0 12px; border-radius: var(--radius-md);
    color: var(--color-text-secondary); font-size: 15px; text-decoration: none;
  }
  .more-sheet__item .caption { margin-left: auto; }
  .more-sheet__item.is-active, .more-sheet__item:hover { background: var(--color-hover-wash); color: var(--color-ink); }
  .sheet-enter-active, .sheet-leave-active { transition: opacity .18s ease; }
  .sheet-enter-active .more-sheet__panel, .sheet-leave-active .more-sheet__panel { transition: transform .22s var(--ease-editorial, ease); }
  .sheet-enter-from, .sheet-leave-to { opacity: 0; }
  .sheet-enter-from .more-sheet__panel, .sheet-leave-to .more-sheet__panel { transform: translateY(12px); }
}
.fatal { max-width: 420px; margin: 18vh auto 0; padding: 0 16px; text-align: center; color: var(--color-text-secondary); }
</style>
