<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError, type Invite } from '../api'
import AgentPanel from '../components/AgentPanel.vue'
import EmbeddingPanel from '../components/EmbeddingPanel.vue'
import UtilityPanel from '../components/UtilityPanel.vue'
import ExtensionsPanel from '../components/ExtensionsPanel.vue'
import Modal from '../components/Modal.vue'
import ModelsPanel from '../components/ModelsPanel.vue'
import PersonaPanel from '../components/PersonaPanel.vue'
import { dateTime } from '../format'
import Icon from '../Icon.vue'
import { memoriesUnlocked, setMemoriesUnlocked } from '../memoryLock'
import { session } from '../session'
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
const compacting = ref(false)
async function compactMemories() {
  compacting.value = true
  try {
    const r = await api.compactMemories()
    toast(r.merged ? `整理好了：${r.merged} 条相近的记忆合成了 ${r.created} 条，现在一共 ${r.after} 条` : `没有需要合并的记忆，现在一共 ${r.after} 条`)
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '整理失败')
  } finally {
    compacting.value = false
  }
}
const error = ref('')
const message = ref('')

const isMulti = computed(() => session.meta?.mode === 'multi')
const isAdmin = computed(() => !!session.user?.is_admin)

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
    session.user = me.user
    if (isMulti.value && me.user.is_admin) invites.value = (await api.invites()).invites
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

onMounted(load)

// 左边分类、右边内容（参考 claude.ai 的设置）。分类记在地址的 #hash 里，旧链接（#models 等）照样能打开。
type IconName = 'message' | 'sparkle' | 'code' | 'grid' | 'brain' | 'refresh' | 'globe' | 'link' | 'settings'
interface Tab { id: string; label: string; icon: IconName; group: string; keys: string }
const tabs = computed<Tab[]>(() => [
  { id: 'agent', label: '牛马', icon: 'message', group: '偏好', keys: '设置助手 agent' },
  { id: 'persona', label: '性格', icon: 'sparkle', group: '偏好', keys: '人格 预设 风格' },
  { id: 'models', label: '模型', icon: 'code', group: '偏好', keys: 'api key opencode 思考' },
  { id: 'extensions', label: '技能与插件', icon: 'grid', group: '偏好', keys: 'skill mcp 插件' },
  { id: 'memory', label: '大脑皮层', icon: 'brain', group: '偏好', keys: '记忆 心情 情绪 整理' },
  ...(isAdmin.value
    ? [
        { id: 'utility', label: '辅助模型', icon: 'refresh' as const, group: '管理', keys: '后台 记忆整理' },
        { id: 'embedding', label: '向量模型', icon: 'globe' as const, group: '管理', keys: 'embedding 记忆检索' },
      ]
    : []),
  ...(isMulti.value && isAdmin.value ? [{ id: 'invites', label: '邀请码', icon: 'link' as const, group: '管理', keys: '注册 邀请' }] : []),
  ...(isAdmin.value || !isMulti.value ? [{ id: 'console', label: '管理后台', icon: 'settings' as const, group: '管理', keys: '系统设置 用户 计费' }] : []),
])
const query = ref('')
const groups = computed(() => {
  const q = query.value.trim().toLowerCase()
  const hit = tabs.value.filter((t) => !q || `${t.label} ${t.keys}`.toLowerCase().includes(q))
  return ['偏好', '管理'].map((g) => ({ label: g, items: hit.filter((t) => t.group === g) })).filter((g) => g.items.length)
})
const tab = ref('persona')
watch(
  [() => route.hash, tabs],
  () => {
    const id = route.hash.replace('#', '')
    tab.value = tabs.value.some((t) => t.id === id) ? id : 'persona'
  },
  { immediate: true },
)
function pick(id: string) {
  router.replace({ hash: `#${id}` })
}
</script>

<template>
  <main class="main">
    <header class="topbar">
      <div class="topbar__titles">
        <h1>设置</h1>
      </div>
    </header>

    <div class="page settings-page">
      <div class="settings">
        <nav class="settings__nav" aria-label="设置分类">
          <label class="settings__search">
            <Icon name="search" :size="15" />
            <input v-model="query" type="search" placeholder="搜索" aria-label="搜索设置" />
          </label>
          <template v-for="g in groups" :key="g.label">
            <p class="settings__group">{{ g.label }}</p>
            <button
              v-for="t in g.items"
              :key="t.id"
              type="button"
              class="settings__tab"
              :class="{ 'is-active': tab === t.id }"
              :aria-current="tab === t.id ? 'page' : undefined"
              @click="pick(t.id)"
            >
              <Icon :name="t.icon" :size="17" />{{ t.label }}
            </button>
          </template>
          <p v-if="!groups.length" class="caption settings__none">没有找到</p>
        </nav>

      <div class="settings__content">
        <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
        <p v-if="message" class="form-flash">{{ message }}</p>

        <AgentPanel v-if="tab === 'agent'" />

        <PersonaPanel v-if="tab === 'persona'" />

        <ModelsPanel v-if="tab === 'models'" />

        <ExtensionsPanel v-if="tab === 'extensions'" />

        <UtilityPanel v-if="isAdmin && tab === 'utility'" />

        <EmbeddingPanel v-if="isAdmin && tab === 'embedding'" />

        <section v-if="tab === 'memory'" id="memory" class="panel panel--inset">
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
          <div class="compact">
            <p class="caption">
              聊得越久记的事越多。整理一下会把重复、相近的记忆合并成一条，信息不丢，聊天时消耗的额度也更少。
              记得太多时也会自动整理。
            </p>
            <button class="btn btn-sm" :disabled="compacting" @click="compactMemories">{{ compacting ? '正在整理…' : '整理记忆' }}</button>
          </div>
        </section>

        <section v-if="isMulti && isAdmin && tab === 'invites'" class="panel panel--inset">
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

        <section v-if="(isAdmin || !isMulti) && tab === 'console'" class="panel panel--inset">
          <h2>管理后台</h2>
          <p class="muted">在网页上查看和修改全部系统设置（需要管理员密码）。修改后需要在服务器上重启服务才会生效。</p>
          <div class="actions">
            <button class="btn" @click="router.push('/console')">进入管理后台</button>
          </div>
        </section>

        <p class="caption version">BUUGOO {{ session.meta?.version }}</p>
      </div>

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
/* 左边分类、右边内容（参考 claude.ai 的设置） */
.settings-page { padding-top: 8px; }
.settings { display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: 40px; max-width: 1040px; margin: 0 auto; }
.settings__nav { position: sticky; top: 0; align-self: start; display: grid; gap: 1px; min-width: 0; }
.settings__search {
  display: flex; align-items: center; gap: 8px; height: 36px; margin-bottom: 14px; padding: 0 10px;
  border: 1px solid var(--color-divider); border-radius: var(--radius-md); background: var(--color-surface); color: var(--color-muted);
}
.settings__search input { flex: 1; width: 100%; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--color-ink); font-size: 14px; }
.settings__group { margin: 12px 10px 6px; color: var(--color-muted); font-size: 13px; }
.settings__group:first-of-type { margin-top: 0; }
.settings__tab {
  display: flex; align-items: center; gap: 11px; height: 38px; padding: 0 10px; border-radius: var(--radius-md);
  color: var(--color-ink); font-size: 15px; text-align: left; transition: background-color .15s ease;
}
.settings__tab :deep(svg) { color: var(--color-text-secondary); }
.settings__tab:hover { background: var(--color-hover-wash); }
.settings__tab.is-active { background: rgba(20, 20, 19, .075); }
.settings__none { margin: 4px 10px; }
/* 右边的内容不用卡片底色：标题 + 细分隔线（和 claude.ai 一样） */
.settings__content { min-width: 0; padding-bottom: 24px; }
.settings__content :deep(.panel) { padding: 0; border-radius: 0; background: transparent; }
.settings__content :deep(.panel + .panel) { margin-top: 36px; }
.settings__content :deep(.panel > h2), .settings__content :deep(.panel > header h2), .settings__content :deep(.panel h2:first-child) {
  margin-bottom: 8px; font-size: 18px; font-weight: 600;
}
@media (max-width: 767px) {
  .settings { grid-template-columns: minmax(0, 1fr); gap: 16px; }
  .settings__nav { position: static; display: flex; gap: 6px; overflow-x: auto; padding-bottom: 4px; scrollbar-width: none; }
  .settings__search, .settings__group { display: none; }
  .settings__tab { flex-shrink: 0; height: 34px; padding: 0 12px; border: 1px solid var(--color-divider); border-radius: var(--radius-pill); font-size: 14px; white-space: nowrap; }
  .settings__tab :deep(svg) { display: none; }
}
.panel p { margin: 0; }
.actions { display: flex; gap: 8px; margin-top: 16px; flex-wrap: wrap; }
.invite-form { display: grid; grid-template-columns: 1fr 1fr 2fr; gap: 0 12px; align-items: start; }
.invite-form .gen { grid-column: 1 / -1; justify-self: start; }
@media (max-width: 767px) { .invite-form { grid-template-columns: 1fr 1fr; } .invite-form .note { grid-column: 1 / -1; } }
.invites { margin-top: 20px; }
.invite { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; padding: 12px 0; border-top: 1px solid var(--color-divider); }
.invite .code { font-size: 14px; letter-spacing: .06em; }
.spacer { flex: 1; }
.version { text-align: center; margin-top: 28px; }
.compact { display: flex; align-items: center; gap: 12px; margin-top: 16px; padding-top: 14px; border-top: 1px solid var(--color-divider); }
.compact .caption { flex: 1; }
.peek-note { margin: 0; font-size: 14px; line-height: 1.65; }
</style>
