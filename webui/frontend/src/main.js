import Vue from 'vue'
import App from './App.vue'
import router from './router'
import store from './store/store'
import axios from 'axios'
import UUID from 'vue-uuid'

import 'bootstrap'; import 'bootstrap/dist/css/bootstrap.min.css'

Vue.config.productionTip = false
Vue.use(UUID)

// By default use HTTP
let protocol = 'http'

// Change port
let httpsCheckHost = location.host.replace(/:[\d]+/, ':8826')
axios.get(`http://${httpsCheckHost}/https-check`)
  .then((response) => {
    if (response.data.enabled) {
      // HTTPS support confirmed
      protocol = 'https'
      // Overwrittes base url, wich by default uses HTTP
      // (remember that this is executed asynchronous)
      axios.defaults.baseURL = `${protocol}://${location.host}`
    } else {
      console.info("PiWorker can't establish a HTTPS connection, using HTTP instead.")
    }
  })
  .catch((err) => {
    console.error(err)
  })

// By default use HTTP, if HTTPS are suported will be overwritten again
// by the async request to the https-check URL
axios.defaults.baseURL = `${protocol}://${location.host}`

new Vue({
  router,
  store,
  render: h => h(App)
}).$mount('#app')
