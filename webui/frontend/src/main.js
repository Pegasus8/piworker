import Vue from 'vue'
import App from './App.vue'
import router from './router'
import store from './store/store'
import axios from 'axios'
import UUID from 'vue-uuid'

import 'bootstrap'; import 'bootstrap/dist/css/bootstrap.min.css'

Vue.config.productionTip = false
Vue.use(UUID)

// FIXME Check if https enabled on server
axios.defaults.baseURL = `https://${location.host}/api`

new Vue({
  router,
  store,
  render: h => h(App)
}).$mount('#app')
