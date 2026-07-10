import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import i18n, { initI18n } from './i18n'
import './styles.css'

async function bootstrap() {
  const preferredTheme = localStorage.getItem('asterrouter_theme')
  const darkMode = preferredTheme === 'dark' || (!preferredTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = darkMode ? 'dark' : 'light'
  await initI18n()
  createApp(App).use(createPinia()).use(router).use(i18n).mount('#app')
}

bootstrap()
