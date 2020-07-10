import axios from 'axios'

const state = {
  triggers: [],
  actions: [],
  typesCompat: {}
}

const mutations = {
  setTriggers: (state, newTriggers) => {
    state.triggers = newTriggers
  },
  setActions: (state, newActions) => {
    state.actions = newActions
  },
  setTypesCompatList: (state, payload) => {
    state.typesCompat = payload
  }
}

const actions = {
  updateTriggersInfo: ({
    commit
  }) => {
    return new Promise((resolve, reject) => {
      axios.get('/api/webui/triggers-structs')
        .then((response) => {
          const triggersStructs = response.data
          commit('setTriggers', triggersStructs)
          resolve(response)
        })
        .catch((err) => {
          reject(err)
        })
    })
  },
  updateActionsInfo: ({
    commit
  }) => {
    return new Promise((resolve, reject) => {
      axios.get('/api/webui/actions-structs')
        .then((response) => {
          const actionsStructs = response.data
          commit('setActions', actionsStructs)
          resolve(response)
        })
        .catch((err) => {
          reject(err)
        })
    })
  },
  getTypesCompatList: ({ commit }) => {
    return new Promise((resolve, reject) => {
      axios.get('/api/info/types-compat')
        .then(response => {
          commit('setTypesCompatList', response.data)
          resolve(response)
        })
        .catch(err => {
          reject(err)
        })
    })
  }
}

const getters = {
  triggers: (state) => {
    return state.triggers
  },
  actions: (state) => {
    return state.actions
  },
  typesCompat: (state) => {
    return state.typesCompat
  }
}

export default {
  namespaced: true,
  state,
  mutations,
  actions,
  getters
}
