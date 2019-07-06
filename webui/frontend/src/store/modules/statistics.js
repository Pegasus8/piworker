const state = {
  activeTasksCounter: 0,
  onExecutionTasksCounter: 0,
  inactiveTasksCounter: 0
}

const mutations = {
  setActiveTasksCounter: (state, number) => {
    state.activeTasksCounter = number
  },
  setOnExecutionTasksCounter: (state, number) => {
    state.onExecutionTasksCounter = number
  },
  setInactiveTasksCounter: (state, number) => {
    state.inactiveTasksCounter = number
  }
}

const actions = {
  setActiveTasksCounter: ({ commit }, number) => {
    commit('setActiveTasksCounter', number)
  },
  setOnExecutionTasksCounter: ({ commit }, number) => {
    commit('setOnExecutionTasksCounter', number)
  },
  setInactiveTasksCounter: ({ commit }, number) => {
    commit('setInactiveTasksCounter', number)
  }
}

const getters = {
  activeTasksCounter: (state) => {
    return state.activeTasksCounter
  },
  onExecutionTasksCounter: (state) => {
    return state.onExecutionTasksCounter
  },
  inactiveTasksCounter: (state) => {
    return state.inactiveTasksCounter
  }
}

export default {
  namespaced: true,
  state,
  mutations,
  actions,
  getters
}
