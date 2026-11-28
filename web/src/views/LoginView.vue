<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from '../Icon.vue'
import { api, ApiError } from '../api'
import { applySession, session } from '../session'

const router = useRouter()
const route = useRoute()

const meta = computed(() => session.meta)
const siteName = computed(() => meta.value?.name?.trim() || '布谷')
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

if ((inviteCode.value || route.query.tab === 'register') && canRegister.value) tab.value = 'register'

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
    <div class="auth-bg" aria-hidden="true">
      <div class="auth-bg__cloud auth-bg__cloud--a" />
      <div class="auth-bg__cloud auth-bg__cloud--b" />
      <div class="auth-bg__cloud auth-bg__cloud--c" />
    </div>

    <div v-if="isMulti" class="auth-top">
      <RouterLink to="/welcome" class="back">
        <Icon name="chevron" :size="14" class="back__ico" />
        <span>了解{{ siteName }}</span>
      </RouterLink>
    </div>

    <div class="auth-stage">
      <div class="brand">
        <img src="/logo.png" :alt="`${siteName}标志`" width="30" height="30" />
        <strong>{{ siteName }}</strong>
      </div>

      <h1 class="auth-title">{{ tab === 'login' ? '欢迎回来' : '认识一下' }}</h1>
      <p class="muted lede">记得你说过的事，也会在合适的时候问候你。</p>

      <form class="auth-card" @submit.prevent="submit">
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
          <p class="field__hint">可以不填，{{ siteName }}会用这个名字称呼你。</p>
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
    </div>
  </main>
</template>

<style scoped>
.auth {
  position: relative;
  min-height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  overflow-x: hidden;
  padding: 76px var(--mobile-gutter) 48px;
  background: var(--color-canvas);
}

/* 与新版首页一脉相承的布谷橄榄/青雾大气径向光晕 */
.auth-bg {
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 0;
  overflow: hidden;
  background:
    radial-gradient(110% 85% at 50% 42%, rgba(250, 249, 245, .72) 0%, rgba(250, 249, 245, .2) 64%, transparent 100%),
    linear-gradient(
      180deg,
      rgba(188, 209, 202, .44) 0%,
      rgba(143, 166, 127, .24) 44%,
      rgba(188, 209, 202, .24) 76%,
      var(--color-canvas) 100%
    );
}

.auth-bg__cloud {
  position: absolute;
  border-radius: 50%;
  filter: blur(88px);
  animation: auth-cloud-drift 18s ease-in-out infinite alternate;
}

.auth-bg__cloud--a {
  top: -14%;
  left: -10%;
  width: 56vw;
  height: 52vh;
  background: radial-gradient(circle, rgba(188, 209, 202, .72) 0%, rgba(188, 209, 202, 0) 70%);
}

.auth-bg__cloud--b {
  top: -8%;
  right: -12%;
  width: 58vw;
  height: 56vh;
  background: radial-gradient(circle, rgba(143, 166, 127, .5) 0%, rgba(143, 166, 127, 0) 72%);
  animation-delay: -6s;
}

.auth-bg__cloud--c {
  bottom: -6%;
  left: 18%;
  width: 54vw;
  height: 44vh;
  background: radial-gradient(circle, rgba(227, 218, 204, .72) 0%, rgba(227, 218, 204, 0) 72%);
  animation-delay: -11s;
}

@keyframes auth-cloud-drift {
  0% { transform: translate3d(0, 0, 0) scale(1); }
  100% { transform: translate3d(20px, 14px, 0) scale(1.05); }
}

.auth-top {
  position: fixed;
  top: 20px;
  left: 24px;
  z-index: 2;
}

.back {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 36px;
  padding: 0 15px 0 11px;
  border-radius: 9999px;
  border: 1px solid rgba(20, 20, 19, .12);
  background: rgba(255, 255, 255, .58);
  -webkit-backdrop-filter: blur(12px);
  backdrop-filter: blur(12px);
  color: var(--color-ink);
  font-size: 13.5px;
  font-weight: 500;
  transition: background-color .18s var(--ease-editorial), border-color .18s var(--ease-editorial);
}

.back:hover {
  background: rgba(255, 255, 255, .86);
  border-color: rgba(20, 20, 19, .24);
}

.back__ico {
  transform: rotate(180deg);
  color: var(--color-text-secondary);
}

.auth-stage {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 420px;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 24px;
}

.brand img {
  width: 30px;
  height: 30px;
  object-fit: contain;
}

.brand strong {
  font-family: var(--font-sans);
  font-size: 18px;
  font-weight: 500;
  letter-spacing: -0.02em;
  line-height: 1;
  color: var(--color-ink);
}

.auth-title {
  margin: 0;
  font-family: var(--font-sans);
  font-size: clamp(32px, 4.6vw, 44px);
  font-weight: 500;
  line-height: 1.14;
  letter-spacing: -0.03em;
  text-align: center;
  color: var(--color-ink);
  word-break: normal;
  overflow-wrap: anywhere;
}

.lede {
  margin: 12px 0 30px;
  max-width: 360px;
  font-size: 15.5px;
  line-height: 1.65;
  text-align: center;
}

.auth-card {
  width: 100%;
  padding: 32px 30px 30px;
  border: 0;
  border-radius: 28px;
  background: rgba(255, 255, 255, .9);
  -webkit-backdrop-filter: blur(16px);
  backdrop-filter: blur(16px);
  box-shadow: var(--md-elev-3);
}

.tabs {
  margin-bottom: 22px;
}

.submit {
  width: 100%;
  margin-top: 6px;
}

@media (max-width: 640px) {
  .auth {
    padding: 72px 16px 36px;
  }
  .auth-top {
    top: 14px;
    left: 16px;
  }
  .auth-card {
    padding: 24px 20px 22px;
    border-radius: 28px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .auth-bg__cloud {
    animation: none;
  }
}
</style>
