<script setup lang="ts">
// 提醒与待办：待办清单；提醒、定时任务与主动关心（服务器开了提醒时）。
import ProactivePanel from '../components/ProactivePanel.vue'
import TodoPanel from '../components/TodoPanel.vue'
import { session } from '../session'
</script>

<template>
  <main class="main">
    <header class="topbar">
      <div class="topbar__titles">
        <h1>{{ session.meta?.reminders ? '提醒与待办' : '待办' }}</h1>
      </div>
    </header>
    <div class="page">
      <div class="page__inner stack">
        <TodoPanel />
        <ProactivePanel v-if="session.meta?.reminders" />
      </div>
    </div>
  </main>
</template>

<style scoped>
.stack { display: grid; gap: 16px; }
</style>
