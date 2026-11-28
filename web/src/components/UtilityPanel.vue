<script setup lang="ts">
// 设置 → 辅助模型（管理员）：直接从现有的模型里选一个做后台任务，不用改配置文件。
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type ModelView, type UtilityView } from '../api'
import { session } from '../session'
import { toast } from '../toast'
import ModelIcon from './ModelIcon.vue'
import { shortName, sourceOf } from '../vendors'

const view = ref<UtilityView | null>(null)
const models = ref<ModelView[]>([])
const saving = ref<number | null | undefined>(undefined)
const error = ref('')
const name = computed(() => session.meta?.name ?? '布谷')
// 只能选内置模型和公共模型（个人模型用的是用户自己的 Key）。
const choices = computed(() => models.value.filter((m) => m.scope !== 'personal'))

onMounted(load)

async function load() {
  try {
    const [u, m] = await Promise.all([api.utility(), api.models()])
    view.value = u
    models.value = m.models
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function choose(id: number | null) {
  if (saving.value !== undefined || (view.value?.model_id ?? null) === id) return
  saving.value = id
  try {
    view.value = await api.setUtility(id)
    toast(id == null ? '辅助模型改回配置文件里的了' : `辅助模型换成了「${shortName(view.value.name)}」`)
  } catch (e) {
    toast(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = undefined
  }
}

function isOn(id: number | null) {
  const cur = view.value?.missing ? null : (view.value?.model_id ?? null)
  return cur === id
}
</script>

<template>
  <section id="utility" class="panel panel--inset">
    <h2>辅助模型</h2>
    <p class="muted intro">
      {{ name }}在后台做的事用这个模型：记忆太多时合并整理、判断要不要主动找对方。每轮聊天后的记忆提取和长对话摘要用的是那段对话自己的模型。选个便宜、快的模型就够了；费用按选的模型的价格计。
    </p>
    <p v-if="error" class="form-flash form-flash--error">{{ error }}</p>
    <p v-if="view?.missing" class="form-flash warn">之前选的模型已经删掉或不能用了，现在暂时用配置文件里的辅助模型。</p>
    <div v-if="view" class="util-list" role="radiogroup" aria-label="辅助模型">
      <button
        type="button"
        class="util-opt"
        role="radio"
        :aria-checked="isOn(null)"
        :class="{ 'is-on': isOn(null) }"
        :disabled="saving !== undefined"
        @click="choose(null)"
      >
        <ModelIcon :model="view.config" :size="22" />
        <span class="util-opt__main">
          <strong>配置文件里的辅助模型</strong>
          <small>{{ view.config || '沿用聊天模型的设置' }} · llm.utility</small>
        </span>
        <span class="util-opt__radio" aria-hidden="true" />
      </button>
      <button
        v-for="m in choices"
        :key="m.id"
        type="button"
        class="util-opt"
        role="radio"
        :aria-checked="isOn(m.id)"
        :class="{ 'is-on': isOn(m.id) }"
        :disabled="saving !== undefined"
        @click="choose(m.id)"
      >
        <ModelIcon :model="m.model" :name="m.name" :size="22" />
        <span class="util-opt__main">
          <strong>
            {{ shortName(m.name) }}
            <span v-if="sourceOf(m.base_url, m.name)" class="util-src">{{ sourceOf(m.base_url, m.name) }}</span>
            <span v-if="m.scope === 'builtin'" class="util-src">内置</span>
          </strong>
          <small>{{ m.model }}</small>
        </span>
        <span class="util-opt__radio" aria-hidden="true" />
      </button>
    </div>
    <p v-if="view && !choices.length" class="caption">还没有添加公共模型；在上面「模型」里添加后就可以选。</p>
  </section>
</template>

<style scoped>
.intro { margin-bottom: 16px; }
.util-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 12px; }
.util-opt {
  display: flex; align-items: center; gap: 12px; min-width: 0; padding: 14px 16px;
  border: 1px solid var(--color-hairline-subtle, rgba(20, 20, 19, .08)); border-radius: 16px;
  background: var(--color-surface); color: var(--color-ink); text-align: left;
  transition: border-color var(--motion-fast) ease, background-color var(--motion-fast) ease, box-shadow var(--motion-fast) var(--ease-editorial), transform var(--motion-fast) var(--ease-editorial);
}
.util-opt:hover:not(:disabled) { border-color: rgba(20, 20, 19, .18); box-shadow: var(--shadow-float); transform: translateY(-1px); }
.util-opt.is-on {
  border-color: var(--color-accent); background: var(--color-success-tint);
  box-shadow: 0 0 0 3px rgba(91, 122, 74, .12);
}
.util-opt:disabled { cursor: default; }
.util-opt__main { display: grid; flex: 1; min-width: 0; gap: 3px; }
.util-opt__main strong { display: flex; align-items: center; gap: 6px; overflow: hidden; font-size: 14px; font-weight: 500; white-space: nowrap; text-overflow: ellipsis; }
.util-opt__main small { overflow: hidden; color: var(--color-muted); font-size: 12px; white-space: nowrap; text-overflow: ellipsis; }
.util-src {
  flex-shrink: 0; padding: 1px 7px; border: 1px solid var(--color-hairline-subtle, rgba(20, 20, 19, .14));
  border-radius: var(--radius-pill); color: var(--color-muted); font-size: 10.5px; font-weight: 500; line-height: 15px;
}
.util-opt__radio {
  flex-shrink: 0; width: 18px; height: 18px; border: 1.5px solid rgba(20, 20, 19, .2); border-radius: 50%;
  transition: border-color .15s ease, box-shadow .2s cubic-bezier(.34, 1.56, .64, 1);
}
.util-opt.is-on .util-opt__radio { border-color: var(--color-accent); box-shadow: inset 0 0 0 4.5px var(--color-accent); }
</style>
