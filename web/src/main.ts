import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { useAuthStore } from './stores/auth'
import './assets/main.css'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)

// Fetch auth status before installing the router so the navigation
// guard sees the correct authEnabled value on the initial navigation.
// Uses .finally() so the app mounts even if the status check fails
// (defaults to auth enabled).
const authStore = useAuthStore()
authStore.fetchAuthStatus().finally(() => {
  app.use(router)
  app.mount('#app')
})
