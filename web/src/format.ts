// 时间与文本格式化工具。
import type { Plan } from './api'
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'
import { mathPlugin } from './math'

const md = new MarkdownIt({ html: false, linkify: true, breaks: true }).use(mathPlugin)

// renderMarkdown 渲染助手回复，结果经过 DOMPurify 清理。
export function renderMarkdown(src: string): string {
  return DOMPurify.sanitize(md.render(wrapBareLatex(src)))
}

// 模型偶尔把 LaTeX 公式直接写成一行、忘了用 $ 包起来（满屏反斜杠）。一整行都是公式时（有 LaTeX 命令、
// 没有 $、没有中文），当成单独一行的公式渲染。代码块里的不动。
const latexCmd = /\\(frac|dfrac|int|iint|oint|sum|prod|sqrt|lim|cdot|times|quad|qquad|ge|le|geq|leq|neq|infty|partial|alpha|beta|gamma|delta|theta|lambda|mu|sigma|tau|omega|pi|mathrm|mathbf|left|right|begin)(?![a-zA-Z])/
const cjk = /[\u3400-\u9fff\uff00-\uffef]/
export function wrapBareLatex(src: string): string {
  if (!src.includes('\\')) return src
  let fence = false
  return src
    .split('\n')
    .map((line) => {
      const t = line.trim()
      if (t.startsWith('```')) fence = !fence
      if (fence || !t || t.includes('$') || t.startsWith('`') || cjk.test(t) || !latexCmd.test(t)) return line
      return `$$${t}$$`
    })
    .join('\n')
}

const weekdays = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六']

function pad(n: number) {
  return n.toString().padStart(2, '0')
}

export function dayKey(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

// dayLabel 返回日期分隔条的文字：今天 / 昨天 / 10月20日 星期一 / 2025年…
export function dayLabel(iso: string): string {
  const d = new Date(iso)
  const today = new Date()
  const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1)
  if (dayKey(iso) === dayKey(today.toISOString())) return '今天'
  if (dayKey(iso) === dayKey(yesterday.toISOString())) return '昨天'
  const base = `${d.getMonth() + 1}月${d.getDate()}日 ${weekdays[d.getDay()]}`
  return d.getFullYear() === today.getFullYear() ? base : `${d.getFullYear()}年${base}`
}

export function timeOf(iso: string): string {
  const d = new Date(iso)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function dateTime(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${timeOf(iso)}`
}

// shortWhen 返回侧栏中对话的时间：今天显示时刻，今年显示月/日，更早显示年份。
export function shortWhen(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  const today = new Date()
  if (dayKey(iso) === dayKey(today.toISOString())) return timeOf(iso)
  if (d.getFullYear() === today.getFullYear()) return `${d.getMonth() + 1}/${d.getDate()}`
  return `${d.getFullYear()}/${d.getMonth() + 1}`
}

// formatSize 把字节数格式化为 KB / MB。
export function formatSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}


// amount 把额度写成好读的样子：1.2 万 token、300 条消息。
// TOKEN_UNIT 是 token 额度的显示和填写单位：百万 token。
export const TOKEN_UNIT = 1_000_000

// mtok 把 token 数写成以百万为单位的数字（最多三位小数）。
export function mtok(n: number): string {
  if (!n) return '0'
  const v = n / TOKEN_UNIT
  if (Math.abs(v) < 0.001) return v > 0 ? '<0.001' : '>-0.001'
  return String(+v.toFixed(Math.abs(v) < 1 ? 3 : 2))
}

// YUAN 是 1 元对应的微元数（金额都以微元存整数）。
export const YUAN = 1_000_000

// yuan 把微元写成「¥12.5」，小于 1 分的保留到 0.0001 元。
export function yuan(micro: number): string {
  const v = micro / YUAN
  const digits = Math.abs(v) >= 1 || v === 0 ? 2 : 4
  return `${v < 0 ? '-' : ''}¥${(+Math.abs(v).toFixed(digits)).toString()}`
}

export function amount(unit: string, n: number): string {
  if (unit === 'tokens') return `${mtok(n)} 百万 token`
  if (unit === 'searches') return `${n} 次搜索`
  return `${n >= 10000 ? `${+(n / 10000).toFixed(1)} 万` : n} 条消息`
}

// planQuotas 列出时长套餐每周期的各项额度（0 是不限，不列出来）。
export function planQuotas(p: { quota_messages: number; quota_tokens: number; quota_searches: number }): string[] {
  const out: string[] = []
  if (p.quota_messages) out.push(amount('messages', p.quota_messages))
  if (p.quota_tokens) out.push(amount('tokens', p.quota_tokens))
  if (p.quota_searches) out.push(amount('searches', p.quota_searches))
  return out
}

// planSummary 是套餐内容的一句话：30 天 · 每天 200 条消息、2 百万 token；或 充值 ¥10。
export function planSummary(p: Pick<Plan, 'kind' | 'days' | 'cycle' | 'quota_messages' | 'quota_tokens' | 'quota_searches' | 'amount'>): string {
  if (p.kind === 'recharge') return `充值 ${yuan(p.amount)} 到余额`
  if (p.kind === 'pack') return '旧余额包（已停用）'
  const q = planQuotas(p)
  return `${p.days} 天 · 每${p.cycle === 'month' ? '月' : '天'} ${q.length ? q.join('、') : '不限量'}`
}
