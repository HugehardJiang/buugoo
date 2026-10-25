// 全局轻提示：一句话说明发生了什么，几秒后自动消失。
import { reactive } from 'vue'

export const toasts = reactive<{ id: number; text: string }[]>([])
let seq = 0

export function toast(text: string, ms = 3200) {
  const id = ++seq
  toasts.push({ id, text })
  setTimeout(() => {
    const i = toasts.findIndex((t) => t.id === id)
    if (i >= 0) toasts.splice(i, 1)
  }, ms)
}
