// markdown-it 数学公式插件：用 KaTeX 渲染 \( … \)、\[ … \]、$ … $、$$ … $$。
// 模型（例如 DeepSeek）常用 \( \) 与 \[ \] 写公式，Markdown 会把其中的反斜杠当成转义吃掉，
// 所以要在 Markdown 解析之前识别出来。
import katex from 'katex'
import 'katex/dist/katex.min.css'
import type { MarkdownIt, StateBlock, StateInline, Token } from 'markdown-it'

function render(tex: string, displayMode: boolean): string {
  try {
    return katex.renderToString(tex, { displayMode, throwOnError: false, output: 'html', strict: 'ignore' })
  } catch {
    return `<code>${tex.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' })[c]!)}</code>`
  }
}

// $…$ 的左右不能是空白，结尾后面不能紧跟数字，避免把"花了 $5 和 $10"当成公式。
function dollarInline(src: string, pos: number): { end: number; tex: string } | null {
  if (src[pos + 1] === '$' || /\s/.test(src[pos + 1] ?? ' ')) return null
  for (let i = pos + 1; i < src.length; i++) {
    if (src[i] === '\\') {
      i++
      continue
    }
    if (src[i] === '\n' && src[i + 1] === '\n') return null
    if (src[i] === '$') {
      if (/\s/.test(src[i - 1]) || /\d/.test(src[i + 1] ?? '')) return null
      return { end: i + 1, tex: src.slice(pos + 1, i) }
    }
  }
  return null
}

function mathInline(state: StateInline, silent: boolean): boolean {
  const src = state.src
  const pos = state.pos
  let open = ''
  let close = ''
  if (src.startsWith('\\(', pos)) [open, close] = ['\\(', '\\)']
  else if (src.startsWith('\\[', pos)) [open, close] = ['\\[', '\\]']
  else if (src.startsWith('$$', pos)) [open, close] = ['$$', '$$']
  else if (src[pos] === '$') {
    const m = dollarInline(src, pos)
    if (!m) return false
    if (!silent) {
      const t = state.push('math_inline', 'math', 0)
      t.content = m.tex
    }
    state.pos = m.end
    return true
  } else return false

  const end = src.indexOf(close, pos + open.length)
  if (end < 0) return false
  if (!silent) {
    const t = state.push(open === '\\(' ? 'math_inline' : 'math_display_inline', 'math', 0)
    t.content = src.slice(pos + open.length, end)
  }
  state.pos = end + close.length
  return true
}

// 独占几行的 \[ … \] 或 $$ … $$。
function mathBlock(state: StateBlock, startLine: number, endLine: number, silent: boolean): boolean {
  const lineText = (n: number) => state.src.slice(state.bMarks[n] + state.tShift[n], state.eMarks[n])
  const first = lineText(startLine)
  let open = ''
  let close = ''
  if (first.startsWith('\\[')) [open, close] = ['\\[', '\\]']
  else if (first.startsWith('$$')) [open, close] = ['$$', '$$']
  else return false

  let body = first.slice(open.length)
  let line = startLine
  let found = false
  // 开头一行里就结束：\[ x \]
  const sameLine = body.indexOf(close)
  if (sameLine >= 0) {
    if (body.slice(sameLine + close.length).trim() !== '') return false
    body = body.slice(0, sameLine)
    found = true
  } else {
    const parts = [body]
    while (++line < endLine) {
      const t = lineText(line)
      const idx = t.indexOf(close)
      if (idx >= 0) {
        if (t.slice(idx + close.length).trim() !== '') return false
        parts.push(t.slice(0, idx))
        found = true
        break
      }
      parts.push(t)
    }
    body = parts.join('\n')
  }
  if (!found) return false
  if (silent) return true
  const tok = state.push('math_block', 'math', 0)
  tok.block = true
  tok.content = body
  tok.map = [startLine, line + 1]
  state.line = line + 1
  return true
}

export function mathPlugin(md: MarkdownIt) {
  md.inline.ruler.before('escape', 'math_inline', mathInline)
  md.block.ruler.before('fence', 'math_block', mathBlock, { alt: ['paragraph', 'reference', 'blockquote', 'list'] })
  md.renderer.rules.math_inline = (tokens: Token[], idx: number) => render(tokens[idx].content, false)
  md.renderer.rules.math_display_inline = (tokens: Token[], idx: number) => `<span class="math-display">${render(tokens[idx].content, true)}</span>`
  md.renderer.rules.math_block = (tokens: Token[], idx: number) => `<div class="math-display">${render(tokens[idx].content, true)}</div>\n`
}
