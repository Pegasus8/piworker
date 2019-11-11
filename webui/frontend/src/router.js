import Vue from 'vue'
import Router from 'vue-router'
import store from './store/store.js'
import StatisticsView from './views/Statistics.vue'
import ManagementView from './views/Management.vue'
import SettingsView from './views/Settings.vue'
import LoginView from './views/Login.vue'
import NewTaskView from './views/NewTask.vue'

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
        // Check if the user is already authenticated.
        if (store.getters['auth/isAuthenticated']) {
          // If authenticated, redirect to statistics view.
          next({ name: 'statistics' })
        } else {
          // If not, try an autologin. It will recover (if exists) credentials stored
          // on local storage.
          store.dispatch('auth/tryAutologin')
          // Check if the autologin was successful.
          if (store.getters['auth/isAuthenticated']) {
            // If successful, redirect to statistics view.
            next({ name: 'statistics' })
          } else {
            // If not, continue to login view.
            next()
          }
        }
      }
    },
    {
      path: '/statistics',
      component: StatisticsView,
      name: 'statistics',
      beforeEnter: (to, from, next) => {
        // NOTE Only is needed try autologin here (and on the login view) because is the path by default.
        // Check if the user is already authenticated.
        if (!store.getters['auth/isAuthenticated']) {
          // If not authenticated, try an autologin. It will recover (if exists) credentials stored
          // on local storage.
          store.dispatch('auth/tryAutologin')
          // Check if the autologin was successful.
          if (store.getters['auth/isAuthenticated']) {
            // If successful, continue to statistics view.
            next()
          } else {
            // If not, redirect to login view.
            next({ name: 'login' })
          }
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
        if (!store.getters['auth/isAuthenticated']) {
          // If not authenticated, redirect to login view.
          next({ name: 'login' })
        } else {
          next()
        }
      },
      children: []
    },
    {
      path: '/settings',
      component: SettingsView,
      name: 'settings',
      beforeEnter: (to, from, next) => {
        if (!store.getters['auth/isAuthenticated']) {
          // If not authenticated, redirect to login view.
          next({ name: 'login' })
        } else {
          next()
        }
      }
    },
    {
      path: '/new-task',
      component: NewTaskView,
      name: 'new-task',
      beforeEnter: (to, from, next) => {
        if (!store.getters['auth/isAuthenticated']) {
          // If not authenticated, redirect to login view.
          next({ name: 'login' })
        } else {
          next()
        }
      }
    },
    {
      path: '*',
      redirect: { name: 'statistics' }
    }
  ]
})
