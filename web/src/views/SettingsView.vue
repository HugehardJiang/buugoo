<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError, type Invite, type Quota } from '../api'
import { dateTime } from '../format'
import { applySession, logout, session } from '../session'

const router = useRouter()
const quota = ref<Quota | null>(null)
const error = ref('')
const message = ref('')

const isMulti = computed(() => session.meta?.mode === 'multi')
const isAdmin = computed(() => !!session.user?.is_admin)
const canLogout = computed(() => !!session.meta?.password_required)

const oldPw = ref('')
const newPw = ref('')

const invites = ref<Invite[]>([])
const inviteUses = ref(1)
const inviteHours = ref(168)
const inviteNote = ref('')

function fail(e: unknown) {
  error.value = e instanceof ApiError ? e.message : '操作失败'
}

async function load() {
  try {
    const me = await api.me()
    quota.value = me.quota
    session.user = me.user
    if (isMulti.value && me.user.is_admin) invites.value = (await api.invites()).invites
  } catch (e) {
    fail(e)
  }
}

function signOut() {
  logout()
  router.replace('/login')
}

async function signOutAll() {
  try {
    await api.logoutAll()
    signOut()
  } catch (e) {
    fail(e)
  }
}

async function changePassword() {
  error.value = ''
  message.value = ''
  try {
    applySession(await api.changePassword(oldPw.value, newPw.value))
    oldPw.value = newPw.value = ''
    message.value = '密码已修改，其他设备需要重新登录'
  } catch (e) {
    fail(e)
  }
}

async function createInvite() {
  error.value = ''
  try {
    const inv = await api.createInvite({ max_uses: inviteUses.value, ttl_hours: inviteHours.value, note: inviteNote.value })
    invites.value = [inv, ...invites.value]
    inviteNote.value = ''
  } catch (e) {
    fail(e)
  }
}

async function deleteInvite(id: number) {
  try {
    await api.deleteInvite(id)
    invites.value = invites.value.filter((i) => i.id !== id)
  } catch (e) {
    fail(e)
  }
}

function inviteLink(code: string) {
  return `${location.origin}/login?invite=${encodeURIComponent(code)}`
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.value = '已复制'
  } catch {
    message.value = text
  }
}

function limitText(used: number, limit: number) {
  return limit > 0 ? `${used} / ${limit}` : `${used}（不限）`
}

onMounted(load)
</script>

<template>
  <main class="page">
    <h1>设置</h1>
    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="message" class="notice">{{ message }}</p>

    <section class="card">
      <h2>账号</h2>
      <p v-if="isMulti">用户名：{{ session.user?.username }}<span v-if="isAdmin" class="tag stated admin">管理员</span></p>
      <p v-else class="muted">自部署单用户模式</p>
      <div v-if="canLogout" class="row">
        <button class="btn ghost" @click="signOut">退出登录</button>
        <button class="btn ghost" @click="signOutAll">退出所有设备</button>
      </div>
    </section>

    <section v-if="quota" class="card">
      <h2>今日用量 <span class="muted day">{{ quota.day }}</span></h2>
      <p>消息：{{ limitText(quota.messages, quota.messages_limit) }}</p>
      <p>Token：{{ limitText(quota.tokens, quota.tokens_limit) }}</p>
      <p v-if="quota.exempt && (quota.messages_limit || quota.tokens_limit)" class="muted">管理员不受限额限制</p>
    </section>

    <section v-if="isMulti" class="card">
      <h2>修改密码</h2>
      <form @submit.prevent="changePassword">
        <div class="field">
          <label for="old">当前密码</label>
          <input id="old" v-model="oldPw" class="input" type="password" autocomplete="current-password" required />
        </div>
        <div class="field">
          <label for="new">新密码（至少 8 位）</label>
          <input id="new" v-model="newPw" class="input" type="password" autocomplete="new-password" minlength="8" required />
        </div>
        <button class="btn">修改</button>
      </form>
    </section>

    <section v-if="isMulti && isAdmin" class="card">
      <h2>邀请码</h2>
      <form class="invite-form" @submit.prevent="createInvite">
        <div class="field">
          <label for="uses">可用次数（0 不限）</label>
          <input id="uses" v-model.number="inviteUses" class="input" type="number" min="0" />
        </div>
        <div class="field">
          <label for="hours">有效期（小时，0 永久）</label>
          <input id="hours" v-model.number="inviteHours" class="input" type="number" min="0" />
        </div>
        <div class="field note">
          <label for="note">备注</label>
          <input id="note" v-model="inviteNote" class="input" maxlength="100" />
        </div>
        <button class="btn">生成</button>
      </form>
      <table v-if="invites.length" class="invites">
        <thead>
          <tr><th>邀请码</th><th>已用</th><th>过期</th><th>备注</th><th /></tr>
        </thead>
        <tbody>
          <tr v-for="inv in invites" :key="inv.id">
            <td><code>{{ inv.code }}</code></td>
            <td>{{ inv.used_count }}/{{ inv.max_uses || '∞' }}</td>
            <td>{{ inv.expires_at ? dateTime(inv.expires_at) : '永久' }}</td>
            <td>{{ inv.note }}</td>
            <td class="actions">
              <button class="btn ghost small" @click="copy(inviteLink(inv.code))">复制链接</button>
              <button class="btn danger small" @click="deleteInvite(inv.id)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <p class="muted version">布谷 {{ session.meta?.version }}</p>
  </main>
</template>

<style scoped>
.card p { margin: 4px 0; }
.row { display: flex; gap: 8px; margin-top: 10px; flex-wrap: wrap; }
.admin { margin-left: 8px; }
.day { font-size: 13px; font-weight: 400; margin-left: 6px; }
.invite-form { display: grid; grid-template-columns: 1fr 1fr 2fr auto; gap: 8px; align-items: end; }
.invite-form .field { margin: 0; }
@media (max-width: 600px) { .invite-form { grid-template-columns: 1fr 1fr; } .invite-form .note { grid-column: 1 / -1; } }
.invites { width: 100%; border-collapse: collapse; margin-top: 14px; font-size: 14px; }
.invites th { text-align: left; color: var(--muted); font-weight: 500; padding: 6px 4px; border-bottom: 1px solid var(--border); }
.invites td { padding: 6px 4px; border-bottom: 1px solid var(--border); }
.actions { display: flex; gap: 6px; justify-content: flex-end; }
.version { text-align: center; font-size: 12px; margin-top: 24px; }
</style>
