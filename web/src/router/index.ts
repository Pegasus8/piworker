import { createRouter, createWebHistory } from 'vue-router'
import FlowsView from '@/views/FlowsView.vue'
import EditorView from '@/views/EditorView.vue'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/account', name: 'account', component: () => import('@/views/AccountView.vue'), meta: { requiresAuth: true } },
    {
      path: '/',
      name: 'flows',
      component: FlowsView,
      meta: { requiresAuth: true }
    },
    {
      path: '/editor/:id?',
      name: 'editor',
      component: EditorView,
      meta: { requiresAuth: true }
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { requiresAuth: false }
    }
  ]
})

// Navigation guard for authentication
router.beforeEach((to, _from, next) => {
  const authStore = useAuthStore()
  const requiresAuth = to.meta.requiresAuth !== false

  if (requiresAuth && !authStore.isAuthenticated) {
    // Redirect to login with return URL
    next({
      name: 'login',
      query: { redirect: to.fullPath }
    })
  } else if (to.name === 'login' && authStore.isAuthenticated) {
    // Already authenticated, redirect to home
    next({ name: 'flows' })
  } else {
    next()
  }
})

export default router
