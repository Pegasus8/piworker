const state = {
  activeTasksCounter: 0,
  onExecutionTasksCounter: 0,
  inactiveTasksCounter: 0,
  completedTasksCounter: 0,

  averageExecutionTime: 0.0,
  runningTime: 0,
  backupLoopState: false,

  raspberryStats: {
    temperature: 0.0,
    cpuLoad: "",
    freeStorage: "",
    ramUsage: "",
    timestamp: null
  }
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
  },
  setCompletedTasksCounter: (state, number) => {
    state.completedTasksCounter = number
  },

  setAverageExecutionTime: (state, number) => {
    state.averageExecutionTime = number
  },
  setRunningTime: (state, number) => {
    state.runningTime = number
  },
  setBackupLoopState: (state, newState) => {
    state.backupLoopState = newState
  },

  setRaspberryStatistics: (state, rpistats) => {
    state.raspberryStats = rpistats
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
  },
  setCompletedTasksCounter: ({ commit }, number) => {
    commit('setCompletedTasksCounter', number)
  },

  setAverageExecutionTime: ({ commit }, number) => {
    commit('setAverageExecutionTime', number)
  },
  setRunningTime: ({ commit }, number) => {
    commit('setRunningTime', number)
  },
  setBackupLoopState: ({ commit }, state) => {
    commit('setBackupLoopState', state)
  },

  setRaspberryStatistics: ({ commit }, rpistats) => {
    commit('setRaspberryStatistics', rpistats)
  }
}

const getters = {
  // Tasks statistics
  activeTasksCounter: (state) => {
    return state.activeTasksCounter
  },
  onExecutionTasksCounter: (state) => {
    return state.onExecutionTasksCounter
  },
  inactiveTasksCounter: (state) => {
    return state.inactiveTasksCounter
  },
  completedTasksCounter: (state) => {
    return state.completedTasksCounter
  },
  // General statistics
  averageExecutionTime: (state) => {
    return state.averageExecutionTime
  },
  runningTime: (state) => {
    return state.runningTime
  },
  backupLoopState: (state) => {
    if (state.backupLoopState) return 'active'
    else return 'inactive'
  },
  // RPi statistics
  raspberryStats: (state) => {
    return state.raspberryStats
  }
}

export default {
  namespaced: true,
  state,
  mutations,
  actions,
  getters
}
