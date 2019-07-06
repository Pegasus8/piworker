import Vue from 'vue'
import Router from 'vue-router'
import Statistics from './views/Statistics.vue'

Vue.use(Router)

export default new Router({
  mode: 'history',
  base: process.env.BASE_URL,
  routes: [
    {
      path: '/statistics',
      component: Statistics,
      name: 'statistics',
      beforeEnter: (to, from, next) => {
        // Check for authentication
        next()
      }
    }
  ]
})
