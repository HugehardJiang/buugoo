// 时间与文本格式化工具。
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'
import { mathPlugin } from './math'

const md = new MarkdownIt({ html: false, linkify: true, breaks: true }).use(mathPlugin)

// renderMarkdown 渲染助手回复，结果经过 DOMPurify 清理。
export function renderMarkdown(src: string): string {
  return DOMPurify.sanitize(md.render(src))
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

export function amount(unit: string, n: number): string {
  if (unit === 'tokens') return `${mtok(n)} 百万 token`
  return `${n >= 10000 ? `${+(n / 10000).toFixed(1)} 万` : n} 条消息`
}

// planSummary 是套餐内容的一句话：30 天 · 每天 200 条消息；或 100 万 token · 90 天内有效。
export function planSummary(p: { kind: string; unit: string; days: number; cycle: string; quota: number; valid_days: number }): string {
  if (p.kind === 'period') return `${p.days} 天 · 每${p.cycle === 'month' ? '月' : '天'} ${amount(p.unit, p.quota)}`
  return `${amount(p.unit, p.quota)} · ${p.valid_days > 0 ? `${p.valid_days} 天内有效` : '永久有效'}`
}
