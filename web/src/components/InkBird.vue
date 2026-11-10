<script setup lang="ts">
// 等待回复时的小动画：水墨剪影风格的布谷鸟停在枝头，时不时轻轻歪头张望，旁边三个墨点依次浮现。
// 用一整块墨色画剪影、留白线条表示羽毛和腹部的横纹（布谷鸟的特征），比卡通造型安静、耐看。
defineProps<{ label?: string }>()
</script>

<template>
  <span class="ink-bird" role="status" :aria-label="label ?? '正在回复'">
    <svg viewBox="0 0 96 52" width="88" height="48" aria-hidden="true">
      <!-- 树枝：一笔下去由粗到细；嫩枝和两片叶子 -->
      <path class="ink-bird__branch" d="M2 45.6 C 20 43.4, 46 44.6, 82 42 C 83.4 41.9, 83.6 42.6, 82.4 42.9 C 50 46.2, 22 46.8, 2.4 46.7 Z" />
      <path class="ink-bird__twig" d="M62 43.3 C 66 41.2, 70 38.9, 74.6 38.2" />
      <path class="ink-bird__leaf" d="M71.4 38.6 C 73.6 35.8, 77.4 35, 79.6 36 C 77.8 38.6, 74.6 39.6, 71.4 38.6 Z" />
      <path class="ink-bird__leaf ink-bird__leaf--back" d="M66.4 41 C 67 38.2, 69.2 36.4, 71.4 36.2 C 71 38.6, 69 40.4, 66.4 41 Z" />
      <g class="ink-bird__bird">
        <path class="ink-bird__legs" d="M42.2 38.2 L 41.3 43.8 M 46.8 37.2 L 47.2 43.4" />
        <path
          class="ink-bird__body"
          d="M66 15.6 L 60.6 13.6 C 59.6 10.6, 56.8 8.9, 53.6 9.1 C 50.2 9.3, 47.6 11.6, 46.8 14.8 C 44.5 16.6, 40 18.4, 35.2 21.4 C 30.6 24.4, 26.8 28, 24 31.4 L 6 40.6 L 7.4 42.2 L 26.6 35.2 C 30.4 37.8, 36 39.2, 41.4 38.6 C 48.6 37.8, 54.6 33, 57.2 26.4 C 58.6 22.6, 59.4 19.6, 60.4 17.6 Z"
        />
        <!-- 留白：翅膀、尾羽和腹部的横纹 -->
        <g class="ink-bird__marks">
          <path d="M33 25.6 C 38 26.8, 44.4 26.6, 50.4 23.2" stroke-opacity=".6" />
          <path d="M30.4 29 C 36 31, 42.6 30.8, 48 28" stroke-opacity=".42" />
          <path d="M24.4 33.2 L 10.6 40.2" stroke-opacity=".4" stroke-width=".7" />
          <path d="M45.8 34.4 l 2.6 -1.3 M 49.4 32.4 l 2.2 -1.3 M 52.4 29.9 l 1.8 -1.3" stroke-opacity=".3" stroke-width=".7" />
        </g>
        <circle class="ink-bird__eye" cx="55.3" cy="13.5" r="1.05" />
      </g>
      <g class="ink-bird__dots">
        <circle cx="76" cy="20" r="1.6" />
        <circle cx="82" cy="20" r="1.6" />
        <circle cx="88" cy="20" r="1.6" />
      </g>
    </svg>
  </span>
</template>

<style scoped>
.ink-bird { display: inline-flex; padding: 6px 0 0; animation: ink-in .5s var(--ease-editorial, ease) both; }
.ink-bird svg { overflow: visible; }
.ink-bird__branch { fill: var(--color-ink); fill-opacity: .5; }
.ink-bird__twig { fill: none; stroke: var(--color-ink); stroke-opacity: .42; stroke-width: 1; stroke-linecap: round; }
.ink-bird__leaf { fill: var(--color-accent); fill-opacity: .8; }
.ink-bird__leaf--back { fill-opacity: .5; }
.ink-bird__legs { fill: none; stroke: var(--color-ink); stroke-width: 1.1; stroke-linecap: round; }
.ink-bird__body { fill: var(--color-ink); fill-opacity: .9; }
.ink-bird__marks path { fill: none; stroke: var(--color-canvas, #faf9f5); stroke-width: .9; stroke-linecap: round; }
.ink-bird__eye { fill: var(--color-canvas, #faf9f5); }
/* 时不时歪头张望：以脚为支点轻轻转一下，大部分时间静止。 */
.ink-bird__bird { transform-origin: 44px 43px; animation: ink-look 4.2s ease-in-out infinite; }
.ink-bird__dots circle { fill: var(--color-accent); opacity: .2; animation: ink-dot 1.5s ease-in-out infinite; }
.ink-bird__dots circle:nth-child(2) { animation-delay: .18s; }
.ink-bird__dots circle:nth-child(3) { animation-delay: .36s; }
@keyframes ink-in { from { opacity: 0; transform: translateY(3px); } to { opacity: 1; transform: none; } }
@keyframes ink-look {
  0%, 30%, 100% { transform: rotate(0deg); }
  38%, 52% { transform: rotate(-4deg); }
  60%, 66% { transform: rotate(1.5deg); }
  74% { transform: rotate(0deg); }
}
@keyframes ink-dot {
  0%, 100% { opacity: .2; transform: translateY(0); }
  35% { opacity: 1; transform: translateY(-1.5px); }
}
@media (prefers-reduced-motion: reduce) {
  .ink-bird, .ink-bird__bird, .ink-bird__dots circle { animation: none; }
  .ink-bird__dots circle { opacity: .6; }
}
</style>
