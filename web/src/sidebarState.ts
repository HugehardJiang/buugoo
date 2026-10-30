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
