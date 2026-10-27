import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './style.css'

// 等第一次路由跳转完成再挂载：否则未登录时会先闪一下聊天页的侧边栏，再跳到介绍主页或登录页。
const app = createApp(App).use(router)
router.isReady().finally(() => app.mount('#app'))
