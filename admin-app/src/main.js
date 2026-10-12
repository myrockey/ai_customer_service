import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import { initTheme } from './theme'
import { session } from './api'
import './styles/theme.css'
import './styles/base.css'

initTheme()

const app = createApp(App)

// v-perm 按钮权限指令：无权限时移除元素。用法：v-perm="'quickreply:edit'"
// 平台管理员 / 租户管理员（app_key 登录）默认放行
app.directive('perm', {
  mounted(el, binding) {
    const perm = binding.value
    if (!perm) return
    if (session.is_platform_admin || session.is_admin) return
    if (!session.permissions.includes(perm)) el.remove()
  }
})

app.use(router).mount('#app')
