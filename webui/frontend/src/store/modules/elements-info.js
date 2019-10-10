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
    updateTriggersInfo: ({ commit }) => {
        console.info('Updating triggers info...')
        axios.get('/api/webui/triggers-structs')
            .then((response) => {
                commit('setTriggers', response.data)
                console.info('Triggers info updated successfully!')
            })
            .catch((err) => {
                console.error(err)
            }) 
    },
    updateActionsInfo: ({ commit }) => {
        console.info('Updating actions info...')
        axios.get('/api/webui/actions-structs')
            .then((response) => {
                commit('setActions', response.data)
                console.info('Actions info updated successfully!')
            })
            .catch((err) => {
                console.error(err)
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