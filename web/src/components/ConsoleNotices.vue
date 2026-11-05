<script setup lang="ts">
// 管理后台 → 通知：发布强制弹窗通知。用户下次打开网页时弹出，看够设定的秒数才能关；之后注册的用户也会弹。
import { onMounted, reactive, ref } from 'vue'
import { ApiError, type Notice } from '../api'
import { renderMarkdown } from '../format'
import Modal from './Modal.vue'
import { toast } from '../toast'

const props = defineProps<{ call: <T>(method: string, path: string, body?: unknown) => Promise<T> }>()

const list = ref<Notice[] | null>(null)
const error = ref('')
const editing = ref<Notice | null>(null)
const isNew = ref(false)
const form = reactive({ title: '', content: '', seconds: 10 as number | '', resend: false })
const formError = ref('')
const saving = ref(false)
const preview = ref(false)
const removing = ref<Notice | null>(null)

onMounted(load)

async function load() {
  try {
    list.value = (await props.call<{ notices: Notice[] }>('GET', '/notices')).notices
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

function openForm(n: Notice | null) {
  isNew.value = !n
  editing.value = n ?? { id: 0, title: '', content: '', seconds: 10, created_at: '' }
  Object.assign(form, { title: n?.title ?? '', content: n?.content ?? '', seconds: n?.seconds ?? 10, resend: false })
  formError.value = ''
  preview.value = false
}

async function save() {
  if (saving.value) return
  saving.value = true
  formError.value = ''
  const body = { title: form.title, content: form.content, seconds: Number(form.seconds) || 0, resend: form.resend }
  try {
    if (isNew.value) await props.call('POST', '/notices', body)
    else await props.call('PUT', `/notices/${editing.value!.id}`, body)
    toast(isNew.value ? '通知已发布，用户下次打开网页时会弹出' : form.resend ? '已修改，所有人会重新弹一次' : '已修改')
    editing.value = null
    await load()
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function remove() {
  const n = removing.value
  if (!n) return
  removing.value = null
  try {
    await props.call('DELETE', `/notices/${n.id}`)
    toast('通知已撤回')
    await load()
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '撤回失败')
  }
}
</script>

<template>
  <section class="panel panel--inset">
    <div class="head">
      <h2>通知</h2>
      <button class="btn btn-sm btn-primary" @click="openForm(null)">发布通知</button>
    </div>
    <p class="muted intro">
      发布后，每个用户下次打开网页时都会强制弹出（之后才注册的用户也会），必须看够设定的秒数才能关；关掉后不再弹，在「设置 → 通知」里还能翻看。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <div v-else-if="list === null" class="skeleton skel" aria-hidden="true" />
    <p v-else-if="!list.length" class="caption">还没有发过通知。</p>
    <div v-for="n in list ?? []" :key="n.id" class="row">
      <div class="row__text">
        <p class="row__title">{{ n.title }}</p>
        <p class="caption">{{ new Date(n.created_at).toLocaleString() }} · 强制阅读 {{ n.seconds }} 秒 · {{ n.reads ?? 0 }} 人已看</p>
      </div>
      <button class="btn btn-sm" @click="openForm(n)">修改</button>
      <button class="btn btn-sm btn-ghost" @click="removing = n">撤回</button>
    </div>

    <Modal :open="!!editing" :title="isNew ? '发布通知' : '修改通知'" wide @close="editing = null">
      <div class="field">
        <label class="field__label" for="n-title">标题</label>
        <input id="n-title" v-model="form.title" class="text-input" maxlength="60" placeholder="例如：服务升级通知" />
      </div>
      <div class="field">
        <div class="label-row">
          <label class="field__label" for="n-content">正文</label>
          <button type="button" class="btn btn-sm btn-ghost" @click="preview = !preview">{{ preview ? '编辑' : '预览' }}</button>
        </div>
        <!-- eslint-disable-next-line vue/no-v-html -->
        <div v-if="preview" class="md preview" v-html="renderMarkdown(form.content || '（没有正文）')" />
        <textarea v-else id="n-content" v-model="form.content" class="textarea content" maxlength="5000" placeholder="支持 Markdown：**加粗**、列表（「- 」开头）、链接" />
      </div>
      <div class="field">
        <label class="field__label" for="n-sec">强制阅读时间</label>
        <div class="sec-row">
          <input id="n-sec" v-model.number="form.seconds" class="text-input sec" type="number" min="0" max="60" step="1" />
          <span class="caption">秒（0 到 60，0 表示弹出后马上就能关）</span>
        </div>
      </div>
      <label v-if="!isNew" class="check-row">
        <input v-model="form.resend" type="checkbox" />
        <span>重新弹给所有人（包括已经看过的；不勾的话只有还没看过的人会看到新内容）</span>
      </label>
      <p v-if="formError" class="form-flash form-flash--error">{{ formError }}</p>
      <div class="modal-actions">
        <button class="btn" @click="editing = null">取消</button>
        <button class="btn btn-primary" :disabled="saving || !form.title.trim()" @click="save">{{ saving ? '正在保存' : isNew ? '发布' : '保存' }}</button>
      </div>
    </Modal>

    <Modal :open="!!removing" :title="`撤回「${removing?.title}」？`" subtitle="还没看到的人不会再弹，「设置 → 通知」里也不再显示。" @close="removing = null">
      <div class="modal-actions">
        <button class="btn" @click="removing = null">取消</button>
        <button class="btn btn-danger" @click="remove">撤回</button>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.head h2 { margin: 0; }
.intro { margin: 8px 0 12px; }
.skel { height: 60px; border-radius: var(--radius-md); }
.row { display: flex; align-items: center; gap: 8px; padding: 12px 0; border-top: 1px solid var(--color-divider); }
.row__text { flex: 1; min-width: 0; }
.row__text p { margin: 0; }
.row__title { color: var(--color-ink); font-size: 14px; font-weight: 500; overflow-wrap: anywhere; }
.label-row { display: flex; align-items: center; justify-content: space-between; }
.textarea.content { min-height: 200px; }
.preview { min-height: 200px; padding: 10px 12px; border: 1px solid var(--color-divider); border-radius: var(--radius-md); font-size: 14px; line-height: 1.7; }
.sec-row { display: flex; align-items: center; gap: 10px; }
.text-input.sec { width: 90px; }
</style>
