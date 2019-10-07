import axios from 'axios'
import router from '../../router'

const state = {
  tokenID: null,
  userID: null,
  user: null
}

const mutations = {
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
}

const actions = {
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
      return
    }
    // Token still valid
    const userID = localStorage.getItem('userID')
    commit('authUser', {
      token,
      userID
    })
    axios.defaults.headers.common['Token'] = response.data.token
  },
  login: ({ commit, dispatch }, authData) => {
    axios.post('/api/login', { // FIXME Replace for the user MASTER_KEY
      user: authData.user,
      password: authData.password
    })
      .then((response) => {
        console.info('User logged, saving the info...')
        //  Response: {token: "", userID: "", expiresIn: ""}  //

        const now = new Date()
        const expirationDate = new Date(now.getTime() + (response.data.expiresAt))
        localStorage.setItem('token', response.data.token)
        localStorage.setItem('userID', authData.user)
        localStorage.setItem('expirationTime', expirationDate)

        commit('authUser', {
          tokenID: response.data.token,
          userID: authData.user
        })
        dispatch('setLogoutTimer', response.data.expiresAt)
        axios.defaults.headers.common['Token'] = response.data.token
      })
      .catch((err) => console.error(err))
  }
}

const getters = {
  user: (state) => {
    return state.user
  },
  isAuthenticated: (state) => {
    return state.tokenID !== null
  }
}

export default {
  namespaced: true,
  state,
  mutations,
  actions,
  getters
}
