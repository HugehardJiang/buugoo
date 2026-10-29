// 模型的厂商图标和显示用的短名字：按模型名认出是哪家的（GLM 是智谱、Kimi 是月之暗面……），
// 名字里的「（OpenCode）」这类来源后缀去掉，改成小标签显示。
import claude from './assets/vendors/claude-color.svg?raw'
import deepseek from './assets/vendors/deepseek-color.svg?raw'
import gemini from './assets/vendors/gemini-color.svg?raw'
import grok from './assets/vendors/grok.svg?raw'
import hunyuan from './assets/vendors/hunyuan-color.svg?raw'
import kimi from './assets/vendors/kimi-color.svg?raw'
import longcat from './assets/vendors/longcat-color.svg?raw'
import meta from './assets/vendors/meta-color.svg?raw'
import minimax from './assets/vendors/minimax-color.svg?raw'
import openai from './assets/vendors/openai.svg?raw'
import qwen from './assets/vendors/qwen-color.svg?raw'
import stepfun from './assets/vendors/stepfun-color.svg?raw'
import mimo from './assets/vendors/xiaomimimo.svg?raw'
import zhipu from './assets/vendors/zhipu-color.svg?raw'

export interface Vendor {
  name: string
  svg: string
}

// 去掉图标里的 <title>（否则会混进按钮的文字里）。
function clean(svg: string) {
  return svg.replace(/<title>.*?<\/title>/s, '')
}

const rules: { re: RegExp; v: Vendor }[] = [
  { re: /deepseek/, v: { name: 'DeepSeek', svg: deepseek } },
  { re: /glm|zhipu|chatglm|智谱/, v: { name: '智谱', svg: zhipu } },
  { re: /kimi|moonshot/, v: { name: 'Kimi', svg: kimi } },
  { re: /qwen|qwq|通义/, v: { name: '通义千问', svg: qwen } },
  { re: /minimax|abab/, v: { name: 'MiniMax', svg: minimax } },
  { re: /mimo|xiaomi/, v: { name: '小米 MiMo', svg: mimo } },
  { re: /claude|anthropic/, v: { name: 'Claude', svg: claude } },
  { re: /gemini|gemma/, v: { name: 'Gemini', svg: gemini } },
  { re: /grok|xai/, v: { name: 'Grok', svg: grok } },
  { re: /gpt|openai|(^|[^a-z])o[134](-|$)/, v: { name: 'OpenAI', svg: openai } },
  { re: /longcat/, v: { name: 'LongCat', svg: longcat } },
  { re: /step-?\d|stepfun/, v: { name: '阶跃星辰', svg: stepfun } },
  { re: /hunyuan|(^|[^a-z])hy\d/, v: { name: '腾讯混元', svg: hunyuan } },
  { re: /llama|muse|meta/, v: { name: 'Meta', svg: meta } },
]

// vendorOf 按模型 ID 和名字认出厂商，认不出时返回 null。
export function vendorOf(model: string, name = ''): Vendor | null {
  const s = `${model} ${name}`.toLowerCase()
  const v = rules.find((r) => r.re.test(s))?.v
  return v ? { name: v.name, svg: clean(v.svg) } : null
}

// sourceOf 认出模型是从哪个平台接入的（目前只有 OpenCode）。
export function sourceOf(baseURL: string, name = ''): string {
  if (/(^|\.|\/\/)opencode\.ai/.test(baseURL) || /opencode/i.test(name)) return 'OpenCode'
  return ''
}

// shortName 去掉名字里的来源后缀，例如「GLM-5.2（OpenCode）」→「GLM-5.2」。
export function shortName(name: string): string {
  return name.replace(/\s*[（(]\s*opencode\s*[)）]\s*$/i, '').trim() || name
}
