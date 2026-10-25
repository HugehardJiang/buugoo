<script setup lang="ts">
// 消息中的附件：图片显示缩略图（点击查看原图），文档显示文件名（点击下载）。
// size 为 large 时按原比例显示大图（用于代码生成的图表）。
// 附件需要带登录令牌读取，所以先取回 Blob 再生成本地地址。
import { ref, watch } from 'vue'
import { api, type Attachment } from '../api'
import { formatSize } from '../format'
import Icon from '../Icon.vue'
import { toast } from '../toast'

const props = defineProps<{ items: Attachment[]; align?: 'end' | 'start'; size?: 'large' }>()

const urls = ref<Record<number, string>>({})

// 同一张图片在页面中只取一次。
const cache = new Map<number, Promise<string>>()
function blobURL(id: number): Promise<string> {
  let p = cache.get(id)
  if (!p) {
    p = api.fetchUpload(id).then((b) => URL.createObjectURL(b))
    cache.set(id, p)
    p.catch(() => cache.delete(id))
  }
  return p
}

// 生成回复的过程中可能陆续加入新图片。
watch(
  () => props.items.map((a) => a.id).join(','),
  () => {
    for (const a of props.items) {
      if (a.kind === 'image' && !urls.value[a.id]) {
        blobURL(a.id).then((u) => (urls.value = { ...urls.value, [a.id]: u })).catch(() => {})
      }
    }
  },
  { immediate: true },
)

async function openImage(a: Attachment) {
  try {
    window.open(await blobURL(a.id), '_blank', 'noopener')
  } catch {
    toast('图片打不开了')
  }
}

async function download(a: Attachment) {
  try {
    const link = document.createElement('a')
    link.href = URL.createObjectURL(await api.fetchUpload(a.id))
    link.download = a.filename
    link.click()
    setTimeout(() => URL.revokeObjectURL(link.href), 10000)
  } catch {
    toast('文件下载失败')
  }
}
</script>

<template>
  <div class="att-list" :class="{ 'att-list--end': align === 'end', 'att-list--large': size === 'large' }">
    <template v-for="a in items" :key="a.id">
      <button v-if="a.kind === 'image'" type="button" class="att-image" :title="a.filename" @click="openImage(a)">
        <img v-if="urls[a.id]" :src="urls[a.id]" :alt="a.filename" />
      </button>
      <button v-else type="button" class="att-file" :title="`下载 ${a.filename}`" @click="download(a)">
        <Icon name="file" :size="18" />
        <span class="att-file__name">{{ a.filename }}</span>
        <span class="att-file__size">{{ formatSize(a.size) }}</span>
      </button>
    </template>
  </div>
</template>

<style scoped>
.att-list { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 8px; margin-bottom: 8px; max-width: min(560px, 86%); }
.att-list--end { justify-content: flex-end; }
.att-image {
  width: 120px; height: 120px; overflow: hidden; padding: 0; border-radius: var(--radius-lg); background: var(--color-canvas-soft);
  transition: transform var(--motion-fast) var(--ease-editorial);
}
.att-image:active { transform: scale(.982); }
.att-image img { display: block; width: 100%; height: 100%; object-fit: cover; }
.att-list--large { max-width: 640px; margin: 4px 0 8px; }
.att-list--large .att-image {
  width: auto; height: auto; max-width: 100%; min-height: 80px; border: 1px solid var(--color-divider); background: #fff;
}
.att-list--large .att-image img { width: auto; height: auto; max-width: 100%; max-height: 480px; object-fit: contain; }
.att-file {
  display: inline-flex; align-items: center; gap: 8px; max-width: 280px; min-height: 44px; padding: 8px 12px;
  border: 1px solid var(--color-divider); border-radius: var(--radius-lg); background: var(--color-surface);
  color: var(--color-text-secondary); font-size: 13px; text-align: left;
}
.att-file:hover { border-color: var(--color-hairline-strong); color: var(--color-ink); }
.att-file__name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--color-ink); }
.att-file__size { flex-shrink: 0; color: var(--color-muted); font-size: 12px; }
@media (max-width: 767px) { .att-image { width: 96px; height: 96px; } }
</style>
