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
    error.value = e instanceof ApiError ? e.message : '登录没有成功，请再试一次。'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth">
    <div class="brand">
      <img src="/logo.png" alt="" width="40" height="40" />
      <strong>BUUGOO</strong>
      <em>布谷</em>
    </div>
    <h1>{{ tab === 'login' ? '欢迎回来。' : '认识一下。' }}</h1>
    <p class="muted lede">记得你说过的事，也会在合适的时候问候你。</p>

    <form class="panel panel--inset form" @submit.prevent="submit">
      <div v-if="canRegister" class="type-switch tabs" role="tablist">
        <button type="button" role="tab" :aria-selected="tab === 'login'" :class="{ 'is-active': tab === 'login' }" @click="tab = 'login'">登录</button>
        <button type="button" role="tab" :aria-selected="tab === 'register'" :class="{ 'is-active': tab === 'register' }" @click="tab = 'register'">注册</button>
      </div>

      <div v-if="isMulti" class="field">
        <label class="field__label" for="username">用户名</label>
        <input id="username" v-model="username" class="text-input" autocomplete="username" required />
      </div>
      <div v-if="tab === 'register'" class="field">
        <label class="field__label" for="display">怎么称呼你</label>
        <input id="display" v-model="displayName" class="text-input" maxlength="32" />
        <p class="field__hint">可以不填，{{ meta?.name ?? '布谷' }}会用这个名字称呼你。</p>
      </div>
      <div class="field">
        <label class="field__label" for="password">{{ isMulti ? '密码' : '访问密码' }}</label>
        <input
          id="password"
          v-model="password"
          class="text-input"
          type="password"
          :autocomplete="tab === 'register' ? 'new-password' : 'current-password'"
          required
        />
        <p v-if="tab === 'register'" class="field__hint">至少 8 位。</p>
      </div>
      <div v-if="tab === 'register' && needInvite" class="field">
        <label class="field__label" for="invite">邀请码</label>
        <input id="invite" v-model="inviteCode" class="text-input" required autocapitalize="characters" />
      </div>

      <p v-if="error" class="form-flash form-flash--error" role="alert">{{ error }}</p>
      <button class="btn btn-primary btn-lg submit" :disabled="busy">
        {{ busy ? '请稍候' : tab === 'login' ? '进入' : '注册并进入' }}
      </button>
    </form>
  </main>
</template>

<style scoped>
.auth { min-height: 100%; display: flex; flex-direction: column; align-items: center; padding: 12vh var(--mobile-gutter) 40px; }
.brand { display: inline-flex; align-items: center; gap: var(--space-xs); margin-bottom: 36px; }
.brand strong { font-family: var(--font-display); font-size: 20px; font-weight: 500; line-height: 1; }
.brand em { font-style: normal; font-size: 14px; color: var(--color-text-secondary); }
h1 { margin: 0; font-size: 36px; line-height: 1.2; letter-spacing: -0.015em; text-align: center; }
.lede { margin: 12px 0 28px; font-size: 17px; line-height: 1.7; text-align: center; }
.form { width: 100%; max-width: 380px; }
.tabs { margin-bottom: 20px; }
.submit { width: 100%; margin-top: 4px; }
</style>
