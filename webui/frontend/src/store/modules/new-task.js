import axios from 'axios'

const cloneObj = (obj) => {
  if (obj == null || typeof obj !== 'object') return obj
  var copy = obj.constructor()
  for (var attr in obj) {
    if (obj.hasOwnProperty(attr)) copy[attr] = obj[attr]
  }
  return copy
}

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
    const newTrigger = cloneObj(trigger)
    state.triggerSelected = [newTrigger]
  },
  removeTrigger: (state, triggerIndex) => {
    state.triggerSelected.splice(triggerIndex, 1)
  },
  setActions: (state, actions) => {
    state.actionsSelected = actions
  },
  addAction: (state, action) => {
    const newAction = cloneObj(action)
    state.actionsSelected.push(newAction)
  },
  removeAction: (state, actionIndex) => {
    state.actionsSelected.splice(actionIndex, 1)
  }
}

const actions = {
  submitData: ({ state }) => {
    // TODO Modify the object according to the API requirements
    const newTaskData = {
      'task': {
        'name': state.taskname,
        'state': state.taskState,
        // Only send one trigger. This is because, for now, multi-triggers are not supported.
        'trigger': state.triggerSelected[0],
        'actions': state.actionsSelected
      }
    }

    // TODO Check the integrity of the data

    console.info("Sending the data to the new tasks's API")
    axios.post('/api/tasks/new', newTaskData)
      .then((response) => {
        console.info('Data submitted correctly, response:', response)
      })
      .catch((err) => {
        console.error(err)
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
