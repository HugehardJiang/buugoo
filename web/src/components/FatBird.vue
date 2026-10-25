<script setup lang="ts">
// 等待回复时的小动画：一只圆滚滚的大肥鸟左右扭动、扑腾小翅膀。颜色来自设计系统的插画色板。
defineProps<{ label?: string }>()
</script>

<template>
  <span class="fat-bird" role="status" :aria-label="label ?? '正在回复'">
    <svg viewBox="0 0 64 56" width="62" height="54" aria-hidden="true">
      <ellipse class="fat-bird__shadow" cx="32" cy="52.5" rx="17" ry="2.5" />
      <g class="fat-bird__body">
        <!-- 尾巴 -->
        <path class="fat-bird__tail" d="M12 30 L3 24 L6 34 Z" />
        <!-- 圆滚滚的身体与肚子 -->
        <ellipse class="fat-bird__belly-back" cx="32" cy="31" rx="22" ry="19" />
        <ellipse class="fat-bird__belly" cx="35" cy="36" rx="14" ry="12" />
        <!-- 小翅膀 -->
        <path class="fat-bird__wing" d="M20 27 Q14 36 22 41 Q27 36 25 28 Z" />
        <!-- 眼睛、脸蛋与嘴 -->
        <circle class="fat-bird__eye" cx="40" cy="24" r="2.3" />
        <circle class="fat-bird__glint" cx="40.8" cy="23.2" r=".7" />
        <ellipse class="fat-bird__cheek" cx="44" cy="30" rx="2.6" ry="1.6" />
        <path class="fat-bird__beak" d="M49 25 L56 27.5 L49 30 Z" />
        <!-- 头顶一撮毛 -->
        <path class="fat-bird__tuft" d="M30 13.5 Q31 8 35 9 Q32 10.5 33 14" />
      </g>
      <!-- 小脚 -->
      <path class="fat-bird__feet" d="M27 48 V51 M37 48 V51 M25 51 H29 M35 51 H39" />
    </svg>
  </span>
</template>

<style scoped>
.fat-bird { display: inline-flex; padding: 4px 0 0; }
.fat-bird svg { overflow: visible; }
.fat-bird__body { transform-origin: 32px 49px; animation: waddle 0.9s ease-in-out infinite; }
.fat-bird__belly-back, .fat-bird__tail { fill: var(--illo-sage); }
.fat-bird__belly { fill: var(--illo-cream); }
.fat-bird__wing { fill: var(--plugin-moss); transform-origin: 24px 29px; animation: flap 0.45s ease-in-out infinite; }
.fat-bird__eye { fill: var(--color-ink); }
.fat-bird__glint { fill: #fff; }
.fat-bird__cheek { fill: #e9b4a0; opacity: .8; }
.fat-bird__beak { fill: var(--color-warning); }
.fat-bird__tuft { fill: none; stroke: var(--plugin-moss); stroke-width: 1.8; stroke-linecap: round; }
.fat-bird__feet { fill: none; stroke: var(--color-warning); stroke-width: 1.6; stroke-linecap: round; }
.fat-bird__shadow { fill: var(--color-ink); opacity: .08; transform-origin: 32px 52.5px; animation: shadow 0.9s ease-in-out infinite; }

/* 左右扭动，身体在中间时微微压扁，像一步一摇地走 */
@keyframes waddle {
  0%, 100% { transform: rotate(-9deg) scale(1, 1); }
  25% { transform: rotate(0deg) scale(1.05, 0.95); }
  50% { transform: rotate(9deg) scale(1, 1); }
  75% { transform: rotate(0deg) scale(1.05, 0.95); }
}
@keyframes flap {
  0%, 100% { transform: rotate(0deg); }
  50% { transform: rotate(-22deg); }
}
@keyframes shadow {
  0%, 50%, 100% { transform: scaleX(1); }
  25%, 75% { transform: scaleX(1.12); }
}
</style>
