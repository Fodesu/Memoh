import { createApp } from 'vue'
import './style.css'
import App from './App.vue'
import router from './router'
import { createPinia } from 'pinia'
import i18n from './i18n'
import '@memoh/ui/style.css'

createApp(App)
  .use(createPinia())
  .use(router)
  .use(i18n)
  .mount('#app')
