<script setup lang="ts">
// 放大查看图片：点击空白处、按 Esc 或关闭按钮收起；可以下载原图。
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { blobURL, download } from '../blobs'
import Icon from '../Icon.vue'
import { closeLightbox, lightbox } from '../lightbox'
import { toast } from '../toast'

const url = ref('')
const failed = ref(false)

watch(
  () => [lightbox.open, lightbox.id] as const,
  async ([open, id]) => {
    if (!open) return
    url.value = ''
    failed.value = false
    try {
      const u = await blobURL(id)
      if (lightbox.id === id) url.value = u
    } catch {
      failed.value = true
    }
  },
)

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && lightbox.open) closeLightbox()
}
onMounted(() => document.addEventListener('keydown', onKey))
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))

async function save() {
  try {
    await download(lightbox.id, lightbox.filename)
  } catch {
    toast('下载失败')
  }
}
</script>

<template>
  <Teleport to="body">
    <Transition name="lightbox">
      <div v-if="lightbox.open" class="lightbox" role="dialog" aria-modal="true" :aria-label="lightbox.filename" @click.self="closeLightbox">
        <div class="lightbox__bar">
          <span class="lightbox__name">{{ lightbox.filename }}</span>
          <button type="button" class="lightbox__btn" aria-label="下载" title="下载" @click="save"><Icon name="download" :size="18" /></button>
          <button type="button" class="lightbox__btn" aria-label="关闭" title="关闭" @click="closeLightbox"><Icon name="x" :size="18" /></button>
        </div>
        <img v-if="url" :src="url" :alt="lightbox.filename" class="lightbox__img" @click="closeLightbox" />
        <p v-else-if="failed" class="lightbox__msg">图片打不开了</p>
        <div v-else class="lightbox__spin" aria-label="加载中" />
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.lightbox {
  position: fixed; inset: 0; z-index: 60; display: flex; align-items: center; justify-content: center; padding: 64px 24px 24px;
  background: rgba(20, 20, 19, .82); backdrop-filter: blur(4px); cursor: zoom-out;
}
.lightbox__bar {
  position: absolute; top: 0; left: 0; right: 0; display: flex; align-items: center; gap: 6px; height: 56px;
  padding: 0 12px 0 20px; color: var(--color-on-dark);
}
.lightbox__name { flex: 1; min-width: 0; overflow: hidden; color: var(--color-on-dark-soft); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.lightbox__btn {
  display: inline-flex; width: 38px; height: 38px; align-items: center; justify-content: center; border-radius: var(--radius-rail);
  color: var(--color-on-dark); transition: background var(--motion-fast) ease;
}
.lightbox__btn:hover { background: rgba(250, 249, 245, .12); }
.lightbox__img {
  max-width: 100%; max-height: 100%; border-radius: var(--radius-md); background: #fff; object-fit: contain;
  box-shadow: 0 20px 60px rgba(0, 0, 0, .35);
}
.lightbox__msg { color: var(--color-on-dark-soft); }
.lightbox__spin {
  width: 28px; height: 28px; border: 2px solid rgba(250, 249, 245, .25); border-top-color: var(--color-on-dark); border-radius: 50%;
  animation: spin .8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
.lightbox-enter-active, .lightbox-leave-active { transition: opacity 200ms ease; }
.lightbox-enter-active .lightbox__img { transition: transform 280ms var(--ease-editorial); }
.lightbox-enter-from, .lightbox-leave-to { opacity: 0; }
.lightbox-enter-from .lightbox__img { transform: scale(.96); }
@media (max-width: 767px) { .lightbox { padding: 56px 8px 16px; } }
</style>
