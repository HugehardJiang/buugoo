<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError, type Invite, type Quota } from '../api'
import EmbeddingPanel from '../components/EmbeddingPanel.vue'
import Modal from '../components/Modal.vue'
import ModelsPanel from '../components/ModelsPanel.vue'
import PersonaPanel from '../components/PersonaPanel.vue'
import { dateTime } from '../format'
import { memoriesUnlocked, setMemoriesUnlocked } from '../memoryLock'
import { applySession, logout, session } from '../session'
import { toast } from '../toast'

const router = useRouter()
const route = useRoute()
const name = computed(() => session.meta?.name ?? '布谷')
const peeking = ref(false)

function openMemories() {
  setMemoriesUnlocked(true)
  peeking.value = false
  router.push('/memories')
}

function hideMemories() {
  setMemoriesUnlocked(false)
  toast(`大脑皮层合上了。${name.value}的记忆和心情都还在，只是不再摊开给你看`)
}
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
    toast('密码已修改，其他设备需要重新登录')
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
    toast(`已生成邀请码 ${inv.code}`)
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
    toast('邀请链接已复制')
  } catch {
    message.value = text
  }
}

function limitText(used: number, limit: number) {
  return limit > 0 ? `${used} / ${limit}` : `${used}`
}

function percent(used: number, limit: number) {
  return limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0
}

onMounted(async () => {
  await load()
  // 从聊天页的提示跳转过来时滚动到对应区块。
  if (route.hash) {
    await nextTick()
    document.querySelector(route.hash)?.scrollIntoView({ block: 'start' })
  }
})
</script>

<template>
  <main class="main">
    <header class="topbar">
      <div class="topbar__titles">
        <span class="eyebrow">SETTINGS · 设置</span>
        <h1>设置</h1>
      </div>
    </header>

    <div class="page">
      <div class="page__inner">
        <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
        <p v-if="message" class="form-flash">{{ message }}</p>

        <PersonaPanel />

        <ModelsPanel />

        <EmbeddingPanel v-if="isAdmin" />

        <section id="memory" class="panel panel--inset">
          <h2>大脑皮层</h2>
          <template v-if="memoriesUnlocked">
            <p class="muted">已经翻开，可以在导航里的「大脑皮层」查看{{ name }}记住的事和此刻的心情。</p>
            <div class="actions">
              <button class="btn" @click="router.push('/memories')">打开大脑皮层</button>
              <button class="btn btn-ghost" @click="hideMemories">重新藏起来</button>
            </div>
          </template>
          <template v-else>
            <p class="muted">{{ name }}会悄悄记住你们聊过的事，也会因为你说的话开心、难过或生气。这些都藏在心里，默认是看不到的。</p>
            <div class="actions">
              <button class="btn" @click="peeking = true">偷看一眼</button>
            </div>
          </template>
        </section>

        <section v-if="quota" class="panel panel--inset">
          <h2>今日用量</h2>
          <dl class="metrics">
            <div class="metric">
              <dt>消息</dt>
              <dd>{{ limitText(quota.messages, quota.messages_limit) }}</dd>
              <div v-if="quota.messages_limit" class="workspace-progress"><i :style="{ width: percent(quota.messages, quota.messages_limit) + '%' }" /></div>
            </div>
            <div class="metric">
              <dt>Token</dt>
              <dd>{{ limitText(quota.tokens, quota.tokens_limit) }}</dd>
              <div v-if="quota.tokens_limit" class="workspace-progress"><i :style="{ width: percent(quota.tokens, quota.tokens_limit) + '%' }" /></div>
            </div>
          </dl>
          <p class="caption">按 {{ quota.day }} 统计<template v-if="!quota.messages_limit && !quota.tokens_limit">，当前不限量</template><template v-else-if="quota.exempt">，管理员不受限额限制</template>。</p>
        </section>

        <section class="panel panel--inset">
          <h2>账号</h2>
          <div v-if="isMulti" class="row-line">
            <span>{{ session.user?.username }}</span>
            <span v-if="isAdmin" class="badge-pill">管理员</span>
          </div>
          <p v-else class="muted">自部署单用户模式，数据只保存在你自己的服务器上。</p>
          <div v-if="canLogout" class="actions">
            <button class="btn" @click="signOut">退出登录</button>
            <button class="btn btn-ghost" @click="signOutAll">退出所有设备</button>
          </div>
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
            <button class="btn btn-primary">修改密码</button>
          </form>
        </section>

        <section v-if="isMulti && isAdmin" class="panel panel--inset">
          <h2>邀请码</h2>
          <form class="invite-form" @submit.prevent="createInvite">
            <div class="field">
              <label class="field__label" for="uses">可用次数</label>
              <input id="uses" v-model.number="inviteUses" class="text-input" type="number" min="0" />
              <p class="field__hint">0 表示不限次数。</p>
            </div>
            <div class="field">
              <label class="field__label" for="hours">有效期（小时）</label>
              <input id="hours" v-model.number="inviteHours" class="text-input" type="number" min="0" />
              <p class="field__hint">0 表示永久有效。</p>
            </div>
            <div class="field note">
              <label class="field__label" for="note">备注</label>
              <input id="note" v-model="inviteNote" class="text-input" maxlength="100" />
            </div>
            <button class="btn btn-primary gen">生成邀请码</button>
          </form>
          <div v-if="invites.length" class="invites">
            <div v-for="inv in invites" :key="inv.id" class="invite">
              <code class="code">{{ inv.code }}</code>
              <span class="caption">已用 {{ inv.used_count }}/{{ inv.max_uses || '不限' }} · {{ inv.expires_at ? dateTime(inv.expires_at) + ' 过期' : '永久有效' }}<template v-if="inv.note"> · {{ inv.note }}</template></span>
              <span class="spacer" />
              <button class="btn" @click="copy(inviteLink(inv.code))">复制链接</button>
              <button class="btn btn-danger-lite" @click="deleteInvite(inv.id)">删除</button>
            </div>
          </div>
        </section>

        <section v-if="isAdmin || !isMulti" class="panel panel--inset">
          <h2>管理后台</h2>
          <p class="muted">在网页上查看和修改全部系统设置（需要管理员密码）。修改后需要在服务器上重启服务才会生效。</p>
          <div class="actions">
            <button class="btn" @click="router.push('/console')">进入管理后台</button>
          </div>
        </section>

        <p class="caption version">BUUGOO {{ session.meta?.version }}</p>

        <Modal
          :open="peeking"
          title="要看看大脑皮层吗？"
          :subtitle="`${name}记得的事和此刻的心情，就像朋友藏在心里的东西。看了会少一点惊喜，确定要看吗？`"
          @close="peeking = false"
        >
          <p class="muted peek-note">翻开后可以查看、删除每一条记忆，看到{{ name }}对你的心情和你们的关系，聊天里也会显示这次想起了哪些事。随时可以重新藏起来。</p>
          <div class="modal-actions">
            <button class="btn" @click="peeking = false">还是保持神秘</button>
            <button class="btn btn-primary" @click="openMemories">翻开看看</button>
          </div>
        </Modal>
      </div>
    </div>
  </main>
</template>

<style scoped>
.panel p { margin: 0; }
.row-line { display: flex; align-items: center; gap: 8px; }
.actions { display: flex; gap: 8px; margin-top: 16px; flex-wrap: wrap; }
.metrics { display: grid; margin: 0 0 12px; }
.metric { display: grid; grid-template-columns: 1fr auto; gap: 8px 16px; padding: 14px 0; border-top: 1px solid var(--color-divider); font-size: 14px; }
.metric dt { color: var(--color-text-secondary); }
.metric dd { margin: 0; font-weight: 500; font-variant-numeric: tabular-nums; }
.metric .workspace-progress { grid-column: 1 / -1; }
.invite-form { display: grid; grid-template-columns: 1fr 1fr 2fr; gap: 0 12px; align-items: start; }
.invite-form .gen { grid-column: 1 / -1; justify-self: start; }
@media (max-width: 767px) { .invite-form { grid-template-columns: 1fr 1fr; } .invite-form .note { grid-column: 1 / -1; } }
.invites { margin-top: 20px; }
.invite { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; padding: 12px 0; border-top: 1px solid var(--color-divider); }
.invite .code { font-size: 14px; letter-spacing: .06em; }
.spacer { flex: 1; }
.version { text-align: center; margin-top: 28px; }
.peek-note { margin: 0; font-size: 14px; line-height: 1.65; }
</style>
