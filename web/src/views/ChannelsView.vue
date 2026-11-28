<script setup lang="ts">
// 聊天软件：扫码连接自己的微信，用绑定码绑定 QQ、Telegram。
import ChannelsPanel from '../components/ChannelsPanel.vue'
</script>

<template>
  <main class="main">
    <header class="topbar">
      <div class="topbar__titles">
        <h1>聊天软件</h1>
      </div>
    </header>
    <div class="page">
      <div class="page__inner channels-wrap">
        <ChannelsPanel />
      </div>
    </div>
  </main>
</template>

<style scoped>
.channels-wrap { max-width: 1040px; }
.channels-wrap :deep(.panel) {
  border: 1px solid var(--color-hairline-subtle, rgba(20, 20, 19, .06));
  border-radius: var(--radius-xxl);
}
.channels-wrap :deep(h2) {
  font-family: var(--font-sans); font-size: 20px; font-weight: 500; letter-spacing: -.02em;
}
</style>
