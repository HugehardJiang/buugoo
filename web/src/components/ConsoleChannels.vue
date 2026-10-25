<script setup lang="ts">
// 管理后台 → 聊天软件：各个聊天软件的连接状态，个人微信扫码登录，QQ（NapCat）的连接方法。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError } from '../api'
import { toast } from '../toast'

interface Status {
  name: 'weixin' | 'qq' | 'telegram'
  label: string
  connected: boolean
  account?: string
  detail?: string
}
interface WeixinLogin {
  qrcode?: string
  status: 'wait' | 'scaned' | 'confirmed' | 'expired' | 'error'
  error?: string
}
interface Resp {
  channels: Status[]
  onebot_path: string
  weixin_login?: WeixinLogin | null
}

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()
const data = ref<Resp | null>(null)
const error = ref('')
const starting = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

const login = computed(() => data.value?.weixin_login ?? null)
const scanning = computed(() => login.value && (login.value.status === 'wait' || login.value.status === 'scaned'))
const wsURL = computed(() => {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}${data.value?.onebot_path ?? '/onebot/v11/ws'}`
})

async function load() {
  try {
    data.value = await props.call<Resp>('GET', '/channels')
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function startLogin() {
  starting.value = true
  try {
    const l = await props.call<WeixinLogin>('POST', '/channels/weixin/login')
    if (data.value) data.value.weixin_login = l
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '申请二维码失败')
  } finally {
    starting.value = false
  }
}

async function logout() {
  try {
    await props.call('POST', '/channels/weixin/logout')
    toast('已退出微信登录')
    load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast('已复制')
  } catch {
    toast(text)
  }
}

onMounted(() => {
  load()
  // 扫码时更快地刷新状态。
  timer = setInterval(() => load(), 3000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <section class="panel panel--inset">
    <h2>聊天软件</h2>
    <p class="muted intro">
      让用户在微信、QQ、Telegram 里也能找布谷聊天，只处理私聊。用户需要先在网页「设置 → 聊天软件」生成绑定码发给机器人，
      绑定后和网页共用同一份记忆和心情。在「系统设置 → 聊天软件」中开启，修改后需要重启服务。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <p v-else-if="data && !data.channels.length" class="form-flash">还没有开启任何聊天软件。到「系统设置 → 聊天软件」里打开需要的，保存后重启服务。</p>

    <div v-for="c in data?.channels ?? []" :key="c.name" class="ch">
      <div class="ch__head">
        <span class="dot" :class="{ 'is-on': c.connected }" />
        <span class="ch__name">{{ c.label }}</span>
        <span class="caption">{{ c.connected ? `已连接${c.account ? ' · ' + c.account : ''}` : '未连接' }}</span>
      </div>
      <p v-if="c.detail" class="caption ch__detail">{{ c.detail }}</p>

      <template v-if="c.name === 'weixin'">
        <div v-if="!c.connected" class="wx">
          <Transition name="fade" mode="out-in">
            <div v-if="scanning && login?.qrcode" key="qr" class="wx__qr">
              <img :src="login.qrcode" alt="微信登录二维码" width="200" height="200" />
              <p class="caption">{{ login.status === 'scaned' ? '已扫码，请在手机上确认登录' : '用手机微信扫码登录（手机微信需要是最新版，并有 ClawBot 插件）' }}</p>
            </div>
            <div v-else key="btn">
              <p v-if="login?.status === 'expired'" class="caption">二维码过期了，重新获取一个</p>
              <p v-else-if="login?.status === 'error'" class="caption err">{{ login.error }}</p>
              <button class="btn btn-primary" :disabled="starting" @click="startLogin">{{ starting ? '正在获取二维码' : '扫码登录微信' }}</button>
            </div>
          </Transition>
        </div>
        <div v-else class="actions">
          <button class="btn btn-ghost" @click="logout">退出登录</button>
        </div>
      </template>

      <template v-else-if="c.name === 'qq'">
        <ol class="caption steps">
          <li>用一个 QQ 小号登录 <a href="https://napneko.github.io/" target="_blank" rel="noopener" class="link">NapCat</a>（非官方协议，有一定封号风险）</li>
          <li>在 NapCat 的「网络配置」中新建「WebSocket 客户端」，地址填下面的地址，Token 填系统设置里的「QQ 连接令牌」，消息格式选 array</li>
          <li>连接成功后这里会显示「已连接」；其他人加这个 QQ 为好友，私聊发绑定码即可</li>
        </ol>
        <div class="cmd">
          <code>{{ wsURL }}</code>
          <button class="btn" @click="copy(wsURL)">复制</button>
        </div>
        <p class="caption">NapCat 和布谷不在同一台机器时，把地址换成布谷的内网地址；经过宝塔反向代理时，需要在代理配置里允许 WebSocket（见 docs/baota.md）。</p>
      </template>

      <template v-else-if="c.name === 'telegram'">
        <p class="caption">用户在 Telegram 里搜索 {{ c.account || '你的机器人' }}，私聊发送绑定码即可。国内服务器需要在系统设置里配置 Telegram 代理。</p>
      </template>
    </div>
  </section>
</template>

<style scoped>
.intro { margin: 0 0 16px; }
.ch { padding: 16px 0; border-top: 1px solid var(--color-divider); }
.ch__head { display: flex; align-items: center; gap: 8px; }
.ch__name { color: var(--color-ink); font-size: 15px; font-weight: 600; }
.ch__detail { margin: 6px 0 0 16px; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--color-hairline-strong); transition: background-color .3s ease; }
.dot.is-on { background: var(--color-success); box-shadow: 0 0 0 3px var(--color-success-tint); }
.wx { margin: 12px 0 0 16px; }
.wx__qr { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
.wx__qr img { border: 1px solid var(--color-divider); border-radius: var(--radius-md); background: #fff; }
.wx__qr .caption { max-width: 260px; }
.err { color: var(--color-danger); }
.actions { margin: 10px 0 0 16px; }
.steps { margin: 10px 0 10px 16px; padding-left: 18px; line-height: 1.8; }
.cmd { display: flex; align-items: center; gap: 8px; margin: 0 0 8px 16px; }
.cmd code { flex: 1; min-width: 0; overflow-x: auto; padding: 8px 10px; border-radius: var(--radius-md); background: var(--color-canvas-soft); white-space: nowrap; }
.ch > .caption { margin: 8px 0 0 16px; }
.link { color: var(--color-accent-active); text-decoration: underline; text-underline-offset: 3px; }
</style>
