// 布谷 HTTP API 客户端。

export interface User {
  id: number
  username: string
  display_name: string
  is_admin: boolean
  created_at: string
}

export interface Session {
  token: string
  expires_at: string
  user: User
}

export interface Meta {
  name: string
  version: string
  mode: 'single' | 'multi'
  password_required: boolean
  registration: 'open' | 'invite' | 'closed'
  llm_ready: boolean
  embedding_ready: boolean
}

export interface Quota {
  day: string
  messages: number
  tokens: number
  messages_limit: number
  tokens_limit: number
  exempt: boolean
}

export interface Message {
  id: number
  role: 'user' | 'assistant'
  content: string
  status: 'complete' | 'interrupted' | 'error'
  created_at: string
}

export interface ScoreBreakdown {
  recency: number
  importance: number
  relevance: number
  total: number
}

export interface UsedMemory {
  id: number
  content: string
  source: 'stated' | 'inferred'
  score: ScoreBreakdown
}

export interface Memory {
  id: number
  type: 'episodic' | 'semantic'
  content: string
  importance: number
  emotion: number
  strength: number
  source: 'stated' | 'inferred'
  access_count: number
  created_at: string
  last_accessed_at: string | null
}

export interface Invite {
  id: number
  code: string
  max_uses: number
  used_count: number
  expires_at: string | null
  note: string
  created_at: string
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

const TOKEN_KEY = 'buugoo.token'

export const tokenStore = {
  get(): string {
    try {
      return localStorage.getItem(TOKEN_KEY) ?? ''
    } catch {
      return ''
    }
  },
  set(token: string) {
    try {
      if (token) localStorage.setItem(TOKEN_KEY, token)
      else localStorage.removeItem(TOKEN_KEY)
    } catch {
      // 无痕模式等情况下无法持久化，仅本次会话有效。
    }
  },
}

let onUnauthorized: () => void = () => {}

// setUnauthorizedHandler 设置令牌失效时的回调（跳转登录页）。
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

function headers(json: boolean): HeadersInit {
  const h: Record<string, string> = {}
  if (json) h['Content-Type'] = 'application/json'
  const t = tokenStore.get()
  if (t) h['Authorization'] = `Bearer ${t}`
  return h
}

async function toError(resp: Response): Promise<ApiError> {
  let code = 'http_error'
  let message = `请求失败（HTTP ${resp.status}）`
  try {
    const body = await resp.json()
    if (body?.error) {
      code = body.error.code ?? code
      message = body.error.message ?? message
    }
  } catch {
    // 非 JSON 响应（例如反向代理返回的错误页）。
  }
  if (resp.status === 401) onUnauthorized()
  return new ApiError(resp.status, code, message)
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let resp: Response
  try {
    resp = await fetch(`/api/v1${path}`, {
      method,
      headers: headers(body !== undefined),
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'network', '无法连接到服务器，请检查网络')
  }
  if (!resp.ok) throw await toError(resp)
  if (resp.status === 204) return undefined as T
  return (await resp.json()) as T
}

export const api = {
  meta: () => request<Meta>('GET', '/meta'),
  login: (username: string, password: string) =>
    request<Session>('POST', '/auth/login', { username, password }),
  register: (input: { username: string; password: string; display_name?: string; invite_code?: string }) =>
    request<Session>('POST', '/auth/register', input),
  logoutAll: () => request<void>('POST', '/auth/logout-all'),
  changePassword: (old_password: string, new_password: string) =>
    request<Session>('POST', '/auth/password', { old_password, new_password }),
  me: () => request<{ user: User; quota: Quota }>('GET', '/me'),
  messages: (beforeId = 0, limit = 50) =>
    request<{ conversation_id: number; messages: Message[]; has_more: boolean }>(
      'GET',
      `/messages?limit=${limit}${beforeId ? `&before_id=${beforeId}` : ''}`,
    ),
  memories: (params: { type?: string; source?: string; q?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') qs.set(k, String(v))
    return request<{ memories: Memory[]; total: number }>('GET', `/memories?${qs}`)
  },
  deleteMemory: (id: number) => request<void>('DELETE', `/memories/${id}`),
  invites: () => request<{ invites: Invite[] }>('GET', '/admin/invites'),
  createInvite: (input: { max_uses: number; ttl_hours: number; note: string }) =>
    request<Invite>('POST', '/admin/invites', input),
  deleteInvite: (id: number) => request<void>('DELETE', `/admin/invites/${id}`),
}

export interface ChatHandlers {
  onStart?: (data: { conversation_id: number; user_message: Message; assistant_message_id: number }) => void
  onMemories?: (memories: UsedMemory[]) => void
  onDelta: (text: string) => void
}

// sendChat 发送消息并解析 SSE 事件流。正常结束时返回最终的助手消息。
export async function sendChat(content: string, h: ChatHandlers, signal?: AbortSignal): Promise<Message> {
  let resp: Response
  try {
    resp = await fetch('/api/v1/chat', {
      method: 'POST',
      headers: headers(true),
      body: JSON.stringify({ content, stream: true }),
      signal,
    })
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e
    throw new ApiError(0, 'network', '无法连接到服务器，请检查网络')
  }
  if (!resp.ok) throw await toError(resp)
  if (!resp.body) throw new ApiError(0, 'stream', '浏览器不支持流式响应')

  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  let final: Message | null = null
  for (;;) {
    const { value, done } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    let idx: number
    while ((idx = buf.indexOf('\n\n')) >= 0) {
      const raw = buf.slice(0, idx)
      buf = buf.slice(idx + 2)
      let event = 'message'
      const data: string[] = []
      for (const line of raw.split('\n')) {
        if (line.startsWith(':')) continue
        if (line.startsWith('event:')) event = line.slice(6).trim()
        else if (line.startsWith('data:')) data.push(line.slice(5).trimStart())
      }
      if (data.length === 0) continue
      const payload = JSON.parse(data.join('\n'))
      switch (event) {
        case 'start':
          h.onStart?.(payload)
          break
        case 'memories':
          h.onMemories?.(payload.memories ?? [])
          break
        case 'delta':
          h.onDelta(payload.text)
          break
        case 'done':
          final = payload.message
          break
        case 'error':
          throw new ApiError(502, payload.error?.code ?? 'error', payload.error?.message ?? '生成失败')
      }
    }
  }
  if (!final) throw new ApiError(0, 'stream', '连接意外中断，回复可能不完整')
  return final
}
