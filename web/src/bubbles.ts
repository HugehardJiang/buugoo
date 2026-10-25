// 把助手回复拆成几条消息气泡（像微信那样一条条发）。

// 一次回复最多拆成的气泡数；段落再多就是长篇回答，放在一个气泡里更好读。
const maxBubbles = 6

// 代码块、公式块、列表、表格、标题、引用这类成块的内容和前一段放在同一个气泡里。
const structured = /^(```|~~~|\$\$|\\\[|\||#{1,6}\s|[-*+]\s|\d+[.、)]\s?|>)/

// splitBubbles 把助手回复按空行拆成几条消息（像微信那样一条条发）；代码块和公式块内部的空行不拆。
export function splitBubbles(text: string): string[] {
  const paras: string[] = []
  let cur: string[] = []
  let fence = ''
  let math = false
  const flush = () => {
    const p = cur.join('\n').trim()
    if (p) paras.push(p)
    cur = []
  }
  for (const line of text.replace(/\r\n/g, '\n').split('\n')) {
    const t = line.trim()
    if (fence) {
      if (t.startsWith(fence)) fence = ''
    } else if (math) {
      if (t.endsWith('$$') || t.endsWith('\\]')) math = false
    } else if (/^(```|~~~)/.test(t)) {
      fence = t.slice(0, 3)
    } else if ((t === '$$' || (t.startsWith('$$') && !t.slice(2).includes('$$'))) || (t.startsWith('\\[') && !t.includes('\\]'))) {
      math = true
    } else if (t === '') {
      flush()
      continue
    }
    cur.push(line)
  }
  flush()
  const out: string[] = []
  for (const p of paras) {
    if (out.length && structured.test(p)) out[out.length - 1] += '\n\n' + p
    else out.push(p)
  }
  if (out.length > maxBubbles) return [out.join('\n\n')]
  return out.length ? out : ['']
}
