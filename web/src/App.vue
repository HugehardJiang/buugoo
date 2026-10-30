<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from './Icon.vue'
import { api, type Conversation } from './api'
import { connectedCount, refreshChannels } from './channelState'
import { recentQuery, searching, sidebarCollapsed, wideScreen } from './sidebarState'
import Avatar from './components/Avatar.vue'
import Lightbox from './components/Lightbox.vue'
import { memoriesUnlocked } from './memoryLock'
import { needsLogin, session } from './session'
import { toasts } from './toast'

const route = useRoute()
const router = useRouter()
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

// 桌面端侧栏（参考 claude.ai）：上面是功能入口，中间是最近的对话，最下面是自己。
// 聊天页会把自己的对话列表放进 #app-recents；其他页面在这里读一份最近的对话。
const sideItems = computed(() => items.value.filter((it) => it.route !== 'chat'))
const recents = ref<Conversation[]>([])
const recentsOpen = ref(true)
const searchInput = ref<HTMLInputElement | null>(null)
const onChat = computed(() => route.name === 'chat')
const shownRecents = computed(() => {
  const q = recentQuery.value.trim().toLowerCase()
  return recents.value.filter((c) => !q || (c.title || '新对话').toLowerCase().includes(q)).slice(0, 40)
})
watch(
  () => inWorkspace.value && wideScreen.value && !onChat.value && !needsLogin(),
  async (on) => {
    if (!on) return
    try {
      recents.value = (await api.conversations()).conversations
    } catch {
      // 只是侧栏的列表，读不到就空着。
    }
  },
  { immediate: true },
)

function newChat() {
  router.push({ path: '/', query: { new: String(Date.now()) } })
}

async function toggleSearch() {
  if (sidebarCollapsed.value) sidebarCollapsed.value = false
  searching.value = !searching.value
  if (!searching.value) {
    recentQuery.value = ''
    return
  }
  recentsOpen.value = true
  await nextTick()
  searchInput.value?.focus()
}

const userName = computed(() => session.user?.display_name || session.user?.username || '')
const userNote = computed(() => (session.user?.is_admin ? '管理员' : session.user ? `@${session.user.username}` : ''))
</script>

<template>
  <div v-if="session.loaded && !session.meta" class="fatal">
    <img src="/logo.png" alt="BUUGOO" width="40" height="40" />
    <p>无法连接到{{ name }}的服务。请确认 buugoo serve 正在运行，然后刷新页面。</p>
  </div>
  <div v-else-if="inWorkspace" class="workspace" :class="{ 'has-side': wideScreen }">
    <aside v-if="wideScreen" class="side" :class="{ 'is-collapsed': sidebarCollapsed }" aria-label="主导航">
      <div class="side__head">
        <button
          type="button"
          class="side__icon-btn"
          :title="sidebarCollapsed ? '展开侧栏' : '收起侧栏'"
          :aria-label="sidebarCollapsed ? '展开侧栏' : '收起侧栏'"
          @click="sidebarCollapsed = !sidebarCollapsed"
        >
          <Icon name="sidebar" :size="18" />
        </button>
        <RouterLink v-if="!sidebarCollapsed" to="/" class="side__brand">{{ name }}</RouterLink>
      </div>
      <nav class="side__nav">
        <button type="button" class="side__item" title="新对话" @click="newChat">
          <span class="side__ico side__ico--new"><Icon name="plus" :size="14" /></span><span class="side__label">新对话</span>
        </button>
        <button type="button" class="side__item" :class="{ 'is-active': searching }" title="搜索对话" @click="toggleSearch">
          <span class="side__ico"><Icon name="search" :size="18" /></span><span class="side__label">搜索</span>
        </button>
        <RouterLink
          v-for="it in sideItems"
          :key="it.to"
          :to="it.to"
          class="side__item"
          :class="{ 'is-active': route.name === it.route }"
          :title="it.label"
        >
          <span class="side__ico">
            <Icon :name="it.icon" :size="18" />
            <span v-if="it.route === 'channels'" class="app-rail__dot" :class="{ 'is-on': linked > 0 }" />
          </span>
          <span class="side__label">{{ it.label }}</span>
        </RouterLink>
      </nav>
      <div v-show="!sidebarCollapsed" class="side__recents">
        <div v-if="searching" class="side__search">
          <Icon name="search" :size="15" />
          <input ref="searchInput" v-model="recentQuery" type="search" placeholder="搜索对话" aria-label="搜索对话" @keydown.esc="toggleSearch" />
        </div>
        <button type="button" class="side__sec" :aria-expanded="recentsOpen" @click="recentsOpen = !recentsOpen">
          最近 <Icon name="chevron" :size="11" class="side__caret" :class="{ 'is-closed': !recentsOpen }" />
        </button>
        <div v-show="recentsOpen" id="app-recents" class="side__list">
          <template v-if="!onChat">
            <RouterLink v-for="c in shownRecents" :key="c.id" :to="{ path: '/', query: { c: String(c.id) } }" class="side__conv">
              {{ c.title || '新对话' }}
            </RouterLink>
            <p v-if="!shownRecents.length" class="side__empty">{{ recentQuery ? '没有找到' : '还没有对话' }}</p>
          </template>
        </div>
      </div>
      <div class="side__foot">
        <RouterLink to="/profile" class="side__me" :class="{ 'is-active': route.name === 'profile' }" title="个人中心">
          <Avatar :seed="session.user?.avatar_seed ?? ''" :size="30" label="" />
          <span v-if="!sidebarCollapsed" class="side__me-text">
            <strong>{{ userName }}</strong>
            <small>{{ userNote }}</small>
          </span>
        </RouterLink>
        <RouterLink v-if="!sidebarCollapsed" to="/settings" class="side__icon-btn" :class="{ 'is-active': route.name === 'settings' }" title="设置" aria-label="设置">
          <Icon name="settings" :size="18" />
        </RouterLink>
      </div>
    </aside>
    <nav v-else class="app-rail" aria-label="主导航">
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
/* 桌面端侧栏（参考 claude.ai） */
.side {
  display: flex; width: 288px; flex: 0 0 288px; flex-direction: column; min-height: 0;
  border-right: 1px solid var(--color-divider); background: var(--color-side);
  transition: width .22s var(--ease-editorial), flex-basis .22s var(--ease-editorial);
}
.side.is-collapsed { width: 60px; flex-basis: 60px; }
.side__head { display: flex; align-items: center; gap: 6px; height: 60px; padding: 0 13px; }
.side__brand { color: var(--color-ink); font-family: var(--font-display); font-size: 22px; font-weight: 500; letter-spacing: -.01em; }
.side__icon-btn {
  display: inline-grid; flex-shrink: 0; place-items: center; width: 34px; height: 34px; border-radius: var(--radius-md);
  color: var(--color-text-secondary); transition: background-color .15s ease, color .15s ease;
}
.side__icon-btn:hover, .side__icon-btn.is-active { background: var(--color-hover-wash); color: var(--color-ink); }
.side__nav { display: grid; gap: 1px; padding: 2px 8px 8px; }
.side__item {
  display: flex; align-items: center; gap: 10px; width: 100%; height: 38px; padding: 0 10px; border-radius: var(--radius-md);
  color: var(--color-ink); font-size: 15px; text-align: left; white-space: nowrap; overflow: hidden;
  transition: background-color .15s ease;
}
.side__item:hover { background: var(--color-hover-wash); }
.side__item.is-active { background: rgba(20, 20, 19, .075); }
.side__ico { position: relative; display: inline-grid; flex: 0 0 20px; place-items: center; width: 20px; height: 20px; color: var(--color-text-secondary); }
.side__item.is-active .side__ico, .side__item:hover .side__ico { color: var(--color-ink); }
.side__ico--new { width: 22px; height: 22px; flex-basis: 22px; margin-left: -1px; border-radius: 50%; background: var(--color-ink); color: #fff !important; }
.side.is-collapsed .side__label { display: none; }
.side__recents { display: flex; flex: 1; min-height: 0; flex-direction: column; padding: 10px 8px 0; }
.side__search {
  display: flex; align-items: center; gap: 8px; height: 36px; margin: 0 2px 8px; padding: 0 10px;
  border: 1px solid var(--color-hairline-strong); border-radius: var(--radius-md); background: var(--color-surface); color: var(--color-muted);
}
.side__search input { flex: 1; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--color-ink); font-size: 14px; }
.side__sec {
  display: inline-flex; align-self: flex-start; align-items: center; gap: 4px; height: 28px; margin: 0 0 2px; padding: 0 10px;
  border-radius: var(--radius-sm); color: var(--color-muted); font-size: 13px;
}
.side__sec:hover { color: var(--color-text-secondary); }
.side__caret { transform: rotate(90deg); transition: transform .2s ease; }
.side__caret.is-closed { transform: rotate(0deg); }
.side__list { flex: 1; min-height: 0; overflow-y: auto; padding-bottom: 12px; }
.side__conv {
  display: block; height: 36px; padding: 0 10px; overflow: hidden; border-radius: var(--radius-md); color: var(--color-ink);
  font-size: 14.5px; line-height: 36px; white-space: nowrap; text-overflow: ellipsis;
}
.side__conv:hover { background: var(--color-hover-wash); }
.side__empty { margin: 4px 10px; color: var(--color-muted); font-size: 13px; }
.side__foot { display: flex; align-items: center; gap: 4px; padding: 10px 8px 12px; border-top: 1px solid var(--color-divider); }
.side.is-collapsed .side__foot { justify-content: center; }
.side__me { display: flex; flex: 1; min-width: 0; align-items: center; gap: 10px; padding: 6px 8px; border-radius: var(--radius-md); }
.side__me:hover, .side__me.is-active { background: var(--color-hover-wash); }
.side.is-collapsed .side__me { flex: 0 0 auto; padding: 6px; }
.side__me-text { display: grid; min-width: 0; line-height: 1.25; }
.side__me-text strong { overflow: hidden; color: var(--color-ink); font-size: 14px; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.side__me-text small { overflow: hidden; color: var(--color-muted); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
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
