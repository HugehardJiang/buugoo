// 全局的图片查看器：点击图片后在页面上放大显示，不用打开新标签页。
import { reactive } from 'vue'

export const lightbox = reactive<{ id: number; filename: string } & { open: boolean }>({ id: 0, filename: '', open: false })

export function openLightbox(id: number, filename: string) {
  lightbox.id = id
  lightbox.filename = filename
  lightbox.open = true
}

export function closeLightbox() {
  lightbox.open = false
}
