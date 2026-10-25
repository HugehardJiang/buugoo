import { createRouter, createWebHistory } from 'vue-router'
import { setUnauthorizedHandler } from './api'
import { bootstrap, logout, needsLogin, session } from './session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'chat', component: () => import('./views/ChatView.vue') },
    { path: '/memories', name: 'memories', component: () => import('./views/MemoriesView.vue') },
    { path: '/settings', name: 'settings', component: () => import('./views/SettingsView.vue') },
    { path: '/console', name: 'console', component: () => import('./views/ConsoleView.vue') },
    { path: '/login', name: 'login', component: () => import('./views/LoginView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

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
  if (to.name !== 'login' && to.name !== 'console' && needsLogin()) return { name: 'login', query: { next: to.fullPath } }
  if (to.name === 'login' && !needsLogin()) return { name: 'chat' }
  return true
})

setUnauthorizedHandler(() => {
  if (!session.meta?.password_required) return
  logout()
  if (router.currentRoute.value.name !== 'login') router.push({ name: 'login' })
})
