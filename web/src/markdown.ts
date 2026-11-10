// Markdown 渲染：助手回复、备忘录、通知等。单独放一个文件，只有用到的页面才下载 markdown-it（公式库 KaTeX 更是等内容里真有公式时才下载）。
import { mathReady } from './math'
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'
import { mathPlugin } from './math'

const md = new MarkdownIt({ html: false, linkify: true, breaks: true }).use(mathPlugin)

// renderMarkdown 渲染助手回复，结果经过 DOMPurify 清理。
export function renderMarkdown(src: string): string {
  // 读一下 mathReady：公式库下载好之后，用到这段内容的地方会自动重新渲染。
  void mathReady.value
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

