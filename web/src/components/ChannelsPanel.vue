<script setup lang="ts">
// 设置 → 聊天软件：微信扫码连接自己的微信；QQ / Telegram 生成绑定码，在聊天软件里私聊发给布谷就绑好了。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, ApiError, type Binding, type ChannelInfo, type WeixinUser } from '../api'
import { shortWhen } from '../format'
import { session } from '../session'
import { toast } from '../toast'

const name = computed(() => session.meta?.name ?? '布谷')
const chans = ref<ChannelInfo[]>([])
const bindings = ref<Binding[]>([])
const weixin = ref<WeixinUser | null>(null)
const code = ref('')
const expires = ref(0)
const now = ref(Date.now())
const starting = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

const left = computed(() => Math.max(0, Math.round((expires.value - now.value) / 1000)))
const leftText = computed(() => `${Math.floor(left.value / 60)}:${String(left.value % 60).padStart(2, '0')}`)
const hasWeixin = computed(() => chans.value.some((c) => c.name === 'weixin'))
// 用绑定码的聊天软件（微信是扫码连接的）。
const codeChans = computed(() => chans.value.filter((c) => c.name !== 'weixin'))
const codeBindings = computed(() => bindings.value.filter((b) => b.channel !== 'weixin'))
const login = computed(() => weixin.value?.login ?? null)
const scanning = computed(() => !weixin.value?.account && !!login.value && (login.value.status === 'wait' || login.value.status === 'scaned'))

async function load() {
  try {
    const r = await api.channels()
    chans.value = r.channels
    bindings.value = r.bindings
    weixin.value = r.weixin
  } catch {
    // 读取失败时不显示这个面板。
  }
}

async function newCode() {
  try {
    const r = await api.bindCode()
    code.value = r.code
    expires.value = new Date(r.expires_at).getTime()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '生成失败')
  }
}

async function unbind(b: Binding) {
  try {
    await api.unbind(b.id)
    bindings.value = bindings.value.filter((x) => x.id !== b.id)
    toast(`已解绑${b.label}`)
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '解绑失败')
  }
}

async function connectWeixin() {
  starting.value = true
  try {
    const l = await api.weixinLogin()
    weixin.value = { account: null, login: l }
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '获取二维码失败')
  } finally {
    starting.value = false
  }
}

async function disconnectWeixin() {
  try {
    await api.weixinLogout()
    toast('已断开微信')
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  }
}

function howTo(c: ChannelInfo) {
  if (c.name === 'qq') return c.account ? `加 QQ ${c.account} 为好友，私聊发送绑定码` : '加机器人的 QQ 为好友，私聊发送绑定码'
  return c.account ? `在 Telegram 里搜索 ${c.account}，私聊发送绑定码` : '在 Telegram 里找到机器人，私聊发送绑定码'
}

onMounted(() => {
  load()
  timer = setInterval(() => {
    now.value = Date.now()
    // 扫码中、或者绑定码还有效：刷新看看是不是连好了。
    if ((scanning.value && now.value % 2000 < 1000) || (code.value && now.value % 5000 < 1000)) load()
    if (code.value && left.value === 0) code.value = ''
  }, 1000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <section v-if="chans.length" id="channels" class="panel panel--inset">
    <h2>聊天软件</h2>
    <p class="muted intro">
      在{{ chans.map((c) => c.label).join('、') }}里也能找{{ name }}聊天，网页上聊过的事{{ name }}都记得，聊天记录在网页的对话列表里也能看到。
      提醒和定时任务也会发到那里。
    </p>

    <div v-if="hasWeixin" class="block">
      <div class="block__head">
        <span class="dot" :class="{ 'is-on': weixin?.account }" />
        <span class="block__name">微信</span>
        <span class="caption">
          {{ weixin?.account ? `已连接 · ${shortWhen(weixin.account.login_at)}` : '未连接' }}
        </span>
        <span class="spacer" />
        <button v-if="weixin?.account" type="button" class="btn btn-ghost btn-sm" @click="disconnectWeixin">断开</button>
      </div>
      <p v-if="weixin?.account?.error" class="caption err">收消息出错：{{ weixin.account.error }}</p>
      <template v-if="!weixin?.account">
        <p v-if="weixin?.notice" class="caption notice">{{ weixin.notice }}</p>
        <Transition name="fade" mode="out-in">
          <div v-if="scanning && login?.qrcode" key="qr" class="wx">
            <img :src="login.qrcode" alt="微信登录二维码" width="180" height="180" />
            <div class="wx__how">
              <p class="caption">{{ login.status === 'scaned' ? '已扫码，请在手机上确认' : '用自己的手机微信扫一扫' }}</p>
              <p class="caption">连接后，在微信里找到 ClawBot 的对话就能和{{ name }}聊天。手机微信需要是最新版。</p>
            </div>
          </div>
          <div v-else key="btn" class="wx-btn">
            <p v-if="login?.status === 'expired'" class="caption">二维码过期了，重新获取一个</p>
            <p v-else-if="login?.status === 'error'" class="caption err">{{ login.error }}</p>
            <button class="btn btn-primary" :disabled="starting" @click="connectWeixin">{{ starting ? '正在获取二维码' : '扫码连接微信' }}</button>
          </div>
        </Transition>
      </template>
      <p v-else class="caption">在微信里找到 ClawBot 的对话，直接聊天就行；发「新对话」换个话题。</p>
    </div>

    <div v-if="codeChans.length" class="block">
      <p class="block__name">{{ codeChans.map((c) => c.label).join(' / ') }}</p>
      <TransitionGroup v-if="codeBindings.length" tag="ul" name="list" class="list">
        <li v-for="b in codeBindings" :key="b.id" class="item">
          <span class="badge-pill">{{ b.label }}</span>
          <span class="item__name">{{ b.peer_name || '已绑定' }}</span>
          <span class="caption">{{ b.last_message_at ? `最近聊天 ${shortWhen(b.last_message_at)}` : '还没聊过' }}</span>
          <span class="spacer" />
          <button type="button" class="btn btn-ghost btn-sm" @click="unbind(b)">解绑</button>
        </li>
      </TransitionGroup>

      <Transition name="fade" mode="out-in">
        <div v-if="code" key="code" class="code">
          <div class="code__value" aria-label="绑定码">{{ code }}</div>
          <div class="code__how">
            <p v-for="c in codeChans" :key="c.name" class="caption">{{ c.label }}：{{ howTo(c) }}</p>
            <p class="caption">{{ leftText }} 后失效，只能用一次</p>
          </div>
        </div>
        <div v-else key="btn" class="actions">
          <button class="btn" @click="newCode">{{ codeBindings.length ? '再绑定一个' : '生成绑定码' }}</button>
        </div>
      </Transition>
    </div>
  </section>
</template>

<style scoped>
.intro { margin: 0 0 14px; }
.block { padding: 14px 0; border-top: 1px solid var(--color-divider); }
.block__head { display: flex; align-items: center; gap: 8px; min-height: 28px; }
.block__name { margin: 0 0 6px; color: var(--color-ink); font-size: 14px; font-weight: 600; }
.block__head .block__name { margin: 0; }
.block > .caption { margin: 8px 0 0; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--color-hairline-strong); transition: background-color .3s ease; }
.dot.is-on { background: var(--color-success); box-shadow: 0 0 0 3px var(--color-success-tint); }
.err { color: var(--color-danger); }
.notice { color: var(--color-warning, var(--color-text-secondary)); }
.wx { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; margin-top: 12px; }
.wx img { border: 1px solid var(--color-divider); border-radius: var(--radius-md); background: #fff; }
.wx__how { display: grid; gap: 6px; max-width: 280px; }
.wx__how .caption, .wx-btn .caption { margin: 0; }
.wx-btn { display: grid; justify-items: start; gap: 8px; margin-top: 12px; }
.list { display: grid; margin: 0 0 14px; padding: 0; list-style: none; }
.item { display: flex; align-items: center; gap: 10px; min-height: 44px; border-top: 1px solid var(--color-divider); font-size: 14px; }
.item__name { color: var(--color-ink); }
.spacer { flex: 1; }
.btn-sm { min-height: 28px; padding: 0 10px; }
.code { display: flex; align-items: center; gap: 20px; flex-wrap: wrap; padding: 16px; border-radius: var(--radius-lg); background: var(--color-canvas-soft); }
.code__value { font-family: var(--font-mono); font-size: 34px; font-weight: 600; letter-spacing: .18em; color: var(--color-ink); }
.code__how { display: grid; gap: 4px; }
.code__how .caption { margin: 0; }
</style>
