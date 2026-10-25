<script setup lang="ts">
// 设置 → 聊天软件：把微信 / QQ / Telegram 绑定到自己的账号。生成绑定码，在聊天软件里私聊发给布谷就绑好了。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, ApiError, type Binding, type ChannelInfo } from '../api'
import { shortWhen } from '../format'
import { session } from '../session'
import { toast } from '../toast'

const name = computed(() => session.meta?.name ?? '布谷')
const chans = ref<ChannelInfo[]>([])
const bindings = ref<Binding[]>([])
const code = ref('')
const expires = ref(0)
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined

const left = computed(() => Math.max(0, Math.round((expires.value - now.value) / 1000)))
const leftText = computed(() => `${Math.floor(left.value / 60)}:${String(left.value % 60).padStart(2, '0')}`)

async function load() {
  try {
    const r = await api.channels()
    chans.value = r.channels
    bindings.value = r.bindings
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

function howTo(c: ChannelInfo) {
  switch (c.name) {
    case 'weixin':
      return `在微信里打开${name.value}的对话（ClawBot），发送绑定码`
    case 'qq':
      return c.account ? `加 QQ ${c.account} 为好友，私聊发送绑定码` : '加机器人的 QQ 为好友，私聊发送绑定码'
    default:
      return c.account ? `在 Telegram 里搜索 ${c.account}，私聊发送绑定码` : '在 Telegram 里找到机器人，私聊发送绑定码'
  }
}

onMounted(() => {
  load()
  timer = setInterval(() => {
    now.value = Date.now()
    // 绑定码用掉了：刷新列表看看是不是绑好了。
    if (code.value && now.value % 5000 < 1000) load()
    if (code.value && left.value === 0) code.value = ''
  }, 1000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <section v-if="chans.length" id="channels" class="panel panel--inset">
    <h2>聊天软件</h2>
    <p class="muted intro">在{{ chans.map((c) => c.label).join('、') }}里也能找{{ name }}聊天，网页上聊过的事{{ name }}都记得。到时间的提醒也会发到那里。</p>

    <TransitionGroup v-if="bindings.length" tag="ul" name="list" class="list">
      <li v-for="b in bindings" :key="b.id" class="item">
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
          <p v-for="c in chans" :key="c.name" class="caption">{{ c.label }}：{{ howTo(c) }}</p>
          <p class="caption">{{ leftText }} 后失效，只能用一次</p>
        </div>
      </div>
      <div v-else key="btn" class="actions">
        <button class="btn btn-primary" @click="newCode">{{ bindings.length ? '再绑定一个' : '生成绑定码' }}</button>
      </div>
    </Transition>
  </section>
</template>

<style scoped>
.intro { margin: 0 0 14px; }
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
