// 桌面端左侧栏的共享状态：收起与否、最近对话的搜索词。
import { ref, watch } from 'vue'

const KEY = 'buugoo.sidebar.collapsed'

function load() {
  try {
    return localStorage.getItem(KEY) === '1'
  } catch {
    return false
  }
}

export const sidebarCollapsed = ref(load())
watch(sidebarCollapsed, (v) => {
  try {
    localStorage.setItem(KEY, v ? '1' : '0')
  } catch {
    // 存不下就算了，只是下次打开不记得。
  }
})

// recentQuery 是侧栏「搜索」里输入的词，用来筛选最近的对话。
export const recentQuery = ref('')
export const searching = ref(false)

// 宽屏（桌面）时用合并的侧栏，窄屏用图标栏和抽屉。
const mq = typeof window !== 'undefined' ? window.matchMedia('(min-width: 961px)') : null
export const wideScreen = ref(mq?.matches ?? true)
mq?.addEventListener('change', (e) => (wideScreen.value = e.matches))

// 侧栏「最近」里的对话：聊天页读到最新的列表后放进来，其他页面也用它，切换页面时列表不会消失或闪一下。
import type { Conversation } from './api'
export const recentConvs = ref<Conversation[]>([])
// activeConvId 是聊天页正在看的对话；openConvHandler 是聊天页提供的「打开对话」（在聊天页时直接切换，不用跳转）。
export const activeConvId = ref(0)
export const openConvHandler = { fn: null as ((id: number) => void) | null }
