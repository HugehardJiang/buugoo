import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './style.css'
import { startImpersonation } from './api'

// 管理后台「临时登录」打开的新标签页：地址里 #imp= 后面是临时令牌（放在 # 后面不会发给服务器、不进日志），
// 存进这个标签页后马上从地址里去掉。
if (location.hash.startsWith('#imp=')) {
  startImpersonation(decodeURIComponent(location.hash.slice(5)))
  history.replaceState(null, '', location.pathname + location.search)
}

// 等第一次路由跳转完成再挂载：否则未登录时会先闪一下聊天页的侧边栏，再跳到介绍主页或登录页。
const app = createApp(App).use(router)
router.isReady().finally(() => app.mount('#app'))
