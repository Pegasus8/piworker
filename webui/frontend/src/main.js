import Vue from 'vue'
import App from './App.vue'
import router from './router'
import store from './store'
import axios from 'axios'

Vue.config.productionTip = false

                      // FIXME Check if https enabled on server
axios.defaults.baseURL = `https://${location.host}/api`

new Vue({
  router,
  store,
  render: h => h(App)
}).$mount('#app')
