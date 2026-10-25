// 记忆本默认藏起来：朋友不会把心里的小本子摊开给你看。用户确认后才显示，选择只保存在本机浏览器。
import { ref } from 'vue'

const KEY = 'buugoo.memories.unlocked'

function read(): boolean {
  try {
    return localStorage.getItem(KEY) === '1'
  } catch {
    return false
  }
}

export const memoriesUnlocked = ref(read())

export function setMemoriesUnlocked(v: boolean) {
  memoriesUnlocked.value = v
  try {
    if (v) localStorage.setItem(KEY, '1')
    else localStorage.removeItem(KEY)
  } catch {
    // 无法持久化时只在本次打开期间有效。
  }
}
