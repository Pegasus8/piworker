import Vue from 'vue'
import Router from 'vue-router'
import StatisticsView from './views/Statistics.vue'
import ManagementView from './views/Management.vue'

Vue.use(Router)

export default new Router({
  mode: 'history',
  base: process.env.BASE_URL,
  routes: [
    {
      path: '/statistics',
      component: StatisticsView,
      name: 'statistics',
      beforeEnter: (to, from, next) => {
        // TODO Check for authentication
        next()
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
    }
  ]
})
