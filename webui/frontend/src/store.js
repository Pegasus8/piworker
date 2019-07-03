import Vue from 'vue'
import Vuex from 'vuex'
import router from './router'
import axios from 'axios'

Vue.use(Vuex)

export default new Vuex.Store({
  state: {
    tokenID: null,
    userID: null,
    user: null
  },
  mutations: {
    authUser: (state, userData) => {
      state.tokenID = userData.token
      state.userID = userData.userID
    },
    storeUser: (state, user) => {
      state.user = user
    },
    clearAuthData: (state) => {
      state.tokenID = null
      state.userID = null
      state.user = null
    }
  },
  actions: {
    // `expirationTime` must be seconds
    setLogoutTimer: ({ dispatch }, expirationTime) => {
      setTimeout(() => {
        dispatch('logout')
      }, expirationTime * 1000)
    },
    logout: ({ commit }) => {
      commit('clearAuthData')
      localStorage.removeItem('token')
      localStorage.removeItem('userID')
      localStorage.removeItem('expirationTime')
      router.replace('/signin') // TODO Add "/signin" path to routes's list
    },
    tryAutologin: ({ commit }) => {
      const token = localStorage.getItem('token')
      if (!token) {
        return
      }
      // If the token was obtained, then we get all the info
      const expirationTime = localStorage.getItem('expirationTime')
      const now = new Date()
      if (now >= expirationTime) {
        // Token expired
        return
      }
      // Token still valid
      const userID = localStorage.getItem('userID')
      commit('authUser', {
        token,
        userID
      })
    },
    login: ({ commit, dispatch }, authData) => {
      axios.post('/auth?key=<MASTER_KEY>', { // FIXME Replace for the user MASTER_KEY
        user: authData.user,
        password: authData.password
      })
        .then((response) => {
          console.info('User logged, saving the info...')
          //  Response: {token: "", userID: "", expiresIn: ""}  //

          const now = new Date()
          const expirationDate = new Date(now.getTime() + (response.data.expiresIn * 1000))
          localStorage.setItem('token', response.data.token)
          localStorage.setItem('userID', response.data.userID)
          localStorage.setItem('expirationTime', expirationDate)

          commit('authUser', {
            tokenID: response.data.token,
            userID: response.data.userID
          })
          dispatch('setLogoutTimer', response.data.expiresIn)
        })
        .catch((err) => console.error(err))
    }
  },
  getters: {
    user: (state) => {
      return state.user
    },
    isAuthenticated: (state) => {
      return state.tokenID !== null
    }
  }
})
