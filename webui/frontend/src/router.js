import Vue from 'vue'
import Router from 'vue-router'
import StatisticsView from './views/Statistics.vue'
import ManagementView from './views/Management.vue'
import TasksListSubview from './views/management-subviews/TasksList.vue'
import NewTaskSubview from './views/management-subviews/NewTask.vue'

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
      // FIXME This view can't be accessed from the menu due the dropdown item
      path: '/management',
      component: ManagementView,
      name: 'management',
      beforeEnter: (to, from, next) => {
        // TODO Check for authentication
        next()
      },
      children: [
        {
          path: 'tasks',
          component: TasksListSubview,
          name: 'tasks-list',
          beforeEnter: (to, from, next) => {
            // TODO Check for authentication
            next()
          }
        },
        {
          path: 'new',
          component: NewTaskSubview,
          name: 'new-task',
          beforeEnter: (from, to, next) => {
            // TODO Check for authentication
            next()
          }
        }
      ]
    }
  ]
})
