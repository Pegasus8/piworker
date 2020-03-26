import axios from 'axios'

const groupRS = (rs) => {
  const stats = {
    timestamps: [],
    cpuLoad: [],
    hsBootTime: [],
    hsUpTime: [],
    hsTemperatures: [],
    sFree: [],
    sUsed: [],
    sUsedPercent: [],
    rAvailable: [],
    rUsed: []
  }

  if (rs == null || !rs.length > 0) {
    return stats
  }

  rs.forEach(s => {
    stats.timestamps.push(formatTimestamp(s.timestamp))
    stats.cpuLoad.push(s.cpuLoad)
    stats.hsBootTime.push(s.hostStats.bootTime)
    stats.hsUpTime.push(s.hostStats.uptime)
    stats.hsTemperatures.push(s.hostStats.temperatures)
    stats.sFree.push(s.storage.free)
    stats.sUsed.push(s.storage.used)
    stats.sUsedPercent.push(s.storage.usedPercent)
    stats.rAvailable.push(s.ram.available)
    stats.rUsed.push(s.ram.used)
  })

  return stats
}

const groupTS = (ts) => {
  const stats = {
    timestamps: [],
    activeTasks: [],
    inactiveTasks: [],
    onExecutionTasks: [],
    failedTasks: []
  }

  if (ts == null || !ts.length > 0) {
    return stats
  }

  ts.forEach(s => {
    stats.timestamps.push(formatTimestamp(s.timestamp))
    stats.activeTasks.push(s.activeTasks)
    stats.inactiveTasks.push(s.inactiveTasks)
    stats.onExecutionTasks.push(s.onExecutionTasks)
    stats.failedTasks.push(s.failedTasks)
  })

  return stats
}

const formatTimestamp = (timestamp) => {
  const regex = /^.+T(\d{2}:\d{2}:\d{2})\..+$/
  let m

  if ((m = regex.exec(timestamp)) !== null) {
    return m[1].match().input
  }

  return ''
}

const state = {
  date: '',
  hour: null,
  viewMode: 'day',

  activeTasksCounter: 0,
  onExecutionTasksCounter: 0,
  inactiveTasksCounter: 0,
  completedTasksCounter: 0,

  averageExecutionTime: 0.0,
  runningTime: 0,
  backupLoopState: false,

  raspberryStats: {
    temperature: 0.0,
    cpuLoad: '',
    freeStorage: '',
    ramUsage: '',
    timestamp: null
  },

  ts: [],
  rs: []
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
  },

  setTasksStats: (state, ts) => {
    state.ts = ts
  },
  setRPiStats: (state, rs) => {
    state.rs = rs
  },

  setDate: (state, date) => {
    state.date = date
  },
  setHour: (state, hour) => {
    state.hour = hour
  },
  setViewMode: (state, mode) => {
    state.viewMode = mode
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
  },

  setDate: ({ commit }, date) => {
    commit('setDate', date)
  },
  setHour: ({ commit }, hour) => {
    commit('setHour', hour)
  },
  setViewMode: ({ commit }, viewMode) => {
    commit('setViewMode', viewMode)
  },

  getStats: ({ commit }, payload) => {
    const configs = {
      params: {
        date: payload.date
      }
    }

    if (payload.hour != null) {
      let h
      if (payload.hour > 9) {
        h = payload.hour + ':00'
      } else {
        h = '0' + payload.hour + ':00'
      }

      configs.params.hour = h
    }

    axios.get('/api/info/statistics', configs)
      .then(response => {
        const rs = groupRS(response.data.rpiStats)
        const ts = groupTS(response.data.tasksStats)
        commit('setRPiStats', rs)
        commit('setTasksStats', ts)
      })
      .catch(err => {
        console.error(err)
      })
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
  },
  ts: (state) => {
    return state.ts
  },
  rs: (state) => {
    return state.rs
  },
  date: (state) => {
    return state.date
  },
  hour: (state) => {
    return state.hour
  },
  viewMode: (state) => {
    return state.viewMode
  }
}

export default {
  namespaced: true,
  state,
  mutations,
  actions,
  getters
}
