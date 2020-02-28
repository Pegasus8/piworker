import axios from 'axios'
import { uuid } from 'vue-uuid'

const state = {
  taskname: '',
  taskState: '',
  triggerSelected: [],
  actionsSelected: []
}

const mutations = {
  setTaskname: (state, name) => {
    state.taskname = name
  },
  setTaskState: (state, newTaskState) => {
    state.taskState = newTaskState
  },
  setTrigger: (state, trigger) => {
    // state.triggerSelected.push(newTrigger)
    if (!trigger) {
      state.triggerSelected = []
      return
    }
    // This will be used only as a reference for `key` prop.
    trigger.internalID = uuid.v4()
    // For `v-model` of `v-expansion-panels`.
    trigger.openArg = null
    // JSON.stringify && JSON.parse create a deep copy of the trigger
    state.triggerSelected = [JSON.parse(JSON.stringify(trigger))]
  },
  removeTrigger: (state, triggerInternalID) => {
    const index = state.triggerSelected.findIndex(t => t.internalID === triggerInternalID)
    state.triggerSelected.splice(index, 1)
  },
  setActions: (state, actions) => {
    for (const [index, action] of actions.entries()) {
      action.order = index
    }
    state.actionsSelected = actions
  },
  addAction: (state, action) => {
    action.order = state.actionsSelected.length
    // This will be used only as a reference for `key` prop.
    action.internalID = uuid.v4()
    // For `v-model` of `v-expansion-panels`.
    action.openArg = null
    // JSON.stringify && JSON.parse create a deep copy of the action
    state.actionsSelected.push(JSON.parse(JSON.stringify(action)))
  },
  removeAction: (state, actionInternalID) => {
    const index = state.actionsSelected.findIndex(a => a.internalID === actionInternalID)
    state.actionsSelected.splice(index, 1)
  },
  updateActionsOrder: (state) => {
    for (const [index, action] of state.actionsSelected.entries()) {
      action.order = index
    }
  }
}

const actions = {
  setTrigger: ({ commit }, trigger) => {
    commit('setTrigger', trigger)
  },
  removeTrigger: ({ commit }, triggerInternalID) => {
    commit('removeTrigger', triggerInternalID)
  },
  setActions: ({ commit }, actions) => {
    commit('setActions', actions)
  },
  addAction: ({ commit }, action) => {
    commit('addAction', action)
  },
  removeAction: ({ commit }, actionInternalID) => {
    commit('removeAction', actionInternalID)
  },
  updateActionsOrder: ({ commit }) => {
    commit('updateActionsOrder')
  },
  submitTask: ({ state }) => {
    return new Promise((resolve, reject) => {
      const newTaskData = {
        task: {
          name: state.taskname,
          state: state.taskState ? 'Active' : 'Inactive',
          // Only send one trigger. This is because, for now, multi-triggers are not supported.
          trigger: state.triggerSelected[0],
          actions: state.actionsSelected
        }
      }

      // TODO Check the integrity of the data

      console.info("Sending the data to the new tasks's API")
      axios.post('/api/tasks/new', newTaskData)
        .then((response) => {
          resolve(response)
        })
        .catch((err) => {
          reject(err)
        })
    })
  },
  updateTask: ({ state }, taskID) => {
    return new Promise((resolve, reject) => {
      const configs = {
        params: {
          id: taskID
        }
      }
      const updatedTaskData = {
        task: {
          name: state.taskname,
          state: state.taskState ? 'Active' : 'Inactive',
          // Only send one trigger. This is because, for now, multi-triggers are not supported.
          trigger: state.triggerSelected[0],
          actions: state.actionsSelected
        }
      }

      axios.put('/api/tasks/update', updatedTaskData, configs)
        .then((response) => {
          resolve(response)
        })
        .catch((err) => {
          reject(err)
        })
    })
  }
}

const getters = {
  taskname: (state) => {
    return state.taskname
  },
  taskState: (state) => {
    return state.taskState
  },
  triggerSelected: (state) => {
    return state.triggerSelected
  },
  actionsSelected: (state) => {
    return state.actionsSelected
  }
}

export default {
  namespaced: true,
  state,
  mutations,
  actions,
  getters
}
