<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError } from '../api'
import { applySession, session } from '../session'

const router = useRouter()
const route = useRoute()

const meta = computed(() => session.meta)
const isMulti = computed(() => meta.value?.mode === 'multi')
const canRegister = computed(() => isMulti.value && meta.value?.registration !== 'closed')
const needInvite = computed(() => meta.value?.registration === 'invite')

const tab = ref<'login' | 'register'>('login')
const username = ref('')
const password = ref('')
const displayName = ref('')
const inviteCode = ref(typeof route.query.invite === 'string' ? route.query.invite : '')
const error = ref('')
const busy = ref(false)

if (inviteCode.value && canRegister.value) tab.value = 'register'

async function submit() {
  error.value = ''
  busy.value = true
  try {
    const s =
      tab.value === 'login'
        ? await api.login(username.value, password.value)
        : await api.register({
            username: username.value,
            password: password.value,
            display_name: displayName.value,
            invite_code: inviteCode.value,
          })
    applySession(s)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
    router.replace(next)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '登录失败'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="login">
    <div class="hero">
      <img src="/favicon.svg" alt="" width="64" height="64" />
      <h1>{{ meta?.name ?? '布谷' }}</h1>
      <p class="muted">记得你的 AI 朋友</p>
    </div>

    <form class="card" @submit.prevent="submit">
      <div v-if="canRegister" class="tabs">
        <button type="button" :class="{ active: tab === 'login' }" @click="tab = 'login'">登录</button>
        <button type="button" :class="{ active: tab === 'register' }" @click="tab = 'register'">注册</button>
      </div>

      <div v-if="isMulti" class="field">
        <label for="username">用户名</label>
        <input id="username" v-model="username" class="input" autocomplete="username" required />
      </div>
      <div v-if="tab === 'register'" class="field">
        <label for="display">怎么称呼你（可选）</label>
        <input id="display" v-model="displayName" class="input" maxlength="32" />
      </div>
      <div class="field">
        <label for="password">{{ isMulti ? '密码' : '访问密码' }}</label>
        <input
          id="password"
          v-model="password"
          class="input"
          type="password"
          :autocomplete="tab === 'register' ? 'new-password' : 'current-password'"
          required
        />
      </div>
      <div v-if="tab === 'register' && needInvite" class="field">
        <label for="invite">邀请码</label>
        <input id="invite" v-model="inviteCode" class="input" required autocapitalize="characters" />
      </div>

      <p v-if="error" class="error">{{ error }}</p>
      <button class="btn submit" :disabled="busy">
        {{ busy ? '请稍候…' : tab === 'login' ? '进入' : '注册并进入' }}
      </button>
    </form>
  </main>
</template>

<style scoped>
.login { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 24px 16px; }
.hero { text-align: center; margin-bottom: 20px; }
.hero h1 { margin: 8px 0 0; font-size: 26px; }
.hero p { margin: 4px 0 0; }
form { width: 100%; max-width: 360px; }
.tabs { display: flex; gap: 4px; background: var(--surface-2); border-radius: 10px; padding: 3px; margin-bottom: 16px; }
.tabs button { flex: 1; border: 0; background: transparent; padding: 6px; border-radius: 8px; cursor: pointer; color: var(--muted); }
.tabs button.active { background: var(--surface); color: var(--text); font-weight: 600; box-shadow: var(--shadow); }
.submit { width: 100%; margin-top: 4px; padding: 10px; }
</style>
