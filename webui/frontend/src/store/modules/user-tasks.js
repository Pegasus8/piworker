import axios from 'axios'

const state = {
  tasks: []
}

const mutations = {
  updateTasks: (state, updatedTasks) => {
    state.tasks = updatedTasks
  }
}

const actions = {
  fetchUserTasks: ({
    commit
  }) => {
    const configs = {
      params: {
        fromWebUI: true
      }
    }
    console.info('Sending request to get-all tasks API...')
    axios.get('/api/tasks/get-all', configs)
      .then((response) => {
        console.info('Response successful, parsing tasks...')
        commit('updateTasks', response.data)
        console.info('Tasks parsed!')
      })
      .catch((err) => {
        console.error('Error on get-all tasks API:', err)
      })
  }
}

const getters = {
  tasks: (state) => {
    return state.tasks
  }
}

export default {
  namespaced: true,
  state,
  mutations,
  actions,
  getters
}
