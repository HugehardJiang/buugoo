import { ref } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import { setUnauthorizedHandler } from './api'
import { bootstrap, logout, needsLogin, session } from './session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'chat', component: () => import('./views/ChatView.vue') },
    { path: '/memories', name: 'memories', component: () => import('./views/MemoriesView.vue') },
    { path: '/memos', name: 'memos', component: () => import('./views/MemosView.vue') },
    { path: '/files', name: 'files', component: () => import('./views/FilesView.vue') },
    { path: '/tasks', name: 'tasks', component: () => import('./views/TasksView.vue') },
    { path: '/bgtasks', name: 'bgtasks', component: () => import('./views/BgTasksView.vue') },
    { path: '/channels', name: 'channels', component: () => import('./views/ChannelsView.vue') },
    { path: '/profile', name: 'profile', component: () => import('./views/ProfileView.vue') },
    { path: '/settings', name: 'settings', component: () => import('./views/SettingsView.vue') },
    { path: '/console', name: 'console', component: () => import('./views/ConsoleView.vue') },
    { path: '/welcome', name: 'welcome', component: () => import('./views/WelcomeView.vue') },
    { path: '/login', name: 'login', component: () => import('./views/LoginView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

// routeLoading 为 true 时页面顶部显示一根细进度线：切换页面要下载新页面的代码、超过 150 毫秒才显示，快的时候不闪。
export const routeLoading = ref(false)
let loadingTimer: ReturnType<typeof setTimeout> | undefined
router.beforeEach(() => {
  clearTimeout(loadingTimer)
  loadingTimer = setTimeout(() => (routeLoading.value = true), 150)
})
const stopLoading = () => {
  clearTimeout(loadingTimer)
  routeLoading.value = false
}
router.afterEach(stopLoading)
router.onError(stopLoading)

router.beforeEach(async (to) => {
  if (!session.loaded) {
    try {
      await bootstrap()
    } catch {
      // 服务不可达时仍渲染页面，由 App 显示错误。
      return true
    }
  }
  // 管理后台用管理员密码单独验证，不需要先登录用户账号。
  const multi = session.meta?.mode === 'multi'
  if (to.name === 'welcome') return multi && needsLogin() ? true : { name: 'chat' }
  if (to.name !== 'login' && to.name !== 'console' && needsLogin()) {
    // 多用户版直接打开首页时先看介绍主页，其他页面直接去登录（登录后回到原来的页面）。
    if (multi && to.name === 'chat') return { name: 'welcome' }
    return { name: 'login', query: { next: to.fullPath } }
  }
  if (to.name === 'login' && !needsLogin()) return { name: 'chat' }
  // 多用户版里已经登录的普通用户看不到管理后台（没登录时仍可以用管理员密码进入，方便第一次配置）。
  if (to.name === 'console' && multi && session.user && !session.user.is_admin) return { name: 'chat' }
  return true
})

setUnauthorizedHandler(() => {
  if (!session.meta?.password_required) return
  logout()
  if (router.currentRoute.value.name !== 'login') router.push({ name: 'login' })
})
