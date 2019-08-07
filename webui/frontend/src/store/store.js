import Vue from 'vue'
import Vuex from 'vuex'
import auth from './modules/auth'
import statistics from './modules/statistics'
import newTask from './modules/new-task'

Vue.use(Vuex)

export default new Vuex.Store({
  state: {
  },
  mutations: {
  },
  actions: {
  },
  getters: {
  },
  modules: {
    auth,
    statistics,
    newTask
  }
})
