// 布谷 HTTP API 客户端。
import { getConsoleToken } from './consoleToken'
import { recordError } from './diag'

export interface User {
  id: number
  username: string
  display_name: string
  is_admin: boolean
  proactive: boolean
  created_at: string
  avatar_seed: string
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
  // billing 为 true 时有会员系统（多用户版）。
  billing?: boolean
  // home 是多用户版未登录时介绍主页的标题和标语，空字符串表示用内置的。
  home?: { title: string; tagline: string }
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
  /** 临时对话：不在列表里、不写记忆和情绪，结束后删除 */
  temporary?: boolean
  /** 临时对话里是否参考记忆和心情 */
  use_context?: boolean
}

// ContextStatus 是一段对话的上下文用量。
export interface ContextStatus {
  used: number
  size: number
  percent: number
  compact_at: number
  keep_recent: number
  pending: boolean
  summary: string
  summarized: boolean
  enabled: boolean
  model: string
  // cache_rate 是最近一条回复命中服务商缓存的比例（%），cache_total 是这段对话合计的；服务商不返回缓存信息时没有。
  cache_rate?: number
  cache_total?: number
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
  // context_tokens 是手动填写的上下文长度（0 自动识别），context 是实际使用的长度与来源
  context_tokens: number
  context: { tokens: number; source: 'manual' | 'api' | 'table' | 'default' }
  scope: 'builtin' | 'public' | 'personal'
  is_default: boolean
  key_mask: string
  editable: boolean
  /** 只有授权用户能用（公共模型） */
  restricted: boolean
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
  context_tokens: number
  public: boolean
  restricted?: boolean
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
  searches: number
  messages_limit: number
  tokens_limit: number
  searches_limit: number
  exempt: boolean
  // free 为 false 表示没有免费额度（按量扣费模式下没有套餐）；paying 为 true 表示额度用完了，正在扣余额。
  free: boolean
  paying: boolean
  mode?: 'quota_first' | 'pay'
  period?: 'day' | 'month'
  reset_at?: string
  member?: Member
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

export interface DiagEntry {
  time: string
  level: string
  msg: string
  attrs?: Record<string, string>
}

export interface Plan {
  id: number
  name: string
  description: string
  price: string
  // pack 是已经停用的旧余额包，只在管理后台里显示。
  kind: 'period' | 'recharge' | 'pack'
  days: number
  cycle: 'day' | 'month' | ''
  // 时长套餐每周期的额度，0 表示不限。
  quota_messages: number
  quota_tokens: number
  quota_searches: number
  // 充值到账的金额（微元）。
  amount: number
  buy_url: string
  visible?: boolean
  sort?: number
}

export interface Member {
  active: boolean
  plan_name?: string
  cycle?: string
  quota_messages?: number
  quota_tokens?: number
  quota_searches?: number
  expires_at?: string
  balance: number
}

export interface CostItem {
  key: string
  amount: number
  calls: number
  tokens: number
}

export interface BillingAccount {
  subscription?: { plan_name: string; cycle: string; quota_messages: number; quota_tokens: number; quota_searches: number; expires_at: string }
  balance: number
  today: CostItem[]
  month: CostItem[]
  logs: { id: number; kind: string; detail: string; created_at: string }[]
}

export interface PricingItem {
  key: string
  label: string
  kind: 'tokens' | 'calls'
  group: string
  hint?: string
  model?: string
}

export interface Price {
  input?: number
  // cache_input 是命中缓存的输入价格，不填和 input 一样。
  cache_input?: number
  output?: number
  per_k?: number
  // off_peak 是空闲时段的价格（开了分时段计价时才用）。
  off_peak?: Price
}

export interface PeakRule {
  // days 是星期几（0 是星期日）。
  days: number[]
  ranges: string[]
  holidays: string[]
}

export interface Pricing {
  mode: 'quota_first' | 'pay'
  prices: Record<string, Price>
  peak?: PeakRule
}

/** 模型价格（元 / 百万 token）；off_peak 是分时段计价时的空闲价 */
export interface ModelPrice {
  input?: number
  cache_input?: number
  output?: number
  off_peak?: { input?: number; cache_input?: number; output?: number }
}

export interface Todo {
  id: number
  title: string
  note: string
  due_at?: string
  /** 截止时间（服务器时区），「YYYY-MM-DD」或「YYYY-MM-DD HH:MM」，没有时为空 */
  due: string
  all_day: boolean
  done: boolean
  done_at?: string
  created_at: string
}

export interface TodoInput {
  title?: string
  note?: string
  /** 空字符串表示去掉截止时间 */
  due?: string
  done?: boolean
}

export interface MemoPageInfo {
  id: number
  title: string
  size: number
  preview: string
  updated_at: string
}

export interface MemoPage {
  id: number
  title: string
  content: string
  updated_at: string
}

export interface CompactReport {
  before: number
  after: number
  merged: number
  created: number
}

export interface Extension {
  kind: 'skill' | 'plugin'
  name: string
  description: string
  enabled: boolean
}

export interface AgentAction {
  id: number
  kind: string
  summary: string
  admin: boolean
  status: 'pending' | 'running' | 'done' | 'canceled' | 'failed'
  result?: string
  created_at: string
  option?: { key: string; label: string; value: boolean }
}

export interface AgentMessage {
  id: number
  role: 'user' | 'assistant'
  content: string
  tools?: { name: string; label: string; summary: string }[]
  actions?: AgentAction[]
  created_at: string
}

export interface AgentState {
  messages: AgentMessage[]
  admin: boolean
  model_id: number | null
  model?: { id: number; name: string; tools: boolean }
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    // requestId 是服务器给这次请求的编号（响应头 X-Request-Id），复制诊断信息时带上，方便在服务器日志里找。
    public requestId = '',
    public path = '',
  ) {
    super(message)
    recordError({ time: new Date().toISOString(), status, code, message, requestId, path })
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
  // 在这个浏览器登录了管理后台时带上，牛马助手据此决定能不能改系统设置。
  const c = getConsoleToken()
  if (c) h['X-Console-Token'] = c
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
  if (resp.status === 502 || resp.status === 503 || resp.status === 504) {
    if (code === 'http_error') message = `没有收到布谷的回复（HTTP ${resp.status}）：可能是布谷的服务停了或在重启，也可能是前面的反向代理（Nginx、宝塔、Cloudflare）把错误换成了自己的页面`
  }
  return new ApiError(resp.status, code, message, resp.headers.get('X-Request-Id') ?? '', new URL(resp.url, location.href).pathname)
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
    throw new ApiError(0, 'network', '无法连接到服务器，请检查网络', '', `/api/v1${path}`)
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
  createConversation: (
    input: { title?: string; model_id?: number | null; thinking?: Thinking; temporary?: boolean; use_context?: boolean } = {},
  ) => request<Conversation>('POST', '/conversations', input),
  updateConversation: (id: number, patch: { title?: string; model_id?: number | null; thinking?: Thinking; use_context?: boolean }) =>
    request<Conversation>('PATCH', `/conversations/${id}`, patch),
  deleteConversation: (id: number) => request<void>('DELETE', `/conversations/${id}`),
  /** 关页面时删掉临时对话（keepalive：页面关掉后请求也能发完） */
  discardConversation: (id: number) => {
    fetch(`/api/v1/conversations/${id}`, { method: 'DELETE', headers: headers(false), keepalive: true }).catch(() => {})
  },
  messages: (conversationId: number, beforeId = 0, limit = 50) =>
    request<{ conversation_id: number; messages: Message[]; has_more: boolean }>(
      'GET',
      `/conversations/${conversationId}/messages?limit=${limit}${beforeId ? `&before_id=${beforeId}` : ''}`,
    ),
  /** prices：计费开着时各公共模型现在的价格（元 / 百万 token）；peak：现在是不是高峰时段 */
  models: () =>
    request<{ models: ModelView[]; prices?: Record<string, ModelPrice>; peak?: boolean }>('GET', '/models'),
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
  profile: () => request<{ user: User; avatar_left: number; avatar_limit: number }>('GET', '/me/profile'),
  updateProfile: (display_name: string) => request<{ user: User }>('PUT', '/me/profile', { display_name }),
  regenerateAvatar: () => request<{ user: User; avatar_left: number }>('POST', '/me/avatar'),
  setProactive: (on: boolean) => request<{ on: boolean }>('PUT', '/me/proactive', { on }),
  context: (conversationId: number) => request<ContextStatus>('GET', `/conversations/${conversationId}/context`),
  compact: (conversationId: number) => request<ContextStatus>('POST', `/conversations/${conversationId}/compact`),
  latestMessage: (conversationId: number) => request<{ latest_id: number }>('GET', `/conversations/${conversationId}/latest`),
  channels: () => request<{ channels: ChannelInfo[]; bindings: Binding[]; weixin: WeixinUser | null }>('GET', '/channels'),
  weixinLogin: () => request<WeixinLogin>('POST', '/channels/weixin/login'),
  weixinLogout: () => request<void>('POST', '/channels/weixin/logout'),
  bindCode: () => request<{ code: string; expires_at: string }>('POST', '/channels/code'),
  unbind: (id: number) => request<void>('DELETE', `/channels/bindings/${id}`),
  billing: () => request<{ account: BillingAccount; quota: Quota; plans: Plan[]; labels: Record<string, string> }>('GET', '/billing'),
  redeem: (code: string) => request<{ message: string }>('POST', '/billing/redeem', { code }),
  diagnostics: () => request<{ server: Record<string, unknown>; recent: DiagEntry[] }>('GET', '/diagnostics'),
  memos: () => request<{ pages: MemoPageInfo[] }>('GET', '/memos'),
  memo: (id: number) => request<MemoPage>('GET', `/memos/${id}`),
  createMemo: (title: string, content = '') => request<MemoPage>('POST', '/memos', { title, content }),
  updateMemo: (id: number, patch: { title?: string; content?: string }) => request<MemoPage>('PUT', `/memos/${id}`, patch),
  deleteMemo: (id: number) => request<void>('DELETE', `/memos/${id}`),
  deleteMessage: (conversationId: number, id: number) => request<void>('DELETE', `/conversations/${conversationId}/messages/${id}`),
  openCodeCatalog: () =>
    request<{ models: { id: string; name: string; vision?: boolean; free?: boolean; note?: string }[]; mine: string[]; date: string }>('GET', '/opencode/catalog'),
  removeMyOpenCode: (models: string[]) => request<{ removed: number }>('POST', '/opencode/mine/remove', { models }),
  addMyOpenCode: (apiKey: string, models: string[]) => request<{ added: number; updated: number }>('POST', '/opencode/mine', { api_key: apiKey, models }),
  todos: () => request<{ items: Todo[] }>('GET', '/todos'),
  createTodo: (input: { title: string; note?: string; due?: string }) => request<Todo>('POST', '/todos', input),
  updateTodo: (id: number, patch: TodoInput) => request<Todo>('PUT', `/todos/${id}`, patch),
  deleteTodo: (id: number) => request<void>('DELETE', `/todos/${id}`),
  clearDoneTodos: () => request<{ deleted: number }>('POST', '/todos/clear-done'),
  compactMemories: () => request<CompactReport>('POST', '/memories/compact'),
  extensions: () => request<{ extensions: Extension[] }>('GET', '/me/extensions'),
  toggleExtension: (kind: Extension['kind'], name: string, enabled: boolean) =>
    request<void>('PUT', '/me/extensions', { kind, name, enabled }),
  agent: () => request<AgentState>('GET', '/agent'),
  agentSend: (content: string) => request<AgentMessage>('POST', '/agent/messages', { content }),
  agentConfirm: (id: number, options?: Record<string, boolean>) =>
    request<AgentAction>('POST', `/agent/actions/${id}/confirm`, options ? { options } : undefined),
  agentCancel: (id: number) => request<AgentAction>('POST', `/agent/actions/${id}/cancel`),
  agentModel: (id: number | null) => request<void>('PUT', '/agent/model', { id }),
  agentClear: () => request<void>('DELETE', '/agent'),
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
export function sendChat(
  conversationId: number,
  content: string,
  attachmentIds: number[],
  h: ChatHandlers,
  signal?: AbortSignal,
): Promise<Message> {
  return streamChat('/api/v1/chat', { conversation_id: conversationId, content, attachment_ids: attachmentIds, stream: true }, h, signal)
}

// regenerateChat 重新生成对话里最后一条回复，事件和 sendChat 一样。
export function regenerateChat(conversationId: number, h: ChatHandlers, signal?: AbortSignal): Promise<Message> {
  return streamChat(`/api/v1/conversations/${conversationId}/regenerate`, {}, h, signal)
}

async function streamChat(url: string, body: unknown, h: ChatHandlers, signal?: AbortSignal): Promise<Message> {
  let resp: Response
  try {
    resp = await fetch(url, {
      method: 'POST',
      headers: headers(true),
      body: JSON.stringify(body),
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
          throw new ApiError(502, payload.error?.code ?? 'error', payload.error?.message ?? '生成失败', resp.headers.get('X-Request-Id') ?? '', '/api/v1/chat')
      }
    }
  }
  if (!final) throw new ApiError(0, 'stream', '连接意外中断，回复可能不完整', resp.headers.get('X-Request-Id') ?? '', '/api/v1/chat')
  return final
}
