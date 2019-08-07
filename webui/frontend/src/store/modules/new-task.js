// TODO Use another instance of axios due to
// the default baseURL used on this instance.
import axios from 'axios'

const state = {
  taskname: '',
  taskState: '',
  triggerSelected: [],
  actionsSelected: [{ Name: 'Action A', Description: 'A random action', ID: 4 },
    { Name: 'Action B', Description: 'A random action', ID: 2 },
    { Name: 'Action C', Description: 'A random action', ID: 5 }
  ]
}

const mutations = {
  setTaskname: (state, name) => {
    state.taskname = name
  },
  setTaskState: (state, newTaskState) => {
    state.taskState = newTaskState
  },
  setTrigger: (state, newTrigger) => {
    state.triggerSelected.push(newTrigger)
  },
  removeTrigger: (state) => {
    state.triggerSelected = []
  },
  setActions: (state, actions) => {
    state.actionsSelected = actions
  },
  addAction: (state, newAction) => {
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
