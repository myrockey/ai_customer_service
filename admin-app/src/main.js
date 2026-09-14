import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import { initTheme } from './theme'
import './styles/theme.css'
import './styles/base.css'

initTheme()

createApp(App).use(router).mount('#app')
