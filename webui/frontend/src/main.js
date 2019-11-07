import Vue from 'vue'
import App from './App.vue'
import router from './router'
import store from './store/store'
import axios from 'axios'
import UUID from 'vue-uuid'
import BootstrapVue from 'bootstrap-vue'
import PortalVue from 'portal-vue'

import 'bootstrap'; import 'bootstrap/dist/css/bootstrap.min.css'
import 'bootstrap-vue/dist/bootstrap-vue.css'

Vue.config.productionTip = false
Vue.use(UUID)
Vue.use(BootstrapVue)
Vue.use(PortalVue)

// Use the protocol used to access the WebUI (HTTP/S)
axios.defaults.baseURL = `${location.protocol}//${location.host}`

new Vue({
  router,
  store,
  render: h => h(App)
}).$mount('#app')
