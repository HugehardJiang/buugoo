<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { session } from './session'

const route = useRoute()
const showNav = computed(() => route.name !== 'login' && session.loaded && session.meta)
const name = computed(() => session.meta?.name ?? '布谷')
</script>

<template>
  <div class="shell">
    <header v-if="showNav" class="topbar">
      <RouterLink to="/" class="brand">
        <img src="/favicon.svg" alt="" width="28" height="28" />
        <span>{{ name }}</span>
      </RouterLink>
      <nav>
        <RouterLink to="/" exact-active-class="active">聊天</RouterLink>
        <RouterLink to="/memories" active-class="active">记忆</RouterLink>
        <RouterLink to="/settings" active-class="active">设置</RouterLink>
      </nav>
    </header>
    <div v-if="session.loaded && !session.meta" class="fatal">无法连接到布谷服务，请确认 buugoo serve 正在运行后刷新页面。</div>
    <RouterView v-else />
  </div>
</template>
