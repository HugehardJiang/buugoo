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
      <div class="page__inner">
        <ChannelsPanel />
      </div>
    </div>
  </main>
</template>
