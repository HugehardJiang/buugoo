<script setup lang="ts">
// 介绍主页的插画：几何色块 + 手绘感的墨线（圆头、略带弧度），颜色来自设计系统的插画色板，
// 只在小地方点一点暖陶土色。每张都是独立的 SVG，按 name 选。
defineProps<{ name: 'hero' | 'memory' | 'emotion' | 'remind' | 'memo' | 'channels' | 'cta' }>()
</script>

<template>
  <!-- 主视觉：胖鸟站在树枝上，身边飘着聊天气泡 -->
  <svg v-if="name === 'hero'" class="illo" viewBox="0 0 520 460" aria-hidden="true">
    <circle cx="300" cy="250" r="178" class="f-mist" />
    <path d="M64 120 a62 62 0 0 1 124 0 z" class="f-lavender" />
    <rect x="388" y="58" width="74" height="74" rx="18" transform="rotate(14 425 95)" class="f-sand" />
    <circle cx="96" cy="356" r="9" class="f-clay" />
    <g class="dots"><circle cx="420" cy="368" r="3" /><circle cx="440" cy="368" r="3" /><circle cx="460" cy="368" r="3" />
      <circle cx="420" cy="388" r="3" /><circle cx="440" cy="388" r="3" /><circle cx="460" cy="388" r="3" /></g>

    <!-- 树枝和叶子 -->
    <path d="M40 392 C140 372 250 386 330 370 S470 352 500 360" class="ink" />
    <path d="M120 381 C112 356 128 340 150 336 C150 360 140 374 120 381 z" class="f-sage ink" />
    <path d="M410 360 C420 336 440 328 462 332 C454 352 436 364 410 360 z" class="f-sage ink" />

    <!-- 胖鸟 -->
    <g class="bird">
      <path d="M178 268 L132 236 L144 284 z" class="f-moss ink" />
      <ellipse cx="262" cy="276" rx="104" ry="92" class="f-sage ink" />
      <ellipse cx="282" cy="306" rx="64" ry="54" class="f-cream" />
      <path d="M200 256 C176 296 196 328 226 336 C240 310 236 280 222 258 z" class="f-moss ink wing" />
      <circle cx="306" cy="238" r="8" class="f-ink" />
      <circle cx="309" cy="235" r="2.4" class="f-white" />
      <ellipse cx="326" cy="266" rx="11" ry="7" class="f-blush" />
      <path d="M352 242 L392 256 L352 270 z" class="f-clay ink" />
      <path d="M246 186 C248 164 266 158 278 166 C266 170 262 178 264 190" class="ink" />
      <path d="M236 364 v10 M290 366 v10 M226 374 h20 M280 376 h20" class="ink" />
    </g>

    <!-- 聊天气泡 -->
    <g class="float-a">
      <path d="M86 168 h112 a18 18 0 0 1 18 18 v28 a18 18 0 0 1 -18 18 h-74 l-20 16 v-16 h-18 a18 18 0 0 1 -18 -18 v-28 a18 18 0 0 1 18 -18 z" class="f-white ink" />
      <circle cx="118" cy="200" r="5" class="f-ink" /><circle cx="142" cy="200" r="5" class="f-ink" /><circle cx="166" cy="200" r="5" class="f-ink" />
    </g>
    <g class="float-b">
      <path d="M372 150 h70 a16 16 0 0 1 16 16 v22 a16 16 0 0 1 -16 16 h-6 v14 l-18 -14 h-46 a16 16 0 0 1 -16 -16 v-22 a16 16 0 0 1 16 -16 z" class="f-white ink" />
      <path d="M407 191 C393 181 388 173 393 166 C397 160 405 162 407 168 C409 162 417 160 421 166 C426 173 421 181 407 191 z" class="f-clay" />
    </g>
    <path d="M206 92 v22 M195 103 h22" class="ink sparkle" />
    <path d="M470 236 v16 M462 244 h16" class="ink sparkle" />
  </svg>

  <!-- 记得你：一叠记忆卡片，用线连着 -->
  <svg v-else-if="name === 'memory'" class="illo" viewBox="0 0 400 320" aria-hidden="true">
    <circle cx="200" cy="168" r="128" class="f-cream" />
    <rect x="86" y="96" width="118" height="140" rx="12" transform="rotate(-9 145 166)" class="f-white ink" />
    <rect x="106" y="122" width="76" height="52" rx="6" transform="rotate(-9 144 148)" class="f-mist" />
    <path d="M112 196 l66 -10 M116 214 l44 -7" class="ink thin" />
    <rect x="200" y="78" width="118" height="140" rx="12" transform="rotate(7 259 148)" class="f-white ink" />
    <circle cx="258" cy="128" r="24" class="f-lavender" />
    <path d="M222 180 l68 8 M220 198 l46 5" class="ink thin" />
    <path d="M150 262 C190 286 236 284 270 252" class="ink dash" />
    <circle cx="150" cy="262" r="9" class="f-clay ink" />
    <circle cx="270" cy="252" r="9" class="f-sage ink" />
    <path d="M330 72 v18 M321 81 h18" class="ink" />
    <circle cx="70" cy="80" r="6" class="f-sage" />
  </svg>

  <!-- 有情绪：三颗不同表情的石子 -->
  <svg v-else-if="name === 'emotion'" class="illo" viewBox="0 0 400 320" aria-hidden="true">
    <path d="M40 250 C120 226 280 226 360 250" class="ink" />
    <ellipse cx="112" cy="196" rx="58" ry="52" class="f-sand ink" />
    <path d="M92 188 q6 -8 12 0 M120 188 q6 -8 12 0" class="ink" />
    <path d="M98 212 q14 14 28 0" class="ink" />
    <ellipse cx="208" cy="150" rx="62" ry="58" class="f-mist ink" />
    <circle cx="190" cy="142" r="5" class="f-ink" /><circle cx="226" cy="142" r="5" class="f-ink" />
    <path d="M194 174 q14 -10 28 0" class="ink" />
    <path d="M178 112 l14 6 M238 112 l-14 6" class="ink" />
    <ellipse cx="300" cy="200" rx="50" ry="46" class="f-lavender ink" />
    <path d="M282 196 h12 M308 196 h12" class="ink" />
    <path d="M288 218 q12 -8 24 0" class="ink" />
    <path d="M334 150 c6 -12 18 -12 22 -2 c4 -10 18 -8 18 4 c0 12 -20 22 -20 22 s-20 -10 -20 -24 z" class="f-clay ink" />
    <path d="M60 110 v16 M52 118 h16" class="ink" />
    <circle cx="150" cy="88" r="6" class="f-sage" />
  </svg>

  <!-- 到点就来找你：钟、铃铛和纸飞机 -->
  <svg v-else-if="name === 'remind'" class="illo" viewBox="0 0 400 320" aria-hidden="true">
    <rect x="60" y="70" width="280" height="200" rx="100" class="f-cream" />
    <circle cx="172" cy="168" r="84" class="f-white ink" />
    <circle cx="172" cy="168" r="66" class="f-mist" />
    <path d="M172 120 v48 l30 18" class="ink thick" />
    <circle cx="172" cy="168" r="6" class="f-ink" />
    <path d="M118 88 l-18 -18 M226 88 l18 -18" class="ink" />
    <path d="M290 136 c0 -26 16 -40 34 -40 s34 14 34 40 v30 l12 16 h-92 l12 -16 z" class="f-sand ink" />
    <circle cx="324" cy="196" r="9" class="f-clay ink" />
    <path d="M270 70 l60 -22 l-14 52 l-14 -18 z" class="f-white ink" />
    <path d="M302 82 l14 18" class="ink thin" />
    <path d="M222 54 c18 -6 30 -2 44 8" class="ink dash" />
  </svg>

  <!-- 帮你记着：本子、清单和铅笔 -->
  <svg v-else-if="name === 'memo'" class="illo" viewBox="0 0 400 320" aria-hidden="true">
    <circle cx="214" cy="164" r="124" class="f-lavender" />
    <rect x="104" y="58" width="168" height="214" rx="14" transform="rotate(-4 188 165)" class="f-white ink" />
    <path d="M122 70 v200" transform="rotate(-4 188 165)" class="ink thin" />
    <g transform="rotate(-4 188 165)">
      <rect x="140" y="98" width="16" height="16" rx="4" class="f-sage ink" />
      <path d="M143 106 l4 4 l8 -9" class="ink thin" />
      <path d="M168 106 h76" class="ink" />
      <rect x="140" y="138" width="16" height="16" rx="4" class="f-white ink" />
      <path d="M168 146 h60" class="ink" />
      <rect x="140" y="178" width="16" height="16" rx="4" class="f-white ink" />
      <path d="M168 186 h70" class="ink" />
      <path d="M140 224 h90" class="ink thin" />
    </g>
    <g transform="rotate(32 300 210)">
      <rect x="286" y="120" width="28" height="140" rx="6" class="f-sand ink" />
      <path d="M286 260 l14 30 l14 -30 z" class="f-cream ink" />
      <rect x="286" y="120" width="28" height="20" rx="6" class="f-clay ink" />
    </g>
    <circle cx="74" cy="236" r="8" class="f-mist" />
    <path d="M330 64 v16 M322 72 h16" class="ink" />
  </svg>

  <!-- 在哪都能聊：网页和手机，用虚线连起来 -->
  <svg v-else-if="name === 'channels'" class="illo" viewBox="0 0 400 320" aria-hidden="true">
    <rect x="40" y="110" width="320" height="150" rx="75" class="f-cream" />
    <rect x="62" y="80" width="170" height="122" rx="12" class="f-white ink" />
    <path d="M62 104 h170" class="ink thin" />
    <circle cx="78" cy="92" r="3.5" class="f-clay" /><circle cx="90" cy="92" r="3.5" class="f-sand" /><circle cx="102" cy="92" r="3.5" class="f-sage" />
    <rect x="80" y="120" width="76" height="22" rx="11" class="f-mist" />
    <rect x="132" y="152" width="80" height="22" rx="11" class="f-sage" />
    <rect x="262" y="118" width="86" height="150" rx="16" class="f-white ink" />
    <path d="M292 132 h26" class="ink thin" />
    <rect x="276" y="152" width="50" height="18" rx="9" class="f-lavender" />
    <rect x="290" y="180" width="46" height="18" rx="9" class="f-sage" />
    <rect x="276" y="208" width="40" height="18" rx="9" class="f-lavender" />
    <path d="M190 224 C210 270 250 280 268 250" class="ink dash" />
    <circle cx="190" cy="224" r="7" class="f-clay ink" />
    <path d="M350 70 v16 M342 78 h16" class="ink" />
  </svg>

  <!-- 结尾：月亮下飞着的小鸟（深色底上用） -->
  <svg v-else class="illo" viewBox="0 0 320 220" aria-hidden="true">
    <circle cx="232" cy="74" r="40" class="f-sand" />
    <circle cx="250" cy="62" r="34" class="f-dark" />
    <g class="float-a">
      <path d="M90 132 L62 116 L70 146 z" class="f-moss ink-light" />
      <ellipse cx="128" cy="138" rx="50" ry="42" class="f-sage ink-light" />
      <ellipse cx="138" cy="152" rx="30" ry="24" class="f-cream" />
      <path d="M104 118 C86 98 96 82 120 94" class="f-moss ink-light wing" />
      <circle cx="146" cy="124" r="4.5" class="f-ink" />
      <path d="M170 128 L190 136 L170 144 z" class="f-clay ink-light" />
    </g>
    <path d="M40 60 v14 M33 67 h14 M280 160 v12 M274 166 h12" class="ink-light" />
    <circle cx="70" cy="190" r="3" class="f-cream" /><circle cx="210" cy="196" r="3" class="f-cream" />
  </svg>
</template>

<style scoped>
.illo { display: block; width: 100%; height: auto; overflow: visible; }
.ink { fill: none; stroke: var(--color-ink); stroke-width: 2.4; stroke-linecap: round; stroke-linejoin: round; }
.ink-light { stroke: var(--color-on-dark); stroke-width: 2.2; stroke-linecap: round; stroke-linejoin: round; fill: none; }
.thin { stroke-width: 1.6; }
.thick { stroke-width: 4; }
.dash { stroke-dasharray: 2 9; stroke-width: 2.6; }
/* 同时有 fill 和 .ink 时，以 fill 类为准 */
.f-mist { fill: var(--illo-mist); }
.f-lavender { fill: var(--illo-lavender); }
.f-sand { fill: var(--illo-sand); }
.f-cream { fill: var(--illo-cream); }
.f-sage { fill: var(--illo-sage); }
.f-moss { fill: var(--plugin-moss); }
.f-white { fill: #fff; }
.f-ink { fill: var(--color-ink); }
.f-dark { fill: var(--color-surface-dark); }
.f-clay { fill: #d97757; }
.f-blush { fill: #e9b8a3; opacity: .85; }
.dots circle { fill: var(--color-hairline-strong); }

.wing { transform-box: fill-box; transform-origin: 70% 10%; animation: flap 2.6s ease-in-out infinite; }
.float-a { animation: float 6s ease-in-out infinite; }
.float-b { animation: float 7s ease-in-out -2s infinite; }
.sparkle { transform-box: fill-box; transform-origin: center; animation: twinkle 3.2s ease-in-out infinite; }
@keyframes flap { 0%, 80%, 100% { transform: rotate(0deg); } 88% { transform: rotate(-9deg); } 94% { transform: rotate(3deg); } }
@keyframes float { 0%, 100% { transform: translateY(0); } 50% { transform: translateY(-8px); } }
@keyframes twinkle { 0%, 100% { opacity: 1; transform: scale(1); } 50% { opacity: .35; transform: scale(.75); } }
@media (prefers-reduced-motion: reduce) { .wing, .float-a, .float-b, .sparkle { animation: none; } }
</style>
