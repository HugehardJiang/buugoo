// 时间与文本格式化工具。
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'

const md = new MarkdownIt({ html: false, linkify: true, breaks: true })

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
