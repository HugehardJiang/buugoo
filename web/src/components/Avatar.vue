<script setup lang="ts">
// 随机头像：一只正面的圆滚滚小鸟（和布谷一个画风）。背景、羽毛颜色、眼神、嘴、头顶的毛、脸蛋和配饰
// 都由种子决定，同一个种子永远画出同一只鸟。颜色固定不随深浅色模式变，头像在哪里看都一样。
import { computed } from 'vue'

const props = withDefaults(defineProps<{ seed: string; size?: number; label?: string }>(), { size: 40, label: '头像' })

// 种子 → 32 位哈希 → 简单的伪随机数（mulberry32）。
function rng(seed: string) {
  let h = 2166136261
  for (let i = 0; i < seed.length; i++) h = Math.imul(h ^ seed.charCodeAt(i), 16777619)
  let a = h >>> 0
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const BACKS = ['#f1e6d6', '#e3ecdf', '#e4e9f0', '#f2e1dc', '#ece4f0', '#f4ecd0', '#dfece9', '#efe7e1']
// 羽毛：[身体, 翅膀（深一点）, 肚子]
const FEATHERS = [
  ['#9fb38f', '#7f9670', '#f4ecdc'], // 鼠尾草绿（布谷本色）
  ['#d9a078', '#bf825b', '#f7e7d6'], // 陶土
  ['#8fa9c4', '#6f8cab', '#eef1f4'], // 雾蓝
  ['#e6c36a', '#c9a44a', '#fbf1d2'], // 鹅黄
  ['#c79aa6', '#a97b88', '#f6e8eb'], // 豆沙粉
  ['#a99ac4', '#8a7ba8', '#efebf5'], // 薰衣草
  ['#7fb5ad', '#5f978f', '#e8f3f1'], // 青瓷
  ['#b8a28c', '#987f68', '#f3ece4'], // 奶茶
  ['#6f7f8f', '#55636f', '#e9ecef'], // 灰鸽
  ['#d98b7e', '#bb6c60', '#f8e5e0'], // 珊瑚
]
const BEAKS = ['#e8a04f', '#f0c04a', '#e3896d']
const EYES = ['dot', 'dot', 'happy', 'sleepy', 'sparkle', 'wink'] as const
const TUFTS = ['none', 'curl', 'spikes', 'leaf'] as const
const EXTRAS = ['none', 'none', 'none', 'bow', 'flower', 'hat', 'crown', 'glasses', 'headphones', 'scarf'] as const

const bird = computed(() => {
  const r = rng(props.seed || 'buugoo')
  const pick = <T,>(xs: readonly T[]) => xs[Math.floor(r() * xs.length)]
  const [body, wing, belly] = pick(FEATHERS)
  return {
    back: pick(BACKS), body, wing, belly,
    beak: pick(BEAKS), eyes: pick(EYES), tuft: pick(TUFTS), extra: pick(EXTRAS),
    cheeks: r() < 0.7, accent: pick(['#d9645a', '#e0a33c', '#6b8fc0', '#9a6fb0', '#4f9a7a']),
  }
})
</script>

<template>
  <svg class="avatar" :width="size" :height="size" viewBox="0 0 64 64" :role="label ? 'img' : undefined" :aria-label="label || undefined" :aria-hidden="label ? undefined : 'true'">
    <circle cx="32" cy="32" r="32" :fill="bird.back" />
    <g>
      <!-- 头顶的毛 -->
      <path v-if="bird.tuft === 'curl'" d="M31 15 Q30 8 36 8 Q33 10 34 15" fill="none" :stroke="bird.wing" stroke-width="2.2" stroke-linecap="round" />
      <path v-else-if="bird.tuft === 'spikes'" d="M27 16 L28 9 L31 15 L33 7 L35 15 L38 10 L38 17 Z" :fill="bird.wing" />
      <path v-else-if="bird.tuft === 'leaf'" d="M32 15 Q31 7 38 6 Q38 13 32 15 Z" fill="#7fae6a" />
      <!-- 身体、翅膀、肚子 -->
      <ellipse cx="32" cy="38" rx="21" ry="20" :fill="bird.body" />
      <path d="M12 38 Q8 46 15 51 Q18 45 16 37 Z" :fill="bird.wing" />
      <path d="M52 38 Q56 46 49 51 Q46 45 48 37 Z" :fill="bird.wing" />
      <ellipse cx="32" cy="45" rx="13" ry="11" :fill="bird.belly" />
      <!-- 围巾在肚子上面 -->
      <g v-if="bird.extra === 'scarf'">
        <path d="M15 41 Q32 49 49 41 L49 45 Q32 53 15 45 Z" :fill="bird.accent" />
        <path d="M40 46 L44 55 L39 54 L37 47 Z" :fill="bird.accent" />
      </g>
      <!-- 脸蛋 -->
      <g v-if="bird.cheeks" fill="#e9a8a0" opacity=".75">
        <ellipse cx="20.5" cy="38.5" rx="3" ry="1.9" />
        <ellipse cx="43.5" cy="38.5" rx="3" ry="1.9" />
      </g>
      <!-- 眼睛 -->
      <g v-if="bird.eyes === 'dot'" fill="#2b2a27">
        <circle cx="24" cy="32" r="2.4" /><circle cx="40" cy="32" r="2.4" />
        <circle cx="24.8" cy="31.2" r=".75" fill="#fff" /><circle cx="40.8" cy="31.2" r=".75" fill="#fff" />
      </g>
      <g v-else-if="bird.eyes === 'sparkle'" fill="#2b2a27">
        <circle cx="24" cy="32" r="3.3" /><circle cx="40" cy="32" r="3.3" />
        <circle cx="25.2" cy="30.8" r="1.2" fill="#fff" /><circle cx="41.2" cy="30.8" r="1.2" fill="#fff" />
        <circle cx="23" cy="33.3" r=".5" fill="#fff" /><circle cx="39" cy="33.3" r=".5" fill="#fff" />
      </g>
      <g v-else fill="none" stroke="#2b2a27" stroke-width="1.9" stroke-linecap="round">
        <template v-if="bird.eyes === 'happy'">
          <path d="M21 33 Q24 29 27 33" /><path d="M37 33 Q40 29 43 33" />
        </template>
        <template v-else-if="bird.eyes === 'sleepy'">
          <path d="M21 32 Q24 34.5 27 32" /><path d="M37 32 Q40 34.5 43 32" />
        </template>
        <template v-else>
          <path d="M21 33 Q24 29 27 33" /><circle cx="40" cy="32" r="2.3" fill="#2b2a27" stroke="none" />
        </template>
      </g>
      <!-- 嘴 -->
      <path d="M28.5 36 L35.5 36 L32 41 Z" :fill="bird.beak" stroke-linejoin="round" />
      <!-- 配饰 -->
      <g v-if="bird.extra === 'glasses'" fill="none" stroke="#2b2a27" stroke-width="1.4">
        <circle cx="24" cy="32" r="5" /><circle cx="40" cy="32" r="5" /><path d="M29 32 H35" />
      </g>
      <g v-else-if="bird.extra === 'bow'" :fill="bird.accent">
        <path d="M38 19 L32 15 L32 23 Z M38 19 L44 15 L44 23 Z" transform="translate(4 -1)" />
        <circle cx="42" cy="18" r="1.8" />
      </g>
      <g v-else-if="bird.extra === 'flower'">
        <g :fill="bird.accent"><circle cx="45" cy="15" r="2.6" /><circle cx="49" cy="18" r="2.6" /><circle cx="47.5" cy="22.5" r="2.6" /><circle cx="42.5" cy="22.5" r="2.6" /><circle cx="41" cy="18" r="2.6" /></g>
        <circle cx="45" cy="19" r="2" fill="#f4d36b" />
      </g>
      <g v-else-if="bird.extra === 'hat'">
        <path d="M24 19 L32 1 L40 19 Z" :fill="bird.accent" />
        <path d="M26.5 13 L37.5 13" stroke="#fff" stroke-width="1.4" opacity=".7" />
        <circle cx="32" cy="2" r="2.4" fill="#f4d36b" />
      </g>
      <g v-else-if="bird.extra === 'crown'">
        <path d="M23 20 L24 10 L28.5 15 L32 8 L35.5 15 L40 10 L41 20 Z" fill="#e8bf4f" stroke="#c99a2e" stroke-width=".8" stroke-linejoin="round" />
        <circle cx="32" cy="16" r="1.4" :fill="bird.accent" />
      </g>
      <g v-else-if="bird.extra === 'headphones'">
        <path d="M13 34 Q13 14 32 14 Q51 14 51 34" fill="none" stroke="#3a3a3a" stroke-width="2.6" />
        <rect x="8.5" y="30" width="7" height="11" rx="3" :fill="bird.accent" />
        <rect x="48.5" y="30" width="7" height="11" rx="3" :fill="bird.accent" />
      </g>
    </g>
  </svg>
</template>

<style scoped>
.avatar { display: block; flex-shrink: 0; border-radius: 50%; }
</style>
