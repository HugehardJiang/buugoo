<script setup lang="ts">
// 设置 → 技能与插件：管理员装好的 skill 和插件，用户可以只在自己这里关掉。
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type Extension } from '../api'
import { session } from '../session'
import { toast } from '../toast'

const name = computed(() => session.meta?.name ?? '布谷')
const list = ref<Extension[] | null>(null)
const loadError = ref('')
const saving = ref('')

const skills = computed(() => list.value?.filter((x) => x.kind === 'skill') ?? [])
const plugins = computed(() => list.value?.filter((x) => x.kind === 'plugin') ?? [])

onMounted(load)

async function load() {
  try {
    list.value = (await api.extensions()).extensions
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : '加载失败'
  }
}

async function toggle(x: Extension) {
  const key = `${x.kind}/${x.name}`
  if (saving.value) return
  saving.value = key
  x.enabled = !x.enabled
  try {
    await api.toggleExtension(x.kind, x.name, x.enabled)
  } catch (e) {
    x.enabled = !x.enabled
    toast(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = ''
  }
}
</script>

<template>
  <section id="extensions" class="panel panel--inset">
    <h2>技能与插件</h2>
    <p class="muted intro">
      管理员给{{ name }}装的技能（skill）和插件，聊天时{{ name }}会在需要的时候自己用，比如查天气、处理文档。不想让它在你这里用某一个，关掉就行，不影响别人。
    </p>
    <p v-if="loadError" class="form-flash form-flash--error">{{ loadError }}</p>
    <div v-else-if="list === null" class="skeleton skel" aria-hidden="true" />
    <p v-else-if="!list.length" class="caption">管理员还没有安装技能或插件。管理员可以在「管理后台 → 技能与插件」里安装。</p>
    <template v-else>
      <template v-for="group in [{ label: '技能', items: skills }, { label: '插件', items: plugins }]" :key="group.label">
        <p v-if="group.items.length" class="group">{{ group.label }}</p>
        <div v-if="group.items.length" class="ext-grid">
          <div v-for="x in group.items" :key="`${x.kind}/${x.name}`" class="row" :class="{ 'is-off': !x.enabled }">
            <div class="row__text">
              <p class="row__title">{{ x.name }}</p>
              <p class="caption row__desc">{{ x.description }}</p>
            </div>
            <button
              type="button"
              class="switch"
              role="switch"
              :aria-checked="x.enabled"
              :aria-label="`${x.enabled ? '关闭' : '打开'} ${x.name}`"
              :class="{ 'is-on': x.enabled }"
              :disabled="saving === `${x.kind}/${x.name}`"
              @click="toggle(x)"
            >
              <span class="switch__knob" />
            </button>
          </div>
        </div>
      </template>
    </template>
  </section>
</template>

<style scoped>
.intro { margin: 0 0 16px; }
.skel { height: 72px; border-radius: 18px; }
.group { margin: 20px 0 10px; color: var(--color-muted); font-size: 12.5px; font-weight: 500; letter-spacing: .04em; }
.ext-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 12px; }
.row {
  display: flex; align-items: flex-start; gap: 14px; padding: 16px 18px;
  border: 0; border-radius: 20px; background: var(--md-surface-container-low);
  transition: background-color var(--motion-fast) var(--md-ease), box-shadow var(--motion-fast) var(--md-ease), opacity var(--motion-fast) ease;
}
.row:hover { background: var(--md-surface-container); box-shadow: var(--md-elev-1); }
.row.is-off { opacity: .72; }
.row__text { flex: 1; min-width: 0; }
.row__title { margin: 0 0 4px; color: var(--color-ink); font-size: 14px; font-weight: 500; font-family: var(--font-mono); overflow-wrap: anywhere; }
.row__desc { margin: 0; overflow-wrap: anywhere; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; line-height: 1.55; }
.switch { margin-top: 2px; }
@media (max-width: 640px) {
  .ext-grid { grid-template-columns: 1fr; }
  .row { padding: 14px 16px; border-radius: 16px; }
}
</style>
