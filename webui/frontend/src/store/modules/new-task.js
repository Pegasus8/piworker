// TODO Use another instance of axios due to
// the default baseURL used on this instance.
import axios from 'axios'

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
  setTrigger: (state, newTrigger) => {
    // state.triggerSelected.push(newTrigger)
    state.triggerSelected = [newTrigger]
  },
  removeTrigger: (state, triggerIndex) => {
    state.triggerSelected.splice(triggerIndex, 1)
  },
  setActions: (state, actions) => {
    state.actionsSelected = actions
  },
  addAction: (state, newAction) => {
    state.actionsSelected.push(newAction)
  },
  removeAction: (state, actionIndex) => {
    state.actionsSelected.splice(actionIndex, 1)
  },
  setActionArgContent: (state, payload) => {
    state.actionsSelected[payload.actionIndex].Args[payload.argumentIndex].Content = payload.contentToSet
  }
}

const actions = {
  submitData: ({ state }) => {
    // TODO Modify the object according to the API requirements
    const newTaskData = {
      Name: state.taskname,
      State: state.taskState,
      Trigger: state.triggerSelected,
      Actions: state.actionsSelected
    }

    // TODO Check the integrity of the data

    console.info("Sending the data to the new tasks's API")
    // TODO API url
    axios.post('', newTaskData)
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
  state,
  mutations,
  actions,
  getters
}
