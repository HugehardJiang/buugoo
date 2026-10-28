<script setup lang="ts">
// 定时任务：提醒、定时任务与主动关心。
import ProactivePanel from '../components/ProactivePanel.vue'
</script>

<template>
  <main class="main">
    <header class="topbar">
      <div class="topbar__titles">
        <h1>提醒与定时任务</h1>
      </div>
    </header>
    <div class="page">
      <div class="page__inner">
        <ProactivePanel />
      </div>
    </div>
  </main>
</template>
