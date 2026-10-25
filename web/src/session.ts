// 全局会话状态（站点信息、当前用户、登录态）。
import { reactive } from 'vue'
import { api, tokenStore, type Meta, type Session, type User } from './api'

export const session = reactive({
  meta: null as Meta | null,
  user: null as User | null,
  loaded: false,
})

// needsLogin 报告当前是否需要先登录。
export function needsLogin(): boolean {
  if (!session.meta) return true
  if (!session.meta.password_required) return false
  return !session.user
}

// bootstrap 读取站点信息，并尝试用已保存的令牌恢复登录。
export async function bootstrap() {
  session.meta = await api.meta()
  if (!session.meta.password_required || tokenStore.get()) {
    try {
      session.user = (await api.me()).user
    } catch {
      tokenStore.set('')
      session.user = null
    }
  }
  session.loaded = true
}

export function applySession(s: Session) {
  tokenStore.set(s.token)
  session.user = s.user
}

export function logout() {
  tokenStore.set('')
  session.user = null
}
