// 诊断信息：记下最近的请求错误，出问题时一键复制「诊断信息」发给管理员或开发者。
// 内容包括版本、时间、页面、最近的错误（带请求编号），以及服务器上和这个账号有关的最近警告和错误（密钥已打码）。
// 不包含聊天内容和任何令牌。
import { toast } from './toast'

export interface ClientError {
  time: string
  status: number
  code: string
  message: string
  requestId: string
  path: string
}

const errors: ClientError[] = []

export function recordError(e: ClientError) {
  errors.push(e)
  if (errors.length > 20) errors.shift()
}

function line(k: string, v: unknown) {
  return `${k}: ${v === undefined || v === null || v === '' ? '—' : String(v)}`
}

// buildDiagnostics 拼出诊断信息文本。context 是出错的地方补充的信息（比如哪条消息、什么错误）。
export async function buildDiagnostics(context: Record<string, unknown> = {}): Promise<string> {
  const out: string[] = ['==== 布谷诊断信息 ====', line('时间', new Date().toLocaleString()), line('页面', location.pathname + location.search),
    line('浏览器', navigator.userAgent)]
  const ctxKeys = Object.keys(context)
  if (ctxKeys.length) {
    out.push('', '-- 出错的地方 --')
    for (const k of ctxKeys) out.push(line(k, context[k]))
  }
  if (errors.length) {
    out.push('', '-- 最近的请求错误 --')
    for (const e of errors.slice(-10)) {
      out.push(`${e.time}  ${e.path || '?'}  HTTP ${e.status || '连不上'}  ${e.code}  ${e.message}${e.requestId ? `  [请求 ${e.requestId}]` : ''}`)
    }
  }
  // 动态引入，避免和 api.ts 互相引用。
  const { api } = await import('./api')
  try {
    const d = await api.diagnostics()
    out.push('', '-- 服务器 --')
    for (const [k, v] of Object.entries(d.server)) out.push(line(k, v))
    out.push('', `-- 服务器上这个账号最近的警告和错误（${d.recent.length} 条）--`)
    for (const e of d.recent) {
      const attrs = Object.entries(e.attrs ?? {})
        .filter(([k]) => k !== 'user_id')
        .map(([k, v]) => `${k}=${v}`)
        .join(' ')
      out.push(`${e.time}  ${e.level}  ${e.msg}  ${attrs}`)
    }
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    out.push('', '-- 服务器 --', `读取服务器信息失败：${msg}`,
      '如果服务器没有响应，请在服务器上执行下面的命令，把输出一起发过来：',
      '  sudo systemctl status buugoo --no-pager',
      '  sudo journalctl -u buugoo -n 100 --no-pager')
  }
  out.push('==== 结束 ====')
  return out.join('\n')
}

// copyDiagnostics 复制诊断信息到剪贴板（复制不了时下载成文件）。
export async function copyDiagnostics(context: Record<string, unknown> = {}) {
  const text = await buildDiagnostics(context)
  try {
    await navigator.clipboard.writeText(text)
    toast('诊断信息已复制，可以直接粘贴发给管理员')
  } catch {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `布谷诊断-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '')}.txt`
    a.click()
    URL.revokeObjectURL(url)
    toast('浏览器不让复制，已经下载成文件')
  }
}
