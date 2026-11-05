<script setup lang="ts">
// 强制弹窗通知：管理员发布的通知一条条弹出来，倒计时结束前不能关（没有关闭按钮，Esc 和点外面都没用），
// 关掉后不再主动弹（设置 → 通知 里还能看到）。服务器也会检查看够了没有，改网页绕不过去。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, ApiError, type Notice } from '../api'
import { renderMarkdown } from '../format'

const queue = ref<Notice[]>([])
const current = computed(() => queue.value[0] ?? null)
const left = ref(0)
const closing = ref(false)
const error = ref('')
const btn = ref<HTMLButtonElement | null>(null)
let timer: ReturnType<typeof setInterval> | undefined
let poll: ReturnType<typeof setInterval> | undefined

async function load() {
  if (queue.value.length) return
  try {
    queue.value = (await api.pendingNotices()).notices
  } catch {
    // 拿不到就下次再说，不影响使用。
  }
}

watch(current, (n) => {
  clearInterval(timer)
  error.value = ''
  if (!n) return
  left.value = n.seconds
  timer = setInterval(() => {
    if (left.value > 0) left.value--
    if (left.value <= 0) {
      clearInterval(timer)
      nextTick(() => btn.value?.focus())
    }
  }, 1000)
})

async function close() {
  const n = current.value
  if (!n || left.value > 0 || closing.value) return
  closing.value = true
  error.value = ''
  try {
    await api.readNotice(n.id)
    queue.value = queue.value.slice(1)
    if (!queue.value.length) load()
  } catch (e) {
    // 服务器觉得还没看够（比如电脑时间不准）：再等几秒。
    const m = e instanceof ApiError ? e.message : '关闭失败，请稍后再试'
    const s = /再看 (\d+) 秒/.exec(m)
    if (s) left.value = Number(s[1])
    error.value = m
  } finally {
    closing.value = false
  }
}

// 弹窗开着时挡住 Esc（别的弹窗靠 Esc 关）。
function onKey(e: KeyboardEvent) {
  if (current.value && e.key === 'Escape') {
    e.stopPropagation()
    e.preventDefault()
  }
}

function onVisible() {
  if (document.visibilityState === 'visible') load()
}

onMounted(() => {
  load()
  // 网页一直开着时也能收到新通知。
  poll = setInterval(load, 5 * 60 * 1000)
  document.addEventListener('visibilitychange', onVisible)
  window.addEventListener('keydown', onKey, true)
})
onBeforeUnmount(() => {
  clearInterval(timer)
  clearInterval(poll)
  document.removeEventListener('visibilitychange', onVisible)
  window.removeEventListener('keydown', onKey, true)
})
</script>

<template>
  <Teleport to="body">
    <Transition name="modal" appear>
      <div v-if="current" class="modal-backdrop notice-backdrop">
        <div class="modal notice" role="alertdialog" aria-modal="true" :aria-label="current.title">
          <header class="modal__header">
            <div>
              <p class="notice__kicker">通知<template v-if="queue.length > 1"> · 还有 {{ queue.length - 1 }} 条</template></p>
              <h2 class="modal__title">{{ current.title }}</h2>
            </div>
          </header>
          <div class="modal__body">
            <!-- eslint-disable-next-line vue/no-v-html -->
            <div v-if="current.content" class="md notice__body" v-html="renderMarkdown(current.content)" />
            <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
            <div class="notice__actions">
              <span class="caption">{{ new Date(current.created_at).toLocaleString() }}</span>
              <button ref="btn" type="button" class="btn btn-primary notice__btn" :disabled="left > 0 || closing" @click="close">
                <template v-if="left > 0">请阅读 {{ left }} 秒</template>
                <template v-else>{{ closing ? '…' : '我知道了' }}</template>
                <span v-if="current.seconds > 0" class="notice__bar" :style="{ transform: `scaleX(${left / current.seconds})` }" />
              </button>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
/* 盖在所有东西上面（包括别的弹窗和临时登录的提示条）。 */
.notice-backdrop { z-index: 200; }
.notice { max-width: 560px; }
.notice__kicker { margin: 0 0 4px; color: var(--color-accent-active); font-size: 12px; font-weight: 600; letter-spacing: .06em; }
.notice__body { max-height: 52vh; overflow-y: auto; font-size: 15px; line-height: 1.75; overflow-wrap: anywhere; }
.notice__body :deep(p) { margin: 0 0 10px; }
.notice__body :deep(ul), .notice__body :deep(ol) { margin: 0 0 10px; padding-left: 22px; }
.notice__body :deep(a) { color: var(--color-accent-active); text-decoration: underline; }
.notice__actions { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 18px; }
.notice__btn { position: relative; overflow: hidden; min-width: 132px; font-variant-numeric: tabular-nums; }
/* 倒计时进度：从满到空。 */
.notice__bar {
  position: absolute; left: 0; right: 0; bottom: 0; height: 3px; background: rgba(255, 255, 255, .55);
  transform-origin: left; transition: transform 1s linear;
}
</style>
