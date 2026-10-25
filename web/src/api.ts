// 布谷 HTTP API 客户端。

export interface User {
  id: number
  username: string
  display_name: string
  is_admin: boolean
  proactive: boolean
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
  embedding_ready: boolean
  // any_file：能运行代码时，表格、压缩包等读不出文字的文件也可以上传
  uploads: { max_file_bytes: number; max_files: number; any_file?: boolean } | null
  reminders?: boolean
  channels?: boolean
}

export interface Attachment {
  id: number
  // file 是读不出文字的其他文件（表格、压缩包等），只能下载或交给代码处理
  kind: 'image' | 'document' | 'file'
  filename: string
  mime: string
  size: number
  // 已经在文件管理中删除（聊天记录里显示占位）
  deleted?: boolean
}

// FileItem 是文件管理中的一个文件：用户上传的，或对话中代码生成的图片。
export interface FileItem extends Attachment {
  source: 'upload' | 'generated'
  created_at: string
  conversation_id: number | null
  conversation_title: string
}

export interface Usage {
  used_bytes: number
  // 其中运行代码保存在对话工作目录里的文件（删除对话时一起删除）
  workspace_bytes: number
  quota_bytes: number // 0 表示不限
  full: boolean
}

export type Repeat = '' | 'daily' | 'weekdays' | 'weekly' | 'monthly' | 'interval' | 'cron'

// Reminder 是提醒（到点提醒一句）或定时任务（到点去做一件事，再把结果发过来）。
export interface Reminder {
  id: number
  kind: 'remind' | 'task'
  content: string
  due_at: string
  time: string // 下次时间的本地写法 YYYY-MM-DD HH:MM
  repeat: Repeat
  every_minutes: number
  cron: string
  repeat_label: string
  paused: boolean
}

export interface ReminderInput {
  kind?: 'remind' | 'task'
  content: string
  time?: string
  repeat: Repeat | 'none'
  every_minutes?: number
  cron?: string
}

export interface WeixinLogin {
  qrcode?: string
  status: 'wait' | 'scaned' | 'confirmed' | 'expired' | 'error'
  error?: string
}

export interface WeixinAccount {
  user_id: number
  account: string
  login_at: string
  error?: string
  legacy?: boolean
}

// WeixinUser 是自己的微信连接情况。
export interface WeixinUser {
  account: WeixinAccount | null
  login: WeixinLogin | null
  notice?: string
}

export interface ChannelInfo {
  name: 'weixin' | 'qq' | 'telegram'
  label: string
  account?: string
  connected: boolean
}

export interface Binding {
  id: number
  channel: string
  label: string
  peer_name: string
  created_at: string
  last_message_at: string | null
}

export interface PersonaPreset {
  id: string // 空字符串为默认
  label: string
  summary: string
  catchphrases: string[]
}

export interface ToolEvent {
  id: string
  name: string
  label: string
  status: 'running' | 'done' | 'error'
  input?: string
  summary?: string
  detail?: string
  // 代码生成的图片（已保存为附件）
  images?: Attachment[]
  // 发给用户的文件（已保存为附件）
  files?: Attachment[]
}

export type Thinking = 'off' | 'low' | 'medium' | 'high'
export type ThinkingStyle = 'none' | 'deepseek' | 'reasoning_effort' | 'enable_thinking' | 'model_switch'

export interface Conversation {
  id: number
  title: string
  model_id: number | null
  thinking: Thinking
  created_at: string
  last_message_at: string | null
  // 来自哪个聊天软件，网页上的对话没有
  channel?: 'weixin' | 'qq' | 'telegram'
}

export interface ModelView {
  id: number
  name: string
  base_url: string
  model: string
  thinking_style: ThinkingStyle
  thinking_model: string
  temperature: number
  max_tokens: number
  vision: boolean
  tools: boolean
  scope: 'builtin' | 'public' | 'personal'
  is_default: boolean
  key_mask: string
  editable: boolean
}

export interface ModelInput {
  name: string
  base_url: string
  api_key: string
  model: string
  thinking_style: ThinkingStyle
  thinking_model: string
  temperature: number
  max_tokens: number
  vision: boolean
  tools: boolean
  public: boolean
}

export interface TestResult {
  ok: boolean
  latency_ms: number
  reply?: string
  error?: string
}

export interface EmbeddingView {
  base_url: string
  model: string
  dim: number
  key_mask: string
  ready: boolean
  source: 'web' | 'config'
}

export interface EmbeddingInput {
  base_url: string
  api_key: string
  model: string
  dim: number
}

export interface ReembedStatus {
  running: boolean
  model: string
  total: number
  done: number
  failed: number
  error?: string
  started_at?: string
  finished_at?: string
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
  reasoning?: string
  model?: string
  attachments?: Attachment[]
  tools?: ToolEvent[]
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

export interface EmotionLevel {
  name: string
  label: string
  intensity: number
}

export interface EmotionEvent {
  kind: 'message' | 'absence' | 'pace'
  cause: string
  changes: Record<string, number>
  created_at: string
}

export interface EmotionView {
  enabled: boolean
  emotions?: EmotionLevel[]
  mood?: number
  affinity?: number
  trust?: number
  summary?: string
  cause?: string
  relation?: string
  events?: EmotionEvent[]
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

// uploadFile 上传一个文件（聊天附件）。
async function uploadFile(file: File, signal?: AbortSignal): Promise<Attachment> {
  const form = new FormData()
  form.append('file', file, file.name)
  let resp: Response
  try {
    resp = await fetch('/api/v1/uploads', { method: 'POST', headers: headers(false), body: form, signal })
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e
    throw new ApiError(0, 'network', '上传失败，请检查网络')
  }
  if (!resp.ok) throw await toError(resp)
  return (await resp.json()) as Attachment
}

// fetchUpload 读取附件内容（需要登录令牌，不能直接用 <img src>）。
async function fetchUpload(id: number): Promise<Blob> {
  const resp = await fetch(`/api/v1/uploads/${id}`, { headers: headers(false) })
  if (!resp.ok) throw await toError(resp)
  return resp.blob()
}

export const api = {
  uploadFile,
  fetchUpload,
  deleteUpload: (id: number) => request<void>('DELETE', `/uploads/${id}`),
  meta: () => request<Meta>('GET', '/meta'),
  login: (username: string, password: string) =>
    request<Session>('POST', '/auth/login', { username, password }),
  register: (input: { username: string; password: string; display_name?: string; invite_code?: string }) =>
    request<Session>('POST', '/auth/register', input),
  logoutAll: () => request<void>('POST', '/auth/logout-all'),
  changePassword: (old_password: string, new_password: string) =>
    request<Session>('POST', '/auth/password', { old_password, new_password }),
  me: () => request<{ user: User; quota: Quota; has_model: boolean; embedding_ready: boolean }>('GET', '/me'),
  conversations: () => request<{ conversations: Conversation[] }>('GET', '/conversations'),
  createConversation: (input: { title?: string; model_id?: number | null; thinking?: Thinking } = {}) =>
    request<Conversation>('POST', '/conversations', input),
  updateConversation: (id: number, patch: { title?: string; model_id?: number | null; thinking?: Thinking }) =>
    request<Conversation>('PATCH', `/conversations/${id}`, patch),
  deleteConversation: (id: number) => request<void>('DELETE', `/conversations/${id}`),
  messages: (conversationId: number, beforeId = 0, limit = 50) =>
    request<{ conversation_id: number; messages: Message[]; has_more: boolean }>(
      'GET',
      `/conversations/${conversationId}/messages?limit=${limit}${beforeId ? `&before_id=${beforeId}` : ''}`,
    ),
  models: () => request<{ models: ModelView[] }>('GET', '/models'),
  createModel: (input: ModelInput) => request<ModelView>('POST', '/models', input),
  updateModel: (id: number, input: ModelInput) => request<ModelView>('PUT', `/models/${id}`, input),
  deleteModel: (id: number) => request<void>('DELETE', `/models/${id}`),
  setDefaultModel: (id: number) => request<void>('POST', `/models/${id}/default`),
  clearDefaultModel: () => request<void>('DELETE', '/models/default'),
  // 不带 input 时测试已保存的模型；带 input 时测试表单中的配置（id 为 0 表示尚未保存）。
  testModel: (id: number, input?: ModelInput) =>
    input && !id ? request<TestResult>('POST', '/models/test', input) : request<TestResult>('POST', `/models/${id}/test`, input),
  embedding: () =>
    request<{ embedding: EmbeddingView; pending: number; counts: Record<string, number>; reembed: ReembedStatus }>(
      'GET',
      '/admin/embedding',
    ),
  setEmbedding: (input: EmbeddingInput) =>
    request<{ embedding: EmbeddingView; changed: boolean; pending: number }>('PUT', '/admin/embedding', input),
  testEmbedding: (input: EmbeddingInput) => request<TestResult>('POST', '/admin/embedding/test', input),
  reembedStatus: () => request<ReembedStatus>('GET', '/admin/embedding/reembed'),
  startReembed: (all = false) => request<ReembedStatus>('POST', '/admin/embedding/reembed', { all }),
  memories: (params: { type?: string; source?: string; q?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') qs.set(k, String(v))
    return request<{ memories: Memory[]; total: number }>('GET', `/memories?${qs}`)
  },
  deleteMemory: (id: number) => request<void>('DELETE', `/memories/${id}`),
  emotion: () => request<EmotionView>('GET', '/emotion'),
  resetEmotion: () => request<void>('DELETE', '/emotion'),
  invites: () => request<{ invites: Invite[] }>('GET', '/admin/invites'),
  createInvite: (input: { max_uses: number; ttl_hours: number; note: string }) =>
    request<Invite>('POST', '/admin/invites', input),
  deleteInvite: (id: number) => request<void>('DELETE', `/admin/invites/${id}`),
  reminders: () => request<{ reminders: Reminder[]; enabled: boolean; care: boolean; timezone: string }>('GET', '/reminders'),
  createReminder: (input: ReminderInput) => request<Reminder>('POST', '/reminders', input),
  updateReminder: (id: number, input: ReminderInput) => request<Reminder>('PUT', `/reminders/${id}`, input),
  pauseReminder: (id: number, pause: boolean) => request<Reminder>('POST', `/reminders/${id}/${pause ? 'pause' : 'resume'}`),
  cancelReminder: (id: number) => request<void>('DELETE', `/reminders/${id}`),
  setProactive: (on: boolean) => request<{ on: boolean }>('PUT', '/me/proactive', { on }),
  latestMessage: (conversationId: number) => request<{ latest_id: number }>('GET', `/conversations/${conversationId}/latest`),
  channels: () => request<{ channels: ChannelInfo[]; bindings: Binding[]; weixin: WeixinUser | null }>('GET', '/channels'),
  weixinLogin: () => request<WeixinLogin>('POST', '/channels/weixin/login'),
  weixinLogout: () => request<void>('POST', '/channels/weixin/logout'),
  bindCode: () => request<{ code: string; expires_at: string }>('POST', '/channels/code'),
  unbind: (id: number) => request<void>('DELETE', `/channels/bindings/${id}`),
  presets: () => request<{ current: string; presets: PersonaPreset[] }>('GET', '/persona/presets'),
  setPreset: (id: string) => request<{ current: string }>('PUT', '/persona/preset', { id }),
  files: (params: { kind?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') qs.set(k, String(v))
    return request<{ files: FileItem[]; total: number; usage: Usage }>('GET', `/files?${qs}`)
  },
  usage: () => request<Usage>('GET', '/files/usage'),
  deleteFiles: (input: { ids: number[] } | { older_than_days: number }) =>
    request<{ deleted: number; usage: Usage }>('POST', '/files/delete', input),
}

export interface ChatHandlers {
  onStart?: (data: {
    conversation: Conversation
    user_message: Message
    assistant_message_id: number
    model: { id: number; name: string }
  }) => void
  onMemories?: (memories: UsedMemory[]) => void
  onReasoning?: (text: string) => void
  onDelta: (text: string) => void
  onTool?: (ev: ToolEvent) => void
}

// sendChat 发送消息并解析 SSE 事件流。正常结束时返回最终的助手消息。
export async function sendChat(
  conversationId: number,
  content: string,
  attachmentIds: number[],
  h: ChatHandlers,
  signal?: AbortSignal,
): Promise<Message> {
  let resp: Response
  try {
    resp = await fetch('/api/v1/chat', {
      method: 'POST',
      headers: headers(true),
      body: JSON.stringify({ conversation_id: conversationId, content, attachment_ids: attachmentIds, stream: true }),
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
        case 'reasoning':
          h.onReasoning?.(payload.text)
          break
        case 'tool':
          h.onTool?.(payload)
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
