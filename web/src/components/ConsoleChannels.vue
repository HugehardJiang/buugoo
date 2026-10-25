<script setup lang="ts">
// 管理后台 → 聊天软件：各个聊天软件的连接状态，用户连接的微信（可以断开），QQ（NapCat）的连接方法。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError } from '../api'
import { shortWhen } from '../format'
import { toast } from '../toast'

interface Status {
  name: 'weixin' | 'qq' | 'telegram'
  label: string
  connected: boolean
  account?: string
  detail?: string
}
interface WeixinAccount {
  user_id: number
  account: string
  login_at: string
  error?: string
  legacy?: boolean
  username?: string
  display_name?: string
}
interface Resp {
  channels: Status[]
  onebot_path: string
  weixin_accounts: WeixinAccount[]
}

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()
const data = ref<Resp | null>(null)
const error = ref('')
let timer: ReturnType<typeof setInterval> | undefined

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

async function kick(a: WeixinAccount) {
  try {
    await props.call('POST', `/channels/weixin/${a.user_id}/logout`)
    toast('已断开')
    load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '操作失败')
  }
}

function who(a: WeixinAccount) {
  if (a.legacy) return '旧版本管理员扫码的微信（用绑定码使用）'
  return a.display_name ? `${a.display_name}（${a.username}）` : a.username || `用户 #${a.user_id}`
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
  timer = setInterval(() => load(), 10000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <section class="panel panel--inset">
    <h2>聊天软件</h2>
    <p class="muted intro">
      让用户在微信、QQ、Telegram 里也能找布谷聊天，只处理私聊。微信由每个用户在「设置 → 聊天软件」里扫码连接自己的微信；
      QQ、Telegram 由用户生成绑定码发给机器人。绑定后和网页共用同一份记忆和心情。在「系统设置 → 聊天软件」中开启，修改后需要重启服务。
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
        <TransitionGroup v-if="data?.weixin_accounts.length" tag="ul" name="list" class="accounts">
          <li v-for="a in data.weixin_accounts" :key="a.user_id" class="account">
            <div class="account__main">
              <p class="account__who">{{ who(a) }}</p>
              <p class="caption">连接于 {{ shortWhen(a.login_at) }}<template v-if="a.error"> · <span class="err">收消息出错：{{ a.error }}</span></template></p>
            </div>
            <button type="button" class="btn btn-ghost btn-sm" @click="kick(a)">断开</button>
          </li>
        </TransitionGroup>
        <p class="caption ch__detail">ClawBot 是扫码人自己微信里的机器人，只有扫码的人自己能用，所以每个用户扫自己的码。手机微信需要是最新版。</p>
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
.err { color: var(--color-danger); }
.accounts { display: grid; margin: 10px 0 0 16px; padding: 0; list-style: none; }
.account { display: flex; align-items: center; gap: 10px; padding: 8px 0; border-top: 1px solid var(--color-divider); }
.account__main { flex: 1; min-width: 0; }
.account__who { margin: 0 0 2px; color: var(--color-ink); font-size: 14px; }
.account .caption { margin: 0; }
.btn-sm { min-height: 28px; padding: 0 10px; }
.steps { margin: 10px 0 10px 16px; padding-left: 18px; line-height: 1.8; }
.cmd { display: flex; align-items: center; gap: 8px; margin: 0 0 8px 16px; }
.cmd code { flex: 1; min-width: 0; overflow-x: auto; padding: 8px 10px; border-radius: var(--radius-md); background: var(--color-canvas-soft); white-space: nowrap; }
.ch > .caption { margin: 8px 0 0 16px; }
.link { color: var(--color-accent-active); text-decoration: underline; text-underline-offset: 3px; }
</style>
