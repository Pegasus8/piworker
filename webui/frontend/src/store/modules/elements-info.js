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
                console.info('Good response from triggers-structs API, parsing triggers...')
                const triggersStructs = response.data
                commit('setTriggers', triggersStructs)
                console.info('Triggers info updated successfully!')
            })
            .catch((err) => {
                console.error('Error on triggers-structs API',err)
            }) 
    },
    updateActionsInfo: ({ commit }) => {
        console.info('Updating actions info...')
        axios.get('/api/webui/actions-structs')
            .then((response) => {
                console.info('Good response from actions-structs API, parsing actions...')
                const actionsStructs = response.data
                commit('setActions', actionsStructs)
                console.info('Actions info updated successfully!')
            })
            .catch((err) => {
                console.error('Error on actions-structs API',err)
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