<script setup lang="ts">
// 消息中的附件：图片显示缩略图（点击放大查看），文档显示文件名（点击下载）；已删除的显示占位。
// size 为 large 时按原比例显示大图（用于代码生成的图表）。
import { ref, watch } from 'vue'
import { type Attachment } from '../api'
import { blobURL, download as save } from '../blobs'
import { openLightbox } from '../lightbox'
import { formatSize } from '../format'
import Icon from '../Icon.vue'
import { toast } from '../toast'

const props = defineProps<{ items: Attachment[]; align?: 'end' | 'start'; size?: 'large' }>()

const urls = ref<Record<number, string>>({})

// 生成回复的过程中可能陆续加入新图片。
watch(
  () => props.items.map((a) => a.id).join(','),
  () => {
    for (const a of props.items) {
      if (a.kind === 'image' && !a.deleted && !urls.value[a.id]) {
        blobURL(a.id).then((u) => (urls.value = { ...urls.value, [a.id]: u })).catch(() => {})
      }
    }
  },
  { immediate: true },
)

function openImage(a: Attachment) {
  openLightbox(a.id, a.filename)
}

async function download(a: Attachment) {
  try {
    await save(a.id, a.filename)
  } catch {
    toast('文件下载失败')
  }
}
</script>

<template>
  <div class="att-list" :class="{ 'att-list--end': align === 'end', 'att-list--large': size === 'large' }">
    <template v-for="a in items" :key="a.id">
      <span v-if="a.deleted" class="att-gone" :title="`${a.filename}（已在文件管理中删除）`">
        <Icon :name="a.kind === 'image' ? 'image' : 'file'" :size="16" />
        <span class="att-gone__text">{{ a.kind === 'image' ? '图片已删除' : '文件已删除' }}</span>
        <span class="att-gone__name">{{ a.filename }}</span>
      </span>
      <button v-else-if="a.kind === 'image'" type="button" class="att-image" :class="{ 'is-loading': !urls[a.id] }" :title="a.filename" @click="openImage(a)">
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
.att-image img { display: block; width: 100%; height: 100%; object-fit: cover; animation: img-in 260ms ease; }
.att-image.is-loading { background: linear-gradient(90deg, var(--color-canvas-soft) 0%, var(--color-surface-hover) 40%, var(--color-canvas-soft) 80%); background-size: 200% 100%; animation: shimmer 1.3s infinite linear; }
@keyframes img-in { from { opacity: 0; } }
.att-gone {
  display: inline-flex; align-items: center; gap: 6px; max-width: 280px; min-height: 36px; padding: 6px 12px;
  border: 1px dashed var(--color-hairline-strong); border-radius: var(--radius-lg); color: var(--color-muted); font-size: 12px;
}
.att-gone__text { flex-shrink: 0; }
.att-gone__name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--color-tertiary); text-decoration: line-through; }
.att-list--large { max-width: 640px; margin: 4px 0 8px; }
.att-list--large .att-image {
  width: auto; height: auto; max-width: 100%; min-width: 200px; min-height: 120px; border: 1px solid var(--color-divider); background: #fff;
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
