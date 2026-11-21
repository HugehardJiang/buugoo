<script setup lang="ts">
// 多用户版未登录时的介绍主页（网站首页）：顶部导航、主视觉、几个功能分区、怎么开始、结尾的行动区。
// 插画见 Illo.vue；标题和标语管理员可以在后台改（home.title、home.tagline）。
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import Illo from '../components/Illo.vue'
import Icon from '../Icon.vue'
import { session } from '../session'

const name = computed(() => session.meta?.name ?? '布谷')
const title = computed(() => session.meta?.home?.title?.trim() || `${name.value}，一个真的记得你的 AI 朋友`)
const tagline = computed(
  () =>
    session.meta?.home?.tagline?.trim() ||
    '像微信上的老朋友一样聊天：记得你说过的事，有自己的脾气和心情，到了时间会主动来找你。',
)
const registration = computed(() => session.meta?.registration ?? 'closed')
const canRegister = computed(() => registration.value !== 'closed')
const startTo = computed(() => (canRegister.value ? { path: '/login', query: { tab: 'register' } } : { path: '/login' }))
const startLabel = computed(() => (canRegister.value ? `开始认识${name.value}` : '登录'))

const sections = computed(() => [
  {
    illo: 'memory' as const,
    eyebrow: '记忆',
    title: '你说过的，它都记得',
    text: `你提过的人、事、喜好，${name.value}会悄悄记下来，下次聊到时自然地想起，不用你一遍遍解释。记得太多时它会自己整理，把相近的合成一条。`,
    points: ['所有对话共用同一份记忆', '想看的时候可以翻开查看、删除'],
  },
  {
    illo: 'emotion' as const,
    eyebrow: '情绪与性格',
    title: '有脾气，也有温度',
    text: `${name.value}会开心、担心、想念，被冒犯了也会生气，慢慢又会消气。你还可以给它选一种性格：温柔治愈、毒舌损友、理性军师，或者有主见会犟嘴的「人工智鸟」。`,
    points: ['心情会随着你们的相处变化', '换了性格，记得的事都还在'],
  },
  {
    illo: 'remind' as const,
    eyebrow: '提醒与定时任务',
    title: '到了时间，它会来找你',
    text: '「半小时后叫我起来活动」是提醒；「每天早上八点搜一下科技新闻发我」是定时任务，到点了它真的会去搜、去算，再把结果发给你。',
    points: ['一次、每天、每周、每月都行', '很久没聊时，也会主动问问你'],
  },
  {
    illo: 'memo' as const,
    eyebrow: '备忘录',
    title: '一句话，帮你记进本子里',
    text: '「把牛奶和鸡蛋记到购物清单里」——它会挑一个合适的页面写进去，没有就新建一页。清单、笔记、资料长期保存，删之前一定先问你。',
    points: ['可以有很多页，网页上也能直接编辑', '平时不会占用聊天的额度'],
  },
  {
    illo: 'channels' as const,
    eyebrow: '随处可聊',
    title: '网页、微信、QQ，都是同一个它',
    text: '扫码连上自己的微信，或者绑定 QQ、Telegram，在哪里聊都共用同一份记忆和心情，提醒也会发到那里。它还能联网搜索、运行代码、用管理员装好的技能和插件。',
    points: ['收发图片和文件都可以', '聊天记录在网页上也能看到'],
  },
])

const steps = computed(() => [
  { n: '1', title: registration.value === 'invite' ? '用邀请码注册' : '注册一个账号', text: registration.value === 'invite' ? '向管理员要一个邀请码，填好就能注册。' : '起个名字、设个密码就好。' },
  { n: '2', title: '选一种相处方式', text: '在设置里挑一个你喜欢的性格，随时可以换。' },
  { n: '3', title: '开始聊天', text: `随便说点什么。想在微信里聊，就在「聊天软件」里扫码连上。` },
])

const arenaColumns = [
  {
    num: '01',
    title: '网感与接梗',
    desc: '用真实中文梗与群聊语境，区分自然接茬和「客服老干部」。',
    href: 'https://arena.buugoo.com/',
  },
  {
    num: '02',
    title: '说人话',
    desc: '检验 AI 能不能把复杂问题讲直白通透，告别套话和官腔。',
    href: 'https://arena.buugoo.com/?col=plain',
  },
]

const arenaPoints = [
  '零系统提示词干扰，直接看模型原生表达',
  '真人匿名双盲投票，独立 K=32 Elo 积分与帕累托前沿排榜',
  '不用注册就能围观实时榜单、参与投票或投稿出题',
]

// 滚动到时淡入。
const root = ref<HTMLElement | null>(null)
let observer: IntersectionObserver | undefined
onMounted(async () => {
  await nextTick()
  const els = root.value?.querySelectorAll('.reveal') ?? []
  if (!('IntersectionObserver' in window)) {
    els.forEach((el) => el.classList.add('is-in'))
    return
  }
  observer = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (e.isIntersecting) {
          e.target.classList.add('is-in')
          observer?.unobserve(e.target)
        }
      }
    },
    { threshold: 0.15 },
  )
  els.forEach((el) => observer!.observe(el))
})
onBeforeUnmount(() => observer?.disconnect())

function scrollTo(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}
</script>

<template>
  <div ref="root" class="site">
    <header class="nav">
      <div class="nav__inner">
        <RouterLink to="/welcome" class="brand">
          <img src="/logo.png" alt="" width="30" height="30" />
          <strong>{{ name }}</strong>
        </RouterLink>
        <nav class="nav__links" aria-label="页面导航">
          <button type="button" @click="scrollTo('features')">能做什么</button>
          <button type="button" @click="scrollTo('start')">怎么开始</button>
          <button type="button" @click="scrollTo('ecosystem')">旗下产品</button>
        </nav>
        <div class="nav__actions">
          <RouterLink to="/login" class="btn btn-outline">登录</RouterLink>
          <RouterLink v-if="canRegister" :to="startTo" class="btn btn-primary">注册</RouterLink>
        </div>
      </div>
    </header>

    <main>
      <section class="hero wrap">
        <div class="hero__text">
          <p class="eyebrow">你的 AI 朋友</p>
          <h1>{{ title }}</h1>
          <p class="lede">{{ tagline }}</p>
          <div class="cta">
            <RouterLink :to="startTo" class="btn btn-primary btn-lg">{{ startLabel }}</RouterLink>
            <RouterLink v-if="canRegister" to="/login" class="btn btn-lg">我已经有账号</RouterLink>
          </div>
          <p v-if="registration === 'invite'" class="caption note">现在需要邀请码才能注册，可以向管理员要一个。</p>
          <p v-else-if="registration === 'closed'" class="caption note">暂时不开放注册，已经有账号的可以直接登录。</p>
        </div>
        <div class="hero__art"><Illo name="hero" /></div>
      </section>

      <section class="manifesto wrap reveal">
        <p>
          不是一个有问必答的助手，<br class="br-wide" />
          而是一个<em>记得你</em>、<em>在乎你</em>、<span class="nowrap">偶尔<em>会主动找你</em>的朋友。</span>
        </p>
      </section>

      <section id="features" class="features wrap">
        <article v-for="(s, i) in sections" :key="s.title" class="feature reveal" :class="{ 'is-flip': i % 2 === 1 }">
          <div class="feature__art"><Illo :name="s.illo" /></div>
          <div class="feature__text">
            <p class="eyebrow">{{ s.eyebrow }}</p>
            <h2>{{ s.title }}</h2>
            <p class="feature__body">{{ s.text }}</p>
            <ul>
              <li v-for="p in s.points" :key="p"><Icon name="check" :size="15" /> {{ p }}</li>
            </ul>
          </div>
        </article>
      </section>

      <div class="band">
      <section class="demo-section wrap reveal">
        <div class="demo-section__text">
          <p class="eyebrow">聊起来是这样的</p>
          <h2>它记得上周你说过什么</h2>
          <p class="feature__body">不用每次从头讲起。它知道你在准备什么、担心什么，也会顺手帮你把事情安排好。</p>
        </div>
        <div class="demo">
          <p class="bubble bubble--me">今天面试好紧张</p>
          <p class="bubble">诶，是上周说的那家做游戏的公司吗？</p>
          <p class="bubble">你准备了那么久，作品集我看过的，稳的</p>
          <p class="bubble bubble--me">嗯嗯，下午三点</p>
          <p class="bubble">那两点半我喊你一声，面完回来跟我讲讲</p>
          <p class="demo__tool"><Icon name="clock" :size="13" /> 设好提醒：今天 14:30</p>
        </div>
      </section>
      </div>

      <section id="start" class="steps wrap reveal">
        <p class="eyebrow">怎么开始</p>
        <h2>三步，认识一个新朋友</h2>
        <ol>
          <li v-for="s in steps" :key="s.n">
            <span class="steps__n">{{ s.n }}</span>
            <h3>{{ s.title }}</h3>
            <p>{{ s.text }}</p>
          </li>
        </ol>
      </section>

      <section id="ecosystem" class="ecosystem wrap reveal">
        <div class="ecosystem__head">
          <p class="eyebrow">除了聊天，我们还做了</p>
          <h2>另一块地方，用来测测 AI 像不像人</h2>
          <p class="ecosystem__intro">
            打磨{{ name }}的对话手感时，我们发现常见跑分测不出谁接得住梗、谁能好好说话，于是把这套真人双盲评测开放了出来。
          </p>
        </div>

        <article class="arena-card">
          <div class="arena-card__art">
            <Illo name="arena" />
          </div>
          <div class="arena-card__body">
            <div class="arena-card__meta">
              <span class="arena-card__name">BuugooArena</span>
              <span class="arena-card__domain">arena.buugoo.com</span>
              <span class="badge-pill">免登录 · 免费开放</span>
            </div>
            <h3>布谷大模型盲测竞技场</h3>
            <p class="arena-card__desc">
              不加系统提示词包装，把同一个真实中文问题直接抛给两个匿名模型，由真人投票判断谁更像活人、谁在说人话，再用独立 K=32 Elo 积分与帕累托前沿曲线实时排榜。
            </p>

            <div class="arena-cols">
              <a
                v-for="col in arenaColumns"
                :key="col.href"
                :href="col.href"
                target="_blank"
                rel="noopener noreferrer"
                class="arena-col"
              >
                <div class="arena-col__top">
                  <span class="arena-col__num">{{ col.num }}</span>
                  <strong>{{ col.title }}</strong>
                  <Icon name="chevron" :size="15" class="arena-col__arrow" />
                </div>
                <p>{{ col.desc }}</p>
              </a>
            </div>

            <ul class="arena-points">
              <li v-for="point in arenaPoints" :key="point">
                <Icon name="check" :size="15" /> {{ point }}
              </li>
            </ul>

            <div class="arena-actions">
              <a
                href="https://arena.buugoo.com"
                target="_blank"
                rel="noopener noreferrer"
                class="btn btn-primary btn-lg"
              >
                看看实时总榜
              </a>
              <a
                href="https://arena.buugoo.com/arena"
                target="_blank"
                rel="noopener noreferrer"
                class="btn btn-outline btn-lg"
              >
                去盲测一局
              </a>
            </div>
          </div>
        </article>
      </section>

      <section class="final wrap reveal">
        <div class="final__card">
          <div class="final__text">
            <h2>{{ name }}在等你开口</h2>
            <p>说一句「你好」就行。剩下的，它会慢慢记住。</p>
            <RouterLink :to="startTo" class="btn btn-lg final__btn">{{ startLabel }}</RouterLink>
          </div>
          <div class="final__art"><Illo name="cta" /></div>
        </div>
      </section>
    </main>

    <footer class="foot wrap">
      <div class="foot__left">
        <span class="brand brand--small"><img src="/logo.png" alt="" width="22" height="22" /> {{ name }}</span>
        <span class="caption">和{{ name }}聊的内容只属于你自己：记忆和心情可以在设置里翻开查看，也可以随时删除。</span>
      </div>
      <a
        href="https://arena.buugoo.com"
        target="_blank"
        rel="noopener noreferrer"
        class="foot__link caption"
      >
        旗下评测站：BuugooArena <Icon name="chevron" :size="13" />
      </a>
    </footer>
  </div>
</template>

<style scoped>
.site { height: 100%; overflow-y: auto; background: var(--color-canvas); scroll-behavior: smooth; }
.wrap { width: 100%; max-width: 1180px; margin: 0 auto; padding-left: 32px; padding-right: 32px; }
/* 小标题：普通大小写的灰字（不用字母全大写加字距） */
.eyebrow { margin: 0 0 14px; color: var(--color-text-secondary); font-size: 16px; font-weight: 400; letter-spacing: 0; text-transform: none; }

/* 导航 */
.nav { position: sticky; top: 0; z-index: 10; background: rgba(250, 249, 245, .9); backdrop-filter: saturate(1.4) blur(10px); border-bottom: 1px solid var(--color-divider); }
.nav__inner { display: flex; align-items: center; gap: 24px; max-width: 1180px; height: 72px; margin: 0 auto; padding: 0 32px; }
.brand { display: inline-flex; align-items: center; gap: 9px; color: var(--color-ink); text-decoration: none; }
.brand strong { font-family: var(--font-display); font-size: 22px; font-weight: 500; letter-spacing: -.01em; }
.nav__links { display: flex; gap: 4px; margin-left: 12px; }
.nav__links button { padding: 8px 12px; border-radius: var(--radius-md); color: var(--color-ink); font-size: 15px; }
.nav__links button:hover { background: var(--color-hover-wash); color: var(--color-ink); }
.nav__actions { display: flex; gap: 8px; margin-left: auto; }

/* 主视觉 */
.hero { display: grid; grid-template-columns: minmax(0, 1.05fr) minmax(0, 1fr); gap: 56px; align-items: center; padding-top: 104px; padding-bottom: 120px; }
h1 { margin: 0; text-wrap: balance; font-family: var(--font-display); font-size: clamp(38px, 5.6vw, 68px); font-weight: 500; line-height: 1.1; letter-spacing: -0.02em; }
.lede { max-width: 32em; margin: 24px 0 32px; color: var(--color-text-secondary); font-size: 20px; line-height: 1.65; }
.cta { display: flex; flex-wrap: wrap; gap: 12px; }
.note { margin: 16px 0 0; }
.hero__art { max-width: 540px; justify-self: end; width: 100%; }

/* 宣言 */
.manifesto { padding-top: 72px; padding-bottom: 96px; border-top: 1px solid var(--color-divider); text-align: center; }
.manifesto p { margin: 0 auto; max-width: 820px; text-wrap: balance; font-family: var(--font-display); font-size: clamp(26px, 3.4vw, 40px); font-weight: 500; line-height: 1.4; letter-spacing: -.01em; }
.nowrap { white-space: nowrap; }
.manifesto em { font-style: normal; background: linear-gradient(transparent 62%, var(--illo-sand) 62%); padding: 0 2px; }

/* 功能 */
.features { display: grid; gap: 40px; padding-top: 24px; padding-bottom: 40px; }
.feature { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 72px; align-items: center; padding: 40px 0; }
.feature.is-flip .feature__art { order: 2; }
.feature__art { padding: 36px; border-radius: var(--radius-card); background: var(--color-card); }
.feature__art :deep(.illo) { max-width: 420px; margin: 0 auto; }
h2 { margin: 0 0 16px; text-wrap: balance; font-family: var(--font-display); font-size: clamp(30px, 3.6vw, 48px); font-weight: 500; line-height: 1.15; letter-spacing: -0.015em; }
.feature__body { margin: 0 0 20px; color: var(--color-text-secondary); font-size: 17px; line-height: 1.8; }
.feature ul { display: grid; gap: 10px; margin: 0; padding: 0; list-style: none; }
.feature li { display: flex; align-items: center; gap: 8px; font-size: 15px; }
.feature li :deep(svg) { flex-shrink: 0; color: var(--color-accent); }

/* 对话示例 */
/* 整行的米色色带，把一段内容和上下分开 */
.band { margin: 40px 0; border-top: 1px solid var(--color-divider); border-bottom: 1px solid var(--color-divider); background: var(--color-canvas-soft); }
.demo-section { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 72px; align-items: center; padding-top: 112px; padding-bottom: 112px; }
.demo { display: flex; flex-direction: column; gap: 9px; padding: 32px 28px; border-radius: var(--radius-card); background: var(--color-canvas); }
.bubble { align-self: flex-start; max-width: 82%; margin: 0; padding: 10px 14px; border-radius: 18px 18px 18px 6px; background: var(--color-card-strong); font-size: 15px; line-height: 1.55; }
.bubble--me { align-self: flex-end; border-radius: 18px 18px 6px 18px; background: var(--color-accent); color: #fff; }
.demo__tool { display: inline-flex; align-items: center; gap: 5px; margin: 4px 0 0; color: var(--color-muted); font-size: 12px; }
.demo-section.is-in .bubble, .demo-section.is-in .demo__tool { animation: pop-in .5s var(--ease-editorial) both; }
.demo-section.is-in .bubble:nth-child(2) { animation-delay: .3s; } .demo-section.is-in .bubble:nth-child(3) { animation-delay: .7s; }
.demo-section.is-in .bubble:nth-child(4) { animation-delay: 1.1s; } .demo-section.is-in .bubble:nth-child(5) { animation-delay: 1.5s; }
.demo-section.is-in .demo__tool { animation-delay: 1.9s; }
@keyframes pop-in { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: none; } }

/* 怎么开始 */
.steps { padding-top: 96px; padding-bottom: 96px; text-align: center; }
.steps ol { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; margin: 40px 0 0; padding: 0; list-style: none; text-align: left; }
.steps li { padding: 32px; border-radius: var(--radius-card); background: var(--color-card); }
.steps__n { display: inline-grid; place-items: center; width: 40px; height: 40px; border: 1.5px solid var(--color-ink); border-radius: 50%; font-family: var(--font-display); font-size: 18px; font-weight: 500; }
.steps li:nth-child(1) .steps__n { background: var(--illo-mist); }
.steps li:nth-child(2) .steps__n { background: var(--illo-sand); }
.steps li:nth-child(3) .steps__n { background: var(--illo-lavender); }
.steps h3 { margin: 20px 0 8px; font-size: 21px; font-weight: 500; }
.steps li p { margin: 0; color: var(--color-text-secondary); font-size: 15px; line-height: 1.7; }

/* 旗下产品 */
.ecosystem { padding-top: 96px; padding-bottom: 64px; border-top: 1px solid var(--color-divider); scroll-margin-top: 72px; }
.ecosystem__head { max-width: 720px; margin: 0 auto 44px; text-align: center; }
.ecosystem__intro { margin: 0; color: var(--color-text-secondary); font-size: 17px; line-height: 1.75; }
.arena-card { display: grid; grid-template-columns: minmax(0, .92fr) minmax(0, 1.15fr); gap: 48px; align-items: center; padding: 48px; border-radius: var(--radius-card); background: var(--color-card); }
.arena-card__art { width: 100%; max-width: 400px; margin: 0 auto; }
.arena-card__body { min-width: 0; }
.arena-card__meta { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; margin-bottom: 12px; }
.arena-card__name { font-family: var(--font-display); font-size: 20px; font-weight: 500; color: var(--color-ink); letter-spacing: -.01em; }
.arena-card__domain { font-family: var(--font-mono); font-size: 13px; color: var(--color-muted); }
.arena-card__body h3 { margin: 0 0 12px; font-family: var(--font-display); font-size: clamp(24px, 2.4vw, 32px); font-weight: 500; line-height: 1.25; letter-spacing: -.01em; }
.arena-card__desc { margin: 0 0 22px; color: var(--color-text-secondary); font-size: 15px; line-height: 1.78; }
.arena-cols { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-bottom: 22px; }
.arena-col { display: block; padding: 16px 18px; border: 1px solid var(--color-divider); border-radius: var(--radius-lg); background: var(--color-canvas); text-decoration: none; transition: border-color var(--motion-fast) var(--ease-editorial), background-color var(--motion-fast) var(--ease-editorial), transform var(--motion-fast) var(--ease-editorial); }
.arena-col:hover { border-color: var(--color-hairline-strong); background: var(--color-surface); transform: translateY(-1px); }
.arena-col__top { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
.arena-col__num { font-family: var(--font-mono); font-size: 12px; color: var(--color-accent); font-weight: 600; }
.arena-col__top strong { font-size: 15px; font-weight: 600; color: var(--color-ink); }
.arena-col__arrow { margin-left: auto; flex-shrink: 0; color: var(--color-muted); transition: transform var(--motion-fast) var(--ease-editorial), color var(--motion-fast) var(--ease-editorial); }
.arena-col:hover .arena-col__arrow { color: var(--color-ink); transform: translateX(2px); }
.arena-col p { margin: 0; color: var(--color-text-secondary); font-size: 13px; line-height: 1.6; }
.arena-points { display: grid; gap: 9px; margin: 0 0 26px; padding: 0; list-style: none; }
.arena-points li { display: flex; align-items: flex-start; gap: 8px; font-size: 14px; line-height: 1.55; color: var(--color-ink); }
.arena-points li :deep(svg) { margin-top: 3px; flex-shrink: 0; color: var(--color-accent); }
.arena-actions { display: flex; flex-wrap: wrap; gap: 12px; }

/* 结尾 */
.final { padding-top: 40px; padding-bottom: 72px; }
.final__card { display: grid; grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr); align-items: center; gap: 32px; padding: 64px 72px; border-radius: var(--radius-card); background: var(--color-surface-dark); color: var(--color-on-dark); overflow: hidden; }
.final h2 { color: var(--color-on-dark); }
.final p { margin: 0 0 28px; color: var(--color-on-dark-soft); font-size: 17px; line-height: 1.7; }
.final__btn { border-color: var(--color-on-dark); background: var(--color-on-dark); color: var(--color-ink); }
.final__btn:hover { background: #fff; }
.final__art { max-width: 340px; justify-self: end; width: 100%; }

.foot { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding-top: 28px; padding-bottom: 40px; border-top: 1px solid var(--color-divider); }
.foot__left { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 16px; min-width: 0; }
.foot__link { display: inline-flex; align-items: center; gap: 3px; color: var(--color-text-secondary); white-space: nowrap; transition: color var(--motion-fast) var(--ease-editorial); }
.foot__link:hover { color: var(--color-ink); }
.brand--small { gap: 7px; font-family: var(--font-display); font-size: 16px; }

/* 滚动淡入 */
.reveal { opacity: 0; transform: translateY(18px); transition: opacity .7s var(--ease-editorial), transform .7s var(--ease-editorial); }
.reveal.is-in { opacity: 1; transform: none; }
@media (prefers-reduced-motion: reduce) {
  .reveal { opacity: 1; transform: none; transition: none; }
  .demo-section.is-in .bubble, .demo-section.is-in .demo__tool { animation: none; }
  .arena-col, .arena-col__arrow, .foot__link { transition: none; }
  .arena-col:hover, .arena-col:hover .arena-col__arrow { transform: none; }
}

@media (max-width: 900px) {
  .wrap, .nav__inner { padding-left: 20px; padding-right: 20px; }
  .nav__links { display: none; }
  .hero { grid-template-columns: minmax(0, 1fr); gap: 32px; padding-top: 44px; padding-bottom: 64px; }
  .manifesto { padding-top: 48px; padding-bottom: 56px; }
  .eyebrow { font-size: 15px; }
  .lede { font-size: 17px; }
  .hero__art { justify-self: center; max-width: 420px; }
  .feature, .demo-section { grid-template-columns: minmax(0, 1fr); gap: 28px; padding: 24px 0; }
  .feature.is-flip .feature__art { order: 0; }
  .feature__art { padding: 20px; }
  .demo-section { padding-top: 56px; padding-bottom: 56px; }
  .steps { padding-top: 56px; padding-bottom: 56px; }
  .steps ol { grid-template-columns: minmax(0, 1fr); }
  .ecosystem { padding-top: 56px; padding-bottom: 36px; }
  .ecosystem__head { margin-bottom: 28px; text-align: left; }
  .arena-card { grid-template-columns: minmax(0, 1fr); gap: 28px; padding: 28px 22px; }
  .arena-card__art { max-width: 320px; }
  .arena-cols { grid-template-columns: minmax(0, 1fr); }
  .final__card { grid-template-columns: minmax(0, 1fr); padding: 40px 28px; }
  .final__art { justify-self: center; max-width: 260px; }
  .br-wide { display: none; }
}
@media (max-width: 480px) {
  .lede { font-size: 16px; }
  .cta .btn, .arena-actions .btn { flex: 1; justify-content: center; }
  .arena-card { padding: 22px 16px; }
  .nav__actions .btn { padding: 0 12px; }
}
</style>
