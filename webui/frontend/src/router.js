import Vue from 'vue'
import Router from 'vue-router'
import store from './store/store.js'
import StatisticsView from './views/Statistics.vue'
import ManagementView from './views/Management.vue'
import SettingsView from './views/Settings.vue'
import LoginView from './views/Login.vue'

Vue.use(Router)

export default new Router({
  mode: 'history',
  base: process.env.BASE_URL,
  routes: [
    {
      path: '/login',
      component: LoginView,
      name: 'login',
      beforeEnter: (to, from, next) => {
        store.dispatch('auth/tryAutologin')
        if (store.getters['auth/isAuthenticated']) {
          next({ name: 'statistics' })
        } else {
          next()
        }
      }
    },
    {
      path: '/statistics',
      component: StatisticsView,
      name: 'statistics',
      beforeEnter: (to, from, next) => {
        store.dispatch('auth/tryAutologin')
        if (!store.getters['auth/isAuthenticated']) {
          next({ name: 'login' })
        } else {
          next()
        }
      }
    },
    {
      path: '/management',
      component: ManagementView,
      name: 'management',
      beforeEnter: (to, from, next) => {
        // TODO Check for authentication
        next()
      },
      children: []
    },
    {
      path: '/settings',
      component: SettingsView,
      name: 'settings',
      beforeEnter: (to, from, next) => {
        // TODO Check for authentication
        next()
      }
    },
    {
      path: '*',
      redirect: { name: 'statistics' }
    }
  ]
})
