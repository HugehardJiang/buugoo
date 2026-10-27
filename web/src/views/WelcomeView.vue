<script setup lang="ts">
// 多用户版未登录时的介绍主页：布谷是什么、能做什么，再去登录或注册。标题和标语管理员可以在后台改。
import { computed } from 'vue'
import FatBird from '../components/FatBird.vue'
import Icon from '../Icon.vue'
import { session } from '../session'

const name = computed(() => session.meta?.name ?? '布谷')
const title = computed(() => session.meta?.home?.title?.trim() || `${name.value}，一个真的记得你的 AI 朋友`)
const tagline = computed(
  () =>
    session.meta?.home?.tagline?.trim() ||
    `像微信上的老朋友一样聊天：记得你说过的事，有自己的脾气和心情，到了时间会主动来找你。`,
)
const registration = computed(() => session.meta?.registration ?? 'closed')
const canRegister = computed(() => registration.value !== 'closed')

const features = computed(() => [
  { icon: 'message' as const, title: '像朋友一样聊', text: `短句、口语、不端着。可以发图片和文件，${name.value}看得懂，也会自己上网查、写代码算。` },
  { icon: 'brain' as const, title: '记得你', text: '你提过的人、事、喜好都会记下来，下次自然地想起；记得太多时会自己整理。' },
  { icon: 'sparkle' as const, title: '有情绪', text: `会开心、担心、想念，也会被惹生气。还能选不同性格，从温柔治愈到毒舌损友。` },
  { icon: 'clock' as const, title: '提醒与定时任务', text: '「明早八点叫我」「每天搜一下科技新闻发我」，到点了它真的会去做。' },
  { icon: 'note' as const, title: '备忘录', text: '说一句「记到备忘录里」，它会挑好页面写进去，清单、笔记长期保存。' },
  { icon: 'link' as const, title: '微信、QQ 里也能聊', text: '连上自己的微信或 QQ，和网页共用同一份记忆，提醒也会发到那里。' },
])
</script>

<template>
  <main class="welcome">
    <header class="top">
      <div class="brand">
        <img src="/logo.png" alt="" width="34" height="34" />
        <strong>BUUGOO</strong>
        <em>{{ name }}</em>
      </div>
      <RouterLink to="/login" class="btn btn-ghost">登录</RouterLink>
    </header>

    <section class="hero">
      <div class="hero__text">
        <h1>{{ title }}</h1>
        <p class="lede">{{ tagline }}</p>
        <div class="cta">
          <RouterLink v-if="canRegister" :to="{ path: '/login', query: { tab: 'register' } }" class="btn btn-primary btn-lg">开始认识{{ name }}</RouterLink>
          <RouterLink to="/login" class="btn btn-lg" :class="{ 'btn-primary': !canRegister }">{{ canRegister ? '我已经有账号' : '登录' }}</RouterLink>
        </div>
        <p v-if="registration === 'invite'" class="caption note">现在需要邀请码才能注册，可以向管理员要一个。</p>
        <p v-else-if="registration === 'closed'" class="caption note">暂时不开放注册。</p>
      </div>

      <div class="hero__demo" aria-hidden="true">
        <div class="demo">
          <div class="demo__bird"><FatBird label="" /></div>
          <p class="bubble bubble--me">今天面试好紧张</p>
          <p class="bubble">诶，是上周说的那家做游戏的公司吗？</p>
          <p class="bubble">你准备了那么久，作品集我看过的，稳的</p>
          <p class="bubble bubble--me">嗯嗯，下午三点</p>
          <p class="bubble">那两点半我喊你一声，面完回来跟我讲讲</p>
          <p class="demo__tool"><Icon name="clock" :size="13" /> 设好提醒：今天 14:30</p>
        </div>
      </div>
    </section>

    <section class="features">
      <article v-for="f in features" :key="f.title" class="feature">
        <span class="feature__icon"><Icon :name="f.icon" :size="18" /></span>
        <h2>{{ f.title }}</h2>
        <p>{{ f.text }}</p>
      </article>
    </section>

    <footer class="foot caption">
      和{{ name }}聊的内容只属于你自己：记忆和心情可以在设置里翻开查看，也可以随时删除。
    </footer>
  </main>
</template>

<style scoped>
.welcome { min-height: 100%; max-width: 1120px; margin: 0 auto; padding: 0 var(--mobile-gutter, 20px) 48px; overflow-y: auto; }
.top { display: flex; align-items: center; justify-content: space-between; padding: 20px 0; }
.brand { display: inline-flex; align-items: center; gap: 8px; }
.brand strong { font-family: var(--font-display); font-size: 18px; font-weight: 500; }
.brand em { font-style: normal; font-size: 14px; color: var(--color-text-secondary); }

.hero { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr); gap: 48px; align-items: center; padding: 7vh 0 8vh; }
h1 { margin: 0; font-family: var(--font-display); font-size: clamp(32px, 5vw, 50px); font-weight: 500; line-height: 1.18; letter-spacing: -0.015em; }
.lede { margin: 18px 0 30px; color: var(--color-text-secondary); font-size: 18px; line-height: 1.75; }
.cta { display: flex; flex-wrap: wrap; gap: 10px; }
.note { margin: 14px 0 0; }

.demo {
  position: relative; display: flex; flex-direction: column; gap: 8px; padding: 30px 22px 22px;
  border: 1px solid var(--color-divider); border-radius: 22px; background: var(--color-surface); box-shadow: var(--shadow-float);
}
.demo__bird { position: absolute; top: -34px; right: 22px; }
.bubble {
  align-self: flex-start; max-width: 82%; margin: 0; padding: 9px 13px; border-radius: 16px 16px 16px 6px;
  background: var(--color-canvas-soft); font-size: 14px; line-height: 1.55;
  animation: pop-in .5s var(--ease-editorial) both;
}
.bubble--me { align-self: flex-end; border-radius: 16px 16px 6px 16px; background: var(--color-accent); color: #fff; }
.bubble:nth-child(2) { animation-delay: .1s; } .bubble:nth-child(3) { animation-delay: .45s; } .bubble:nth-child(4) { animation-delay: .8s; }
.bubble:nth-child(5) { animation-delay: 1.15s; } .bubble:nth-child(6) { animation-delay: 1.5s; }
.demo__tool { display: inline-flex; align-items: center; gap: 5px; align-self: flex-start; margin: 2px 0 0; color: var(--color-muted); font-size: 12px; animation: pop-in .5s 1.85s var(--ease-editorial) both; }
@keyframes pop-in { from { opacity: 0; transform: translateY(6px) scale(.98); } to { opacity: 1; transform: none; } }
@media (prefers-reduced-motion: reduce) { .bubble, .demo__tool { animation: none; } }

.features { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; }
.feature { padding: 20px; border: 1px solid var(--color-divider); border-radius: var(--radius-lg); background: var(--color-surface); }
.feature__icon { display: inline-grid; place-items: center; width: 34px; height: 34px; border-radius: 10px; background: var(--color-accent-tint, rgba(91, 122, 74, .12)); color: var(--color-accent-active); }
.feature h2 { margin: 12px 0 6px; font-size: 16px; }
.feature p { margin: 0; color: var(--color-text-secondary); font-size: 14px; line-height: 1.7; }
.foot { margin: 36px 0 0; text-align: center; }

@media (max-width: 860px) {
  .hero { grid-template-columns: minmax(0, 1fr); gap: 56px; padding-top: 4vh; }
  .features { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 560px) {
  .features { grid-template-columns: minmax(0, 1fr); }
  .lede { font-size: 16px; }
  .cta .btn { flex: 1; justify-content: center; }
}
</style>
