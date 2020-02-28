import axios from 'axios'

const state = {
  triggers: [],
  actions: []
}

const mutations = {
  setTriggers: (state, newTriggers) => {
    state.triggers = newTriggers
  },
  setActions: (state, newActions) => {
    state.actions = newActions
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
  }
}

const getters = {
  triggers: (state) => {
    return state.triggers
  },
  actions: (state) => {
    return state.actions
  }
}

export default {
  namespaced: true,
  state,
  mutations,
  actions,
  getters
}
