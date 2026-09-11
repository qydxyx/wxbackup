import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { applyTheme, readTheme } from './theme'
import './styles.css'

applyTheme(readTheme())

createApp(App).use(createPinia()).use(router).mount('#app')
