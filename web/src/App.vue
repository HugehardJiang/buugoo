<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from './Icon.vue'
import { api } from './api'
import { connectedCount, refreshChannels } from './channelState'
import { activeConvId, navOpen, openConvHandler, recentConvs, recentQuery, searching, sidebarCollapsed, wideScreen } from './sidebarState'
import Avatar from './components/Avatar.vue'
import Lightbox from './components/Lightbox.vue'
import { memoriesUnlocked } from './memoryLock'
import { logout, needsLogin, session } from './session'
import { toasts } from './toast'
import { routeLoading } from './router'

// 强制通知弹窗要用 Markdown 渲染，单独加载，不拖慢首屏。
const NoticeGate = defineAsyncComponent(() => import('./components/NoticeGate.vue'))

const route = useRoute()
const router = useRouter()
const inWorkspace = computed(
  () =>
    route.name !== 'login' &&
    route.name !== 'welcome' &&
    route.name !== 'posts' &&
    route.name !== 'post-detail' &&
    session.loaded &&
    !!session.meta &&
    !(route.name === 'console' && needsLogin()),
)
const name = computed(() => session.meta?.name ?? '布谷')

// 「大脑皮层」（记忆与情绪）默认藏起来，用户在设置里确认翻开后才出现在导航中。设置固定在最下面。
// 手机底栏只放常用的几个（聊天、待办、备忘录、我的），其余收进「更多」。short 是手机底栏上的短名字。
type NavItem = { to: string; route: string; label: string; short?: string; icon: 'message' | 'clock' | 'note' | 'brain' | 'folder' | 'link' | 'grid'; more?: boolean }
const items = computed<NavItem[]>(() => [
  { to: '/', route: 'chat', label: '聊天', icon: 'message' },
  { to: '/tasks', route: 'tasks', label: session.meta?.reminders ? '提醒与待办' : '待办', short: '待办', icon: 'clock' },
  { to: '/memos', route: 'memos', label: '备忘录', icon: 'note' },
  ...(session.meta?.bg_tasks ? [{ to: '/bgtasks', route: 'bgtasks', label: '后台任务', short: '任务', icon: 'grid' as const, more: true }] : []),
  ...(memoriesUnlocked.value ? [{ to: '/memories', route: 'memories', label: '大脑皮层', icon: 'brain' as const, more: true }] : []),
  ...(session.meta?.uploads ? [{ to: '/files', route: 'files', label: '文件', icon: 'folder' as const, more: true }] : []),
  ...(session.meta?.channels ? [{ to: '/channels', route: 'channels', label: '聊天软件', icon: 'link' as const, more: true }] : []),
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

// 桌面端侧栏（参考 claude.ai）：上面是功能入口，中间是最近的对话，最下面是自己。
// 聊天页会把自己的对话列表放进 #app-recents；其他页面在这里读一份最近的对话。
const sideItems = computed(() => items.value.filter((it) => it.route !== 'chat'))
const recentsOpen = ref(true)
// 只有桌面端能收起成窄栏；手机上的抽屉总是完整的。
const collapsed = computed(() => wideScreen.value && sidebarCollapsed.value)
const searchInput = ref<HTMLInputElement | null>(null)
const onChat = computed(() => route.name === 'chat')
// 「最近」分成网页上的对话和聊天软件里的对话（微信、QQ、Telegram），每个页面显示的都一样。
const channelLabels: Record<string, string> = { weixin: '微信', qq: 'QQ', telegram: 'Telegram' }
const filteredConvs = computed(() => {
  const q = recentQuery.value.trim().toLowerCase()
  return recentConvs.value.filter((c) => !q || convTitle(c).toLowerCase().includes(q))
})
// 聊天软件里的对话放在「最近」上面（像 claude.ai 的「已置顶」），不用翻到底才看得到。
const extConvs = computed(() => filteredConvs.value.filter((c) => !!c.channel))
const webConvs = computed(() => filteredConvs.value.filter((c) => !c.channel))
const extOpen = ref(true)
function convTitle(c: { title: string; channel?: string; character?: { name: string } }) {
  return c.title || (c.channel ? `${channelLabels[c.channel]}里的聊天` : c.character ? `和${c.character.name}聊天` : '新对话')
}
function openConv(id: number) {
  navOpen.value = false
  if (onChat.value && openConvHandler.fn) openConvHandler.fn(id)
  else router.push({ path: '/', query: { c: String(id) } })
}
watch(
  () => inWorkspace.value && wideScreen.value && !onChat.value && !needsLogin(),
  async (on) => {
    if (!on) return
    try {
      recentConvs.value = (await api.conversations()).conversations
    } catch {
      // 只是侧栏的列表，读不到就空着。
    }
  },
  { immediate: true },
)

// 手机上点了导航里的东西就收起抽屉。
watch(() => route.fullPath, () => (navOpen.value = false))

// 结束临时登录：只清掉这个标签页的临时令牌，关掉标签页（打不开就回首页）。
function endImpersonation() {
  logout()
  window.close()
  location.href = '/'
}

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
  <div v-if="routeLoading" class="route-bar" role="progressbar" aria-label="正在打开页面" />
  <!-- 管理员临时登录到别人账号时一直显示，免得忘了自己在用谁的账号 -->
  <div v-if="session.impersonated && session.user" class="imp-bar" role="status">
    <span>正在临时登录「{{ userName }}」的账号排查问题 · 操作和本人一样生效</span>
    <button type="button" @click="endImpersonation">结束临时登录</button>
  </div>
  <div v-if="session.loaded && !session.meta" class="fatal">
    <img src="/logo.png" alt="BUUGOO" width="40" height="40" />
    <p>无法连接到{{ name }}的服务。请确认 buugoo serve 正在运行，然后刷新页面。</p>
  </div>
  <div v-else-if="inWorkspace" class="workspace" :class="{ 'has-side': wideScreen, 'has-imp': session.impersonated }">
    <!-- 手机和窄屏：侧栏变成从左边滑出来的抽屉（和 claude.ai 手机版一样），左上角的按钮打开 -->
    <button v-if="!wideScreen" type="button" class="nav-toggle" aria-label="打开导航" @click="navOpen = true"><Icon name="menu" /></button>
    <Transition name="fade">
      <div v-if="!wideScreen && navOpen" class="side-backdrop" @click="navOpen = false" />
    </Transition>
    <aside
      class="side"
      :class="{ 'is-collapsed': wideScreen && sidebarCollapsed, 'is-drawer': !wideScreen, 'is-open': !wideScreen && navOpen }"
      aria-label="主导航"
    >
      <div class="side__head">
        <RouterLink v-if="!collapsed" to="/" class="side__brand">
          <img src="/logo.png" alt="" width="20" height="20" class="side__logo" />
          <span>{{ name }}</span>
        </RouterLink>
        <div class="side__head-actions">
          <button
            v-if="!collapsed"
            type="button"
            class="side__icon-btn"
            :class="{ 'is-active': searching }"
            title="搜索对话"
            aria-label="搜索对话"
            @click="toggleSearch"
          >
            <Icon name="search" :size="17" />
          </button>
          <button
            v-if="wideScreen"
            type="button"
            class="side__icon-btn"
            :title="sidebarCollapsed ? '展开侧栏' : '收起侧栏'"
            :aria-label="sidebarCollapsed ? '展开侧栏' : '收起侧栏'"
            @click="sidebarCollapsed = !sidebarCollapsed"
          >
            <Icon name="sidebar" :size="17" />
          </button>
          <button v-if="!wideScreen" type="button" class="side__icon-btn" aria-label="关闭导航" @click="navOpen = false"><Icon name="x" :size="17" /></button>
        </div>
      </div>
      <nav class="side__nav">
        <button type="button" class="side__item side__item--new" title="新对话" @click="newChat">
          <span class="side__ico"><Icon name="edit" :size="17" /></span><span class="side__label">新对话</span>
        </button>
        <button v-if="collapsed" type="button" class="side__item" :class="{ 'is-active': searching }" title="搜索对话" @click="toggleSearch">
          <span class="side__ico"><Icon name="search" :size="17" /></span><span class="side__label">搜索</span>
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
            <Icon :name="it.icon" :size="17" />
            <span v-if="it.route === 'channels'" class="app-rail__dot" :class="{ 'is-on': linked > 0 }" />
          </span>
          <span class="side__label">{{ it.label }}</span>
        </RouterLink>
      </nav>
      <div v-show="!collapsed" class="side__recents">
        <!-- 搜索框平滑地展开、收起（高度和透明度一起过渡） -->
        <div class="side__search-wrap" :class="{ 'is-open': searching }" :aria-hidden="!searching">
          <div class="side__search-inner">
            <div class="side__search">
              <Icon name="search" :size="15" />
              <input
                ref="searchInput"
                v-model="recentQuery"
                type="search"
                placeholder="搜索对话"
                aria-label="搜索对话"
                :tabindex="searching ? 0 : -1"
                @keydown.esc="toggleSearch"
              />
            </div>
          </div>
        </div>
        <div class="side__scroll">
          <template v-if="extConvs.length">
            <button type="button" class="side__sec" :aria-expanded="extOpen" @click="extOpen = !extOpen">
              外部聊天 <Icon name="chevron" :size="11" class="side__caret" :class="{ 'is-closed': !extOpen }" />
            </button>
            <div v-show="extOpen" class="side__group">
              <button
                v-for="c in extConvs"
                :key="c.id"
                type="button"
                class="side__conv"
                :class="{ 'is-active': onChat && c.id === activeConvId }"
                :title="convTitle(c)"
                @click="openConv(c.id)"
              >
                <i class="side__tag" :class="`side__tag--${c.channel}`">{{ channelLabels[c.channel!] }}</i>{{ convTitle(c) }}
              </button>
            </div>
          </template>
          <button type="button" class="side__sec" :aria-expanded="recentsOpen" @click="recentsOpen = !recentsOpen">
            最近 <Icon name="chevron" :size="11" class="side__caret" :class="{ 'is-closed': !recentsOpen }" />
          </button>
          <div v-show="recentsOpen" class="side__group">
            <button
              v-for="c in webConvs"
              :key="c.id"
              type="button"
              class="side__conv"
              :class="{ 'is-active': onChat && c.id === activeConvId }"
              :title="c.character ? `Cosplay · ${c.character.name}：${convTitle(c)}` : convTitle(c)"
              @click="openConv(c.id)"
            >
              <Icon v-if="c.character" name="mask" :size="13" class="side__mask" />{{ convTitle(c) }}
            </button>
            <p v-if="!webConvs.length" class="side__empty">{{ recentQuery ? '没有找到' : '还没有对话' }}</p>
          </div>
        </div>
      </div>
      <div class="side__foot">
        <RouterLink to="/profile" class="side__me" :class="{ 'is-active': route.name === 'profile' }" title="个人中心">
          <Avatar :seed="session.user?.avatar_seed ?? ''" :size="28" label="" />
          <span v-if="!collapsed" class="side__me-text">
            <strong>{{ userName }}</strong>
            <small>{{ userNote }}</small>
          </span>
        </RouterLink>
        <RouterLink v-if="!collapsed" to="/settings" class="side__icon-btn" :class="{ 'is-active': route.name === 'settings' }" title="设置" aria-label="设置">
          <Icon name="settings" :size="17" />
        </RouterLink>
      </div>
    </aside>
    <RouterView v-slot="{ Component }">
      <component :is="Component" :key="route.name" />
    </RouterView>
  </div>
  <RouterView v-else />

  <!-- 强制弹窗通知（管理员临时登录排查问题时不弹） -->
  <NoticeGate v-if="inWorkspace && session.user && !session.impersonated && !needsLogin()" :key="session.user.id" />
  <Lightbox />
  <TransitionGroup tag="div" name="toast" class="toast-stack" aria-live="polite">
    <div v-for="t in toasts" :key="t.id" class="toast">{{ t.text }}</div>
  </TransitionGroup>
</template>

<style scoped>
/* 桌面端侧栏：参考 Gemini 高密度 M3 Navigation Drawer（紧凑行高 + 右置搜索与折叠） */
.side {
  display: flex; width: 262px; flex: 0 0 262px; flex-direction: column; min-height: 0;
  border-right: 0; background: var(--md-surface-container-low);
  transition: width .24s var(--md-ease), flex-basis .24s var(--md-ease);
}
.side.is-collapsed { width: 64px; flex-basis: 64px; }
.side__head {
  display: flex; align-items: center; gap: 6px; height: 52px; padding: 0 10px 0 14px;
  border-bottom: 0;
}
.side__head-actions { display: flex; align-items: center; gap: 2px; margin-left: auto; }
.side.is-collapsed .side__head-actions { margin-left: 0; }
.side__brand {
  display: inline-flex; align-items: center; gap: 8px; min-width: 0;
  color: var(--color-ink); font-family: var(--font-sans); font-size: 16.5px; font-weight: 500; letter-spacing: -.02em;
}
.side__brand span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.side__logo { width: 20px; height: 20px; flex: 0 0 20px; object-fit: contain; }
.side__icon-btn {
  display: inline-grid; flex-shrink: 0; place-items: center; width: 34px; height: 34px; border-radius: 9999px;
  color: var(--color-text-secondary); transition: background-color .16s ease, color .16s ease;
}
.side__icon-btn:hover, .side__icon-btn.is-active { background: var(--md-state-hover); color: var(--color-ink); }
.side__nav { display: flex; flex-direction: column; gap: 1px; padding: 2px 10px 6px; }

/* M3 Active Indicator Pill：紧凑高密度主导航项 + 新对话 */
.side__item {
  display: flex; align-items: center; gap: 11px; width: 100%; height: 35px; padding: 0 12px; border-radius: 9999px;
  color: var(--color-text-secondary); font-size: 13.5px; font-weight: 500; text-align: left; white-space: nowrap; overflow: hidden;
  transition: background-color .16s var(--md-ease), color .16s ease;
}
.side__item--new { color: var(--color-ink); font-weight: 500; }
.side__item:hover { background: var(--md-state-hover); color: var(--color-ink); }
.side__item.is-active {
  background: var(--md-primary-container); color: var(--md-on-primary-container); font-weight: 600;
  box-shadow: none;
}
.side__ico { position: relative; display: inline-grid; flex: 0 0 18px; place-items: center; width: 18px; height: 18px; color: inherit; }
.side.is-collapsed .side__head { justify-content: center; padding: 0; }
.side.is-collapsed .side__nav { align-items: center; padding: 2px 8px 6px; }
.side.is-collapsed .side__item { justify-content: center; width: 42px; height: 38px; padding: 0; }
.side.is-collapsed .side__label { display: none; }

.side__recents {
  display: flex; flex: 1; min-height: 0; flex-direction: column; padding: 6px 10px 0;
  border-top: 0;
}
.side__search-wrap {
  display: grid; grid-template-rows: 0fr; opacity: 0; transform: translateY(-4px);
  transition: grid-template-rows .26s var(--md-ease), opacity .2s ease, transform .26s var(--md-ease);
}
.side__search-wrap.is-open { grid-template-rows: 1fr; opacity: 1; transform: none; }
.side__search-inner { min-height: 0; overflow: hidden; }
/* M3 填充胶囊搜索框 */
.side__search {
  display: flex; align-items: center; gap: 8px; height: 36px; margin: 2px 0 8px; padding: 0 12px;
  border: 0; border-radius: 9999px; background: var(--md-surface-container-high); color: var(--color-muted);
  transition: box-shadow .18s var(--md-ease), background-color .18s ease, color .18s ease;
}
.side__search:focus-within {
  background: var(--md-surface-container-lowest); color: var(--color-ink); box-shadow: var(--md-elev-1), 0 0 0 2px rgba(91, 122, 74, .22);
}
.side__search input { flex: 1; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--color-ink); font-size: 13px; }
.side__search input::placeholder { color: var(--color-tertiary); }
.side__search input::-webkit-search-cancel-button { display: none; }
.side__sec {
  display: inline-flex; align-self: flex-start; align-items: center; gap: 4px; height: 24px; margin: 4px 0 2px; padding: 0 12px;
  border-radius: 9999px; color: var(--color-muted); font-size: 12px; font-weight: 500; letter-spacing: .01em;
  transition: color .16s ease, background-color .16s ease;
}
.side__sec:hover { color: var(--color-ink); background: var(--md-state-hover); }
.side__caret { transform: rotate(90deg); transition: transform .2s var(--md-ease); }
.side__caret.is-closed { transform: rotate(0deg); }
.side__scroll { flex: 1; min-height: 0; overflow-y: auto; padding-bottom: 10px; }
.side__group { display: grid; gap: 1px; margin-bottom: 8px; }
.side__conv {
  display: block; width: 100%; height: 32px; padding: 0 12px; overflow: hidden; border-radius: 9999px; color: var(--color-ink);
  font-size: 13.5px; line-height: 32px; text-align: left; white-space: nowrap; text-overflow: ellipsis;
  transition: background-color .16s var(--md-ease), color .16s ease;
}
.side__conv:hover { background: var(--md-state-hover); }
.side__conv.is-active {
  background: var(--md-primary-container); color: var(--md-on-primary-container); font-weight: 600;
  box-shadow: none;
}
.side__tag {
  display: inline-block; margin-right: 6px; padding: 0 6px; border-radius: 9999px;
  background: var(--md-secondary-container); color: var(--md-on-secondary-container); font-size: 10.5px; font-style: normal; line-height: 16px; vertical-align: 1px;
}
.side__mask { margin-right: 5px; vertical-align: -2px; color: var(--color-accent-active); }
.side__tag--qq { background: #dbeafe; color: #1d4ed8; }
.side__tag--telegram { background: #e0f2fe; color: #0369a1; }
.side__empty { margin: 4px 12px; color: var(--color-muted); font-size: 12.5px; }
.side__foot {
  display: flex; align-items: center; gap: 6px; padding: 8px 10px 10px;
  border-top: 0;
}
.side.is-collapsed .side__foot { justify-content: center; }
.side__me {
  display: flex; flex: 1; min-width: 0; align-items: center; gap: 9px; padding: 5px 8px; border-radius: 9999px;
  transition: background-color .16s var(--md-ease);
}
.side__me:hover { background: var(--md-state-hover); }
.side__me.is-active {
  background: var(--md-primary-container); color: var(--md-on-primary-container);
  box-shadow: none;
}
.side.is-collapsed .side__me { flex: 0 0 auto; padding: 5px; }
.side__me-text { display: grid; min-width: 0; line-height: 1.25; }
.side__me-text strong { overflow: hidden; color: var(--color-ink); font-size: 13px; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.side__me.is-active .side__me-text strong { color: var(--md-on-primary-container); font-weight: 600; }
.side__me-text small { overflow: hidden; color: var(--color-muted); font-size: 11.5px; text-overflow: ellipsis; white-space: nowrap; }

/* 手机和窄屏：M3 Modal Navigation Drawer（右侧大圆角，无框悬浮圆钮打开） */
.nav-toggle {
  position: fixed; top: 9px; left: 10px; z-index: 20; display: inline-grid; place-items: center; width: 36px; height: 36px;
  border-radius: 9999px; border: 0; background: rgba(235, 232, 222, .9);
  -webkit-backdrop-filter: blur(10px); backdrop-filter: blur(10px); color: var(--color-ink);
  box-shadow: var(--md-elev-1);
  transition: background-color .16s ease, box-shadow .16s ease;
}
.nav-toggle:hover { background: var(--md-surface-container-high); box-shadow: var(--md-elev-2); }
.nav-toggle:active { background: var(--md-state-pressed); }
.side.is-drawer {
  position: fixed; top: 0; bottom: 0; left: 0; z-index: 40; width: min(296px, 84vw); flex: none;
  border-right: 0; border-radius: 0 28px 28px 0; box-shadow: none;
  padding-bottom: env(safe-area-inset-bottom);
  transform: translateX(-102%); transition: transform .28s var(--md-ease), box-shadow .28s ease;
}
.side.is-drawer.is-open { transform: none; box-shadow: var(--md-elev-3); }
.side.is-drawer .side__item { height: 40px; font-size: 14.5px; }
.side.is-drawer .side__conv { height: 38px; line-height: 38px; font-size: 14.5px; }
.side-backdrop {
  position: fixed; inset: 0; z-index: 39; background: var(--color-backdrop);
  -webkit-backdrop-filter: blur(6px); backdrop-filter: blur(6px);
}
.imp-bar {
  position: fixed; top: 0; left: 0; right: 0; z-index: 70; display: flex; align-items: center; justify-content: center; gap: 12px;
  height: 32px; padding: 0 12px; background: #f3e3c3; color: #6b4a12; font-size: 13px; white-space: nowrap; overflow: hidden;
}
.imp-bar span { overflow: hidden; text-overflow: ellipsis; }
.imp-bar button { flex-shrink: 0; padding: 2px 10px; border: 1px solid rgba(107, 74, 18, .3); border-radius: var(--radius-pill); color: inherit; font-size: 12px; }
.imp-bar button:hover { background: rgba(107, 74, 18, .08); }
.fatal { max-width: 420px; margin: 18vh auto 0; padding: 0 16px; text-align: center; color: var(--color-text-secondary); }
</style>
