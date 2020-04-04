import axios from 'axios'
import router from '../../router'

const state = {
  tokenID: null,
  userID: null,
  user: null,
  admin: false
}

const mutations = {
  authUser: (state, userData) => {
    state.tokenID = userData.token
    state.userID = userData.userID
    state.admin = userData.admin
  },
  storeUser: (state, user) => {
    state.user = user
  },
  clearAuthData: (state) => {
    state.tokenID = null
    state.userID = null
    state.user = null
    state.admin = false
  }
}

const actions = {
  // `expirationTime` must be seconds
  setLogoutTimer: ({ dispatch }, expirationTime) => {
    const now = new Date()
    const timeout = (expirationTime * 1000) - now.getTime()
    setTimeout(() => {
      dispatch('logout')
    }, timeout)
  },
  logout: ({ commit }) => {
    commit('clearAuthData')
    localStorage.removeItem('token')
    localStorage.removeItem('userID')
    localStorage.removeItem('expirationTime')
    localStorage.removeItem('admin')
    router.replace('/login')
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
      console.warn('Token expired, autologin canceled')
      return
    }
    // Token still valid
    const userID = localStorage.getItem('userID')
    const admin = localStorage.getItem('admin')
    commit('authUser', {
      token,
      userID,
      admin: admin === 'true'
    })

    axios.defaults.headers.common.Authorization = 'Bearer ' + token
  },
  login: ({ commit, dispatch }, authData) => {
    return new Promise((resolve, reject) => {
      axios.post('/api/login', {
        username: authData.user,
        password: authData.password
      })
        .then((response) => {
          if (response.data.token === '') {
            console.warn('Server rejected username or password')
            resolve({ successful: false })
            return
          }

          const expirationDate = new Date(response.data.expiresAt * 1000) // Seconds to milliseconds

          localStorage.setItem('token', response.data.token)
          localStorage.setItem('userID', authData.user)
          localStorage.setItem('expirationTime', expirationDate)
          localStorage.setItem('admin', response.data.admin)

          commit('authUser', {
            tokenID: response.data.token,
            userID: authData.user,
            admin: response.data.admin
          })
          dispatch('setLogoutTimer', response.data.expiresAt)

          axios.defaults.headers.common.Authorization = 'Bearer ' + response.data.token
          router.replace({ name: 'statistics' })
          resolve({ successful: true })
        })
        .catch((err) => {
          reject(err)
        })
    })
  }
}

const getters = {
  user: (state) => {
    return state.userID
  },
  token: (state) => {
    return state.tokenID
  },
  isAuthenticated: (state) => {
    return state.tokenID !== null
  },
  isAdmin: (state) => {
    return state.admin
  }
}

export default {
  namespaced: true,
  state,
  mutations,
  actions,
  getters
}
