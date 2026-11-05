<script setup lang="ts">
// 设置 → 角色卡：Cosplay 用的角色提示词。可以从网址（例如 aigengtu.com/persona 的大肥鱼人设卡）批量导入，
// 也可以粘贴或上传 .txt / .md / SillyTavern 的 .json。新建对话时点面具图标选一张，这段对话里 AI 就扮演它。
import { computed, onMounted, reactive, ref } from 'vue'
import { api, ApiError, type CharacterCandidate, type CharacterCard } from '../api'
import { session } from '../session'
import { toast } from '../toast'
import Icon from '../Icon.vue'
import Modal from './Modal.vue'

const name = computed(() => session.meta?.name ?? '布谷')
const isAdmin = computed(() => !!session.user?.is_admin)
const list = ref<CharacterCard[] | null>(null)
const maxRunes = ref(30000)
const loadError = ref('')
const mine = computed(() => list.value?.filter((c) => c.mine) ?? [])
const shared = computed(() => list.value?.filter((c) => !c.mine) ?? [])

onMounted(load)

async function load() {
  try {
    const r = await api.characters()
    list.value = r.characters
    maxRunes.value = r.max_runes
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

function wan(n: number) {
  return n >= 10000 ? `${Math.round(n / 1000) / 10} 万` : n.toLocaleString()
}
function errText(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback
}

// ---- 导入 ----
const importOpen = ref(false)
const mode = ref<'url' | 'text'>('url')
const url = ref('')
const text = ref('')
const filename = ref('')
const busy = ref(false)
const importError = ref('')
const candidates = ref<(CharacterCandidate & { picked: boolean })[]>([])
const asPublic = ref(false)
const pickedCount = computed(() => candidates.value.filter((c) => c.picked).length)

function openImport() {
  importOpen.value = true
  mode.value = 'url'
  url.value = ''
  text.value = ''
  filename.value = ''
  importError.value = ''
  candidates.value = []
  asPublic.value = false
}

async function readUrl() {
  if (!url.value.trim() || busy.value) return
  busy.value = true
  importError.value = ''
  candidates.value = []
  try {
    const r = await api.fetchCharacters(url.value.trim())
    // 默认全选（超长的不能导入，不选）。
    candidates.value = r.characters.map((c) => ({ ...c, picked: !c.too_long }))
  } catch (e) {
    importError.value = errText(e, '读取失败')
  } finally {
    busy.value = false
  }
}

async function onFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  ;(e.target as HTMLInputElement).value = ''
  if (!f) return
  if (f.size > 2 << 20) {
    importError.value = '文件太大了'
    return
  }
  text.value = await f.text()
  filename.value = f.name
  await parseText()
}

async function parseText() {
  if (!text.value.trim() || busy.value) return
  busy.value = true
  importError.value = ''
  candidates.value = []
  try {
    const r = await api.parseCharacter(text.value, filename.value)
    candidates.value = r.characters.map((c) => ({ ...c, picked: !c.too_long }))
  } catch (e) {
    importError.value = errText(e, '解析失败')
  } finally {
    busy.value = false
  }
}

async function importPicked() {
  const picked = candidates.value.filter((c) => c.picked)
  if (!picked.length || busy.value) return
  busy.value = true
  importError.value = ''
  let ok = 0
  const failed: string[] = []
  for (const c of picked) {
    try {
      await api.createCharacter({ name: c.name || '未命名角色', description: c.description, prompt: c.prompt, source: c.source, public: asPublic.value })
      ok++
      c.picked = false
    } catch (e) {
      failed.push(`「${c.name || '未命名角色'}」：${errText(e, '导入失败')}`)
    }
  }
  busy.value = false
  await load()
  if (failed.length) {
    importError.value = `导入了 ${ok} 张，${failed.length} 张没导入：\n${failed.join('\n')}`
    return
  }
  importOpen.value = false
  toast(`导入了 ${ok} 张角色卡，新建对话时点面具图标就能选`)
}

// ---- 查看 / 编辑 ----
const editing = ref<CharacterCard | null>(null)
const form = reactive({ name: '', description: '', prompt: '', public: false })
const formError = ref('')
const saving = ref(false)
const loadingCard = ref(false)
const canEdit = computed(() => !!editing.value && (editing.value.mine || (isAdmin.value && editing.value.public)))
// 普通用户打开公共卡：也能改，保存时另存为自己的副本（公共卡本身不变）。
const forking = computed(() => !!editing.value && !canEdit.value)
const formRunes = computed(() => [...form.prompt].length)

// 修改记录：自我进化或手动修改前的样子，可以恢复。
const revisions = ref<{ id: number; note: string; created_at: string; runes: number }[]>([])
const restoring = ref(0)

async function loadRevisions(id: number) {
  try {
    revisions.value = (await api.characterRevisions(id)).revisions
  } catch {
    revisions.value = []
  }
}

async function restore(rid: number) {
  const c = editing.value
  if (!c || restoring.value) return
  restoring.value = rid
  try {
    await api.restoreCharacter(c.id, rid)
    const d = await api.character(c.id)
    form.prompt = d.prompt
    await loadRevisions(c.id)
    await load()
    toast('已恢复，用这张卡的对话从下一条消息起按恢复后的内容扮演')
  } catch (e) {
    toast(errText(e, '恢复失败'))
  } finally {
    restoring.value = 0
  }
}

async function openCard(c: CharacterCard) {
  editing.value = c
  formError.value = ''
  loadingCard.value = true
  revisions.value = []
  Object.assign(form, { name: c.name, description: c.description, prompt: '', public: c.public })
  try {
    const d = await api.character(c.id)
    Object.assign(form, { name: d.name, description: d.description, prompt: d.prompt, public: d.public })
    if (c.mine || (isAdmin.value && c.public)) await loadRevisions(c.id)
  } catch (e) {
    formError.value = errText(e, '读取失败')
  } finally {
    loadingCard.value = false
  }
}

async function save() {
  if (!editing.value || saving.value) return
  saving.value = true
  formError.value = ''
  try {
    if (forking.value) {
      await api.forkCharacter(editing.value.id, { ...form, public: false })
      editing.value = null
      toast('已另存为你自己的角色卡，你用这张公共卡的对话也都换成了这份副本')
    } else {
      await api.updateCharacter(editing.value.id, { ...form })
      editing.value = null
      toast('角色卡已保存，用它的对话从下一条消息起按新内容扮演')
    }
    await load()
  } catch (e) {
    formError.value = errText(e, '保存失败')
  } finally {
    saving.value = false
  }
}

const removing = ref<CharacterCard | null>(null)
async function remove() {
  const c = removing.value
  if (!c) return
  removing.value = null
  try {
    await api.deleteCharacter(c.id)
    editing.value = null
    toast(`已删除「${c.name}」，用它建的对话还能接着聊`)
    await load()
  } catch (e) {
    toast(errText(e, '删除失败'))
  }
}
</script>

<template>
  <section id="characters" class="panel panel--inset">
    <div class="head">
      <h2>角色卡</h2>
      <button type="button" class="btn btn-sm btn-primary" @click="openImport"><Icon name="plus" :size="15" /> 导入</button>
    </div>
    <p class="muted intro">
      聊天时直接跟角色说「以后别老用括号」「说话再凶一点」，它会自己改角色卡（自我进化），改之前的样子在卡的修改记录里能恢复。
    </p>
    <p class="muted intro">
      Cosplay 模式：导入角色提示词（比如大肥鱼人设卡），新建对话时点对话页右上角的<Icon name="mask" :size="14" class="inline-icon" />面具图标选一张，这段对话里{{ name }}就会扮演这个角色。每张卡每轮都会整张发给模型，卡越长越费 token。
    </p>
    <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
    <div v-else-if="list === null" class="skeleton skel" aria-hidden="true" />
    <p v-else-if="!list.length" class="caption empty">
      还没有角色卡。点「导入」，填网址 <code>https://aigengtu.com/persona</code> 可以一次导入一整页的大肥鱼人设卡。
    </p>
    <template v-else>
      <template v-for="group in [{ label: '我导入的', items: mine }, { label: '公共角色卡', items: shared }]" :key="group.label">
        <p v-if="group.items.length" class="group">{{ group.label }}</p>
        <button v-for="c in group.items" :key="c.id" type="button" class="row" @click="openCard(c)">
          <span class="row__icon"><Icon name="mask" :size="18" /></span>
          <span class="row__text">
            <span class="row__title">{{ c.name }}<span v-if="c.public" class="row__tag">公共</span></span>
            <span v-if="c.description" class="caption row__desc">{{ c.description }}</span>
          </span>
          <span class="caption row__meta" :title="`${c.runes.toLocaleString()} 字，每轮大约 ${c.tokens.toLocaleString()} token`">{{ wan(c.runes) }} 字</span>
        </button>
      </template>
    </template>

    <!-- 导入 -->
    <Modal :open="importOpen" title="导入角色卡" wide @close="importOpen = false">
      <div class="type-switch tabs" role="tablist">
        <button type="button" role="tab" :aria-selected="mode === 'url'" :class="{ 'is-active': mode === 'url' }" @click="mode = 'url'; candidates = []; importError = ''">从网址</button>
        <button type="button" role="tab" :aria-selected="mode === 'text'" :class="{ 'is-active': mode === 'text' }" @click="mode = 'text'; candidates = []; importError = ''">粘贴或上传</button>
      </div>
      <form v-if="mode === 'url'" class="src" @submit.prevent="readUrl">
        <input v-model="url" class="text-input" type="url" inputmode="url" placeholder="https://aigengtu.com/persona" aria-label="网址" />
        <button class="btn btn-primary" :disabled="busy || !url.trim()">{{ busy ? '正在读取' : '读取' }}</button>
      </form>
      <p v-if="mode === 'url'" class="caption hint">
        支持一整页的人设卡（会列出来让你挑），也支持直接是提示词的 .txt / .md / SillyTavern .json 网址。
      </p>
      <template v-if="mode === 'text'">
        <textarea v-model="text" class="textarea mono" spellcheck="false" placeholder="把角色提示词全文粘贴到这里（「复制提示词」复制下来的内容、SillyTavern 角色卡 JSON 都行）" aria-label="角色提示词" />
        <div class="src-actions">
          <label class="btn btn-sm">
            <Icon name="file" :size="15" /> 选文件
            <input type="file" accept=".txt,.md,.markdown,.json,text/plain,application/json" hidden @change="onFile" />
          </label>
          <span class="caption grow">{{ filename }}</span>
          <button type="button" class="btn btn-sm btn-primary" :disabled="busy || !text.trim()" @click="parseText">{{ busy ? '正在解析' : '解析' }}</button>
        </div>
      </template>

      <p v-if="importError" class="form-flash form-flash--error pre">{{ importError }}</p>

      <template v-if="candidates.length">
        <p class="caption found">
          找到 {{ candidates.length }} 张<template v-if="candidates.length > 1">，选要导入的</template>。名字可以先改好再导入。
        </p>
        <ul class="cands">
          <li v-for="(c, i) in candidates" :key="i" class="cand" :class="{ 'is-off': !c.picked, 'is-long': c.too_long }">
            <input v-model="c.picked" type="checkbox" :disabled="c.too_long" :aria-label="`导入 ${c.name}`" />
            <div class="cand__body">
              <input v-model="c.name" class="cand__name" maxlength="40" placeholder="角色名" aria-label="角色名" />
              <p v-if="c.description" class="caption cand__desc">{{ c.description }}</p>
              <p class="caption cand__meta">
                <span v-for="t in c.tags" :key="t" class="cand__tag">{{ t }}</span>
                {{ c.runes.toLocaleString() }} 字 · 每轮约 {{ c.tokens.toLocaleString() }} token
                <strong v-if="c.too_long" class="danger">超过上限 {{ maxRunes.toLocaleString() }} 字，不能导入</strong>
              </p>
            </div>
          </li>
        </ul>
        <label v-if="isAdmin" class="check-row">
          <input v-model="asPublic" type="checkbox" />
          <span>设为公共角色卡（所有用户都能选用）</span>
        </label>
      </template>
      <div class="modal-actions">
        <button class="btn" @click="importOpen = false">取消</button>
        <button class="btn btn-primary" :disabled="busy || !pickedCount" @click="importPicked">
          {{ busy && candidates.length ? '正在导入' : pickedCount > 1 ? `导入 ${pickedCount} 张` : '导入' }}
        </button>
      </div>
    </Modal>

    <!-- 查看 / 编辑 -->
    <Modal :open="!!editing" :title="forking ? '公共角色卡' : '编辑角色卡'" wide @close="editing = null">
      <div class="field">
        <label class="field__label" for="ch-name">角色名</label>
        <input id="ch-name" v-model="form.name" class="text-input" maxlength="40" />
      </div>
      <div class="field">
        <label class="field__label" for="ch-desc">简介</label>
        <input id="ch-desc" v-model="form.description" class="text-input" maxlength="200" placeholder="一句话说说这是个什么角色（可以不填）" />
      </div>
      <div class="field">
        <label class="field__label" for="ch-prompt">角色提示词</label>
        <textarea id="ch-prompt" v-model="form.prompt" class="textarea mono prompt" spellcheck="false" :placeholder="loadingCard ? '正在读取…' : ''" />
        <p class="field__hint" :class="{ danger: formRunes > maxRunes }">
          {{ formRunes.toLocaleString() }} / {{ maxRunes.toLocaleString() }} 字。聊天时整张卡都会发给模型；已经在用这张卡的对话，从下一条消息起按新内容扮演。
        </p>
      </div>
      <label v-if="isAdmin && canEdit" class="check-row">
        <input v-model="form.public" type="checkbox" />
        <span>公共角色卡（所有用户都能选用）</span>
      </label>
      <div v-if="revisions.length" class="revs">
        <p class="field__label">修改记录<span class="caption">（聊天时让角色「自我进化」改的，或者手动改的；可以恢复成改之前的样子）</span></p>
        <ul>
          <li v-for="r in revisions" :key="r.id" class="rev">
            <span class="rev__note">{{ r.note }}</span>
            <span class="caption">{{ new Date(r.created_at).toLocaleString() }} · {{ r.runes.toLocaleString() }} 字</span>
            <button type="button" class="btn btn-sm btn-ghost" :disabled="!!restoring" @click="restore(r.id)">{{ restoring === r.id ? '正在恢复' : '恢复成这样' }}</button>
          </li>
        </ul>
      </div>
      <p v-if="forking" class="form-flash">这是管理员发布的公共角色卡，不能直接修改。改完点「另存为我的副本」，会保存成一张只属于你的卡，你正在用这张公共卡的对话也会换成副本。</p>
      <p v-if="editing?.source" class="caption">来源：<a :href="editing.source" target="_blank" rel="noopener noreferrer" class="link">{{ editing.source }}</a></p>
      <p v-if="formError" class="form-flash form-flash--error">{{ formError }}</p>
      <div class="modal-actions">
        <button v-if="canEdit" class="btn btn-ghost danger left" @click="removing = editing"><Icon name="trash" :size="15" /> 删除</button>
        <button class="btn" @click="editing = null">取消</button>
        <button class="btn btn-primary" :disabled="saving || loadingCard" @click="save">{{ saving ? '正在保存' : forking ? '另存为我的副本' : '保存' }}</button>
      </div>
    </Modal>

    <Modal :open="!!removing" :title="`删除「${removing?.name}」？`" subtitle="删除后不能再用它新建对话；已经用它建的对话还能接着聊。" @close="removing = null">
      <div class="modal-actions">
        <button class="btn" @click="removing = null">取消</button>
        <button class="btn btn-danger" @click="remove">删除</button>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.head h2 { margin: 0; }
.intro { margin: 8px 0 12px; }
.inline-icon { vertical-align: -2px; margin: 0 2px; }
.skel { height: 56px; border-radius: var(--radius-md); }
.empty code { font-size: 12px; }
.group { margin: 14px 0 2px; color: var(--color-muted); font-size: 12px; letter-spacing: .04em; }
.row {
  display: flex; align-items: center; gap: 12px; padding: 10px 8px; margin: 0 -8px; width: calc(100% + 16px);
  border-radius: var(--radius-md); text-align: left; transition: background-color .15s ease;
}
.row:hover { background: var(--color-hover-wash); }
.row__icon { display: grid; place-items: center; flex-shrink: 0; width: 34px; height: 34px; border-radius: 10px; background: var(--color-accent-tint); color: var(--color-accent-active); }
.row__text { display: grid; gap: 2px; flex: 1; min-width: 0; }
.row__title { color: var(--color-ink); font-size: 14px; font-weight: 500; overflow-wrap: anywhere; }
.row__tag { margin-left: 6px; padding: 1px 6px; border-radius: var(--radius-pill); background: var(--color-canvas-soft); color: var(--color-text-secondary); font-size: 11px; font-weight: 400; vertical-align: 1px; }
.row__desc { margin: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.row__meta { flex-shrink: 0; white-space: nowrap; }

.tabs { margin-bottom: 12px; }
.src { display: flex; gap: 8px; }
.src .text-input { flex: 1; min-width: 0; }
.hint { margin: 6px 0 0; }
.src-actions { display: flex; align-items: center; gap: 8px; margin-top: 8px; }
.src-actions .grow { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mono { font-family: var(--font-mono); font-size: 13px; }
.textarea.mono { min-height: 180px; }
.textarea.prompt { min-height: 46vh; line-height: 1.6; }
.pre { white-space: pre-line; }
.found { margin: 14px 0 6px; }
.cands { display: grid; gap: 2px; max-height: 46vh; overflow-y: auto; margin: 0 0 8px; padding: 0; list-style: none; }
.cand { display: flex; gap: 10px; align-items: flex-start; padding: 8px 6px; border-radius: var(--radius-md); }
.cand:hover { background: var(--color-hover-wash); }
.cand input[type='checkbox'] { margin-top: 5px; flex-shrink: 0; }
.cand.is-off .cand__body { opacity: .55; }
.cand__body { flex: 1; min-width: 0; }
.cand__name { width: 100%; padding: 1px 4px; margin-left: -4px; border: 1px solid transparent; border-radius: 6px; background: transparent; color: var(--color-ink); font: inherit; font-size: 14px; font-weight: 500; }
.cand__name:hover { border-color: var(--color-hairline-strong); }
.cand__name:focus { outline: none; border-color: var(--color-accent); background: var(--color-surface); }
.cand__desc { margin: 2px 0 0; overflow-wrap: anywhere; }
.cand__meta { margin: 3px 0 0; }
.cand__tag { display: inline-block; margin-right: 4px; padding: 0 6px; border-radius: var(--radius-pill); background: var(--color-canvas-soft); font-size: 11px; }
.danger { color: var(--color-error, #b42318); }
.left { margin-right: auto; }
.revs { margin: 4px 0 12px; }
.revs ul { display: grid; gap: 2px; max-height: 200px; overflow-y: auto; margin: 6px 0 0; padding: 0; list-style: none; }
.rev { display: flex; align-items: center; gap: 10px; padding: 6px 8px; border-radius: var(--radius-md); }
.rev:hover { background: var(--color-hover-wash); }
.rev__note { flex: 1; min-width: 0; color: var(--color-ink); font-size: 13px; overflow-wrap: anywhere; }
@media (max-width: 600px) {
  .row__meta { display: none; }
}
</style>
