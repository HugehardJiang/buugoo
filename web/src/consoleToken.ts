// 管理后台的登录：存在 localStorage 里，同一个浏览器的所有标签页共用（牛马助手据此决定能不能改系统设置）。
// 服务器端 8 小时后过期，退出管理后台时作废。
const KEY = 'buugoo.console'

export function getConsoleToken(): string {
  try {
    // 旧版本存在 sessionStorage 里，读到就搬过来。
    const old = sessionStorage.getItem(KEY)
    if (old) {
      sessionStorage.removeItem(KEY)
      localStorage.setItem(KEY, old)
    }
    return localStorage.getItem(KEY) ?? ''
  } catch {
    return ''
  }
}

export function setConsoleToken(t: string) {
  try {
    if (t) localStorage.setItem(KEY, t)
    else localStorage.removeItem(KEY)
  } catch {
    // 无痕模式下只在本页有效。
  }
}

// consoleLogin 用管理员密码登录管理后台，返回令牌（失败时抛出带中文说明的错误）。
export async function consoleLogin(password: string): Promise<string> {
  const resp = await fetch('/api/v1/console/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  const j = await resp.json().catch(() => null)
  if (!resp.ok) throw new Error(j?.error?.message ?? `登录失败（HTTP ${resp.status}）`)
  setConsoleToken(j.token)
  return j.token as string
}
