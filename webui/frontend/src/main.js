import Vue from 'vue'
import App from './App.vue'
import router from './router'
import store from './store/store'
import axios from 'axios'
import UUID from 'vue-uuid'

import vuetify from './plugins/vuetify'
require('typeface-roboto')

Vue.config.productionTip = false
Vue.use(UUID)

// Use the protocol used to access the WebUI (HTTP/S)
axios.defaults.baseURL = `${location.protocol}//${location.host}`

new Vue({
  router,
  store,
  vuetify,
  render: h => h(App)
}).$mount('#app')
