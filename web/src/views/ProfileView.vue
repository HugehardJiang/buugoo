<script setup lang="ts">
// 个人中心：头像、昵称、会员与用量、修改密码、退出登录、诊断信息。和布谷相关的设置在「设置」里。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError, type Quota } from '../api'
import Avatar from '../components/Avatar.vue'
import MemberPanel from '../components/MemberPanel.vue'
import { copyDiagnostics } from '../diag'
import { dateTime, mtok } from '../format'
import { applySession, logout, session } from '../session'
import { toast } from '../toast'

const router = useRouter()
const isMulti = computed(() => session.meta?.mode === 'multi')
const isAdmin = computed(() => !!session.user?.is_admin)
const canLogout = computed(() => !!session.meta?.password_required)
const billing = computed(() => isMulti.value && !!session.meta?.billing)

const quota = ref<Quota | null>(null)
const avatarLeft = ref(0)
const avatarLimit = ref(5)
const rolling = ref(false)
const nickname = ref('')
const savingName = ref(false)
const oldPw = ref('')
const newPw = ref('')
const pwError = ref('')

const shownName = computed(() => session.user?.display_name || session.user?.username || '我')

onMounted(async () => {
  try {
    const [p, me] = await Promise.all([api.profile(), api.me()])
    session.user = p.user
    nickname.value = p.user.display_name
    avatarLeft.value = p.avatar_left
    avatarLimit.value = p.avatar_limit
    quota.value = me.quota
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '加载失败')
  }
})

async function reroll() {
  if (rolling.value || avatarLeft.value <= 0) return
  rolling.value = true
  try {
    const r = await api.regenerateAvatar()
    session.user = r.user
    avatarLeft.value = r.avatar_left
    toast(r.avatar_left ? `换了一只新的小鸟，今年还能换 ${r.avatar_left} 次` : '换了一只新的小鸟，今年的次数用完了')
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '换头像失败')
    if (e instanceof ApiError && e.code === 'avatar_limit') avatarLeft.value = 0
  } finally {
    rolling.value = false
  }
}

async function saveName() {
  savingName.value = true
  try {
    session.user = (await api.updateProfile(nickname.value)).user
    toast('昵称已保存')
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    savingName.value = false
  }
}

async function changePassword() {
  pwError.value = ''
  try {
    applySession(await api.changePassword(oldPw.value, newPw.value))
    oldPw.value = newPw.value = ''
    toast('密码已修改，其他设备需要重新登录')
  } catch (e) {
    pwError.value = e instanceof ApiError ? e.message : '修改失败'
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
    toast(e instanceof ApiError ? e.message : '操作失败')
  }
}

function limitText(unit: 'messages' | 'tokens', used: number, limit: number) {
  const f = (n: number) => (unit === 'tokens' ? mtok(n) : String(n))
  const suffix = unit === 'tokens' ? ' 百万' : ' 条'
  return limit > 0 ? `${f(used)} / ${f(limit)}${suffix}` : `${f(used)}${suffix}`
}
function percent(used: number, limit: number) {
  return limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0
}
</script>

<template>
  <main class="main">
    <header class="topbar">
      <div class="topbar__titles">
        <h1>个人中心</h1>
      </div>
    </header>

    <div class="page">
      <div class="page__inner">
        <section class="panel panel--inset hero">
          <div class="hero__avatar">
            <Avatar :seed="session.user?.avatar_seed ?? ''" :size="96" :label="`${shownName}的头像`" />
            <button type="button" class="btn btn-sm" :disabled="rolling || avatarLeft <= 0" @click="reroll">
              {{ rolling ? '正在换…' : '换一只' }}
            </button>
            <p class="caption">今年还能换 {{ avatarLeft }}/{{ avatarLimit }} 次</p>
          </div>
          <div class="hero__info">
            <p class="hero__name">{{ shownName }}</p>
            <p class="caption">
              <template v-if="isMulti">@{{ session.user?.username }} · </template>
              <template v-if="session.user?.created_at">{{ dateTime(session.user.created_at) }} 加入</template>
              <span v-if="isMulti && isAdmin" class="badge-pill">管理员</span>
            </p>
            <form class="nick" @submit.prevent="saveName">
              <label class="field__label" for="nick">昵称</label>
              <div class="nick__row">
                <input id="nick" v-model="nickname" class="text-input" maxlength="30" placeholder="布谷怎么称呼你" />
                <button class="btn" :disabled="savingName || nickname === (session.user?.display_name ?? '')">保存</button>
              </div>
              <p class="field__hint">布谷会这样称呼你。</p>
            </form>
          </div>
        </section>

        <MemberPanel v-if="billing" />

        <section v-else-if="quota" class="panel panel--inset">
          <h2>今日用量</h2>
          <dl class="metrics">
            <div class="metric">
              <dt>消息</dt>
              <dd>{{ limitText('messages', quota.messages, quota.messages_limit) }}</dd>
              <div v-if="quota.messages_limit" class="workspace-progress"><i :style="{ width: percent(quota.messages, quota.messages_limit) + '%' }" /></div>
            </div>
            <div class="metric">
              <dt>Token</dt>
              <dd>{{ limitText('tokens', quota.tokens, quota.tokens_limit) }}</dd>
              <div v-if="quota.tokens_limit" class="workspace-progress"><i :style="{ width: percent(quota.tokens, quota.tokens_limit) + '%' }" /></div>
            </div>
            <div v-if="quota.searches_limit" class="metric">
              <dt>联网搜索</dt>
              <dd>{{ quota.searches }} / {{ quota.searches_limit }} 次</dd>
              <div class="workspace-progress"><i :style="{ width: percent(quota.searches, quota.searches_limit) + '%' }" /></div>
            </div>
          </dl>
          <p class="caption">按 {{ quota.day }} 统计，token 以百万为单位<template v-if="!quota.messages_limit && !quota.tokens_limit">，当前不限量</template><template v-else-if="quota.exempt">，管理员不受限额限制</template>。</p>
        </section>

        <section v-if="isMulti" class="panel panel--inset">
          <h2>修改密码</h2>
          <form @submit.prevent="changePassword">
            <div class="form-row">
              <div class="field">
                <label class="field__label" for="old">当前密码</label>
                <input id="old" v-model="oldPw" class="text-input" type="password" autocomplete="current-password" required />
              </div>
              <div class="field">
                <label class="field__label" for="new">新密码</label>
                <input id="new" v-model="newPw" class="text-input" type="password" autocomplete="new-password" minlength="8" required />
                <p class="field__hint">至少 8 位。修改后其他设备需要重新登录。</p>
              </div>
            </div>
            <p v-if="pwError" class="form-flash form-flash--error">{{ pwError }}</p>
            <button class="btn btn-primary">修改密码</button>
          </form>
        </section>

        <section class="panel panel--inset">
          <h2>账号</h2>
          <p v-if="!isMulti" class="muted">自部署单用户模式，数据只保存在你自己的服务器上。</p>
          <div v-if="canLogout" class="actions">
            <button class="btn" @click="signOut">退出登录</button>
            <button class="btn btn-ghost" @click="signOutAll">退出所有设备</button>
          </div>
          <div class="diag">
            <p class="caption">遇到问题时，把诊断信息发给管理员，方便定位。里面没有聊天内容，密钥会自动打码。</p>
            <button class="btn btn-sm" @click="copyDiagnostics({ 来自: '个人中心' })">复制诊断信息</button>
          </div>
        </section>
      </div>
    </div>
  </main>
</template>

<style scoped>
.panel p { margin: 0; }
.hero { display: flex; gap: 28px; align-items: flex-start; }
.hero__avatar { display: flex; flex-direction: column; align-items: center; gap: 10px; flex-shrink: 0; }
.hero__avatar .caption { font-size: 12px; }
.hero__info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
.hero__name { font-family: var(--font-display); font-size: 28px; color: var(--color-ink); line-height: 1.2; overflow-wrap: anywhere; }
.hero__info .badge-pill { margin-left: 8px; }
.nick { margin-top: 12px; }
.nick__row { display: flex; gap: 8px; margin-top: 6px; }
.nick__row .text-input { flex: 1; min-width: 0; }
.metrics { display: grid; margin: 0 0 12px; }
.metric { display: grid; grid-template-columns: 1fr auto; gap: 8px 16px; padding: 14px 0; border-top: 1px solid var(--color-divider); font-size: 14px; }
.metric dt { color: var(--color-text-secondary); }
.metric dd { margin: 0; font-weight: 500; font-variant-numeric: tabular-nums; }
.metric .workspace-progress { grid-column: 1 / -1; }
.actions { display: flex; gap: 8px; margin-top: 16px; flex-wrap: wrap; }
.diag { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--color-divider); }
@media (max-width: 560px) {
  .hero { flex-direction: column; align-items: center; text-align: center; }
  .hero__info { width: 100%; align-items: center; }
  .nick { width: 100%; text-align: left; }
  .diag { flex-direction: column; align-items: flex-start; }
}
</style>
