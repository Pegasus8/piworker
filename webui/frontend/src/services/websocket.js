// Credits: https://github.com/latovicalmin/vuejs-websockets-example
const VueWebSocket = {}

/* --- Options ---

  - url - Required
  - store - Required
  - reconnectInterval
  - maxReconnectInterval
*/

VueWebSocket.install = (Vue, options) => {
  let ws = new WebSocket(options.url)
  let reconnectInterval = options.reconnectInterval || 1000

  Vue.prototype.$websocket = {}

  Vue.prototype.$websocket.connect = () => {
    ws = new WebSocket(options.url)

    ws.onopen = () => {
      reconnectInterval = options.reconnectInterval || 1000
      authenticateConnection()
    }

    ws.onmessage = (event) => {
      // handle the message from the backend
      handleMessage(JSON.parse(event.data))
    }

    ws.onclose = (event) => {
      if (event) {
        // Event.code 1000 is our normal close event
        if (event.code !== 1000) {
          const maxReconnectInterval = options.maxReconnectInterval || 3000
          setTimeout(() => {
            if (reconnectInterval < maxReconnectInterval) {
              // Reconnect interval can't be > x seconds
              reconnectInterval += 1000
            }
            Vue.prototype.$websocket.connect()
          }, reconnectInterval)
        }
      }
    }

    ws.onerror = (err) => {
      ws.close()
    }
  }

  Vue.prototype.$websocket.disconnect = () => {
    ws.close()
  }

  Vue.prototype.$websocket.send = (data) => {
    // Send the data thought the WebSocket as a JSON object.
    ws.send(JSON.stringify(data))
  }

  /*
      Here we write our custom functions to not make a mess in one function
  */

  const handleMessage = (data) => {
    if (data.type !== 'stat') {
      return
    }
    options.store.dispatch('statistics/setActiveTasksCounter', data.payload.activeTasks)
    options.store.dispatch('statistics/setOnExecutionTasksCounter', data.payload.onExecutionTasks)
    options.store.dispatch('statistics/setInactiveTasksCounter', data.payload.inactiveTasks)

    options.store.dispatch('statistics/setAverageExecutionTime', data.payload.averageExecutionTime)
    options.store.dispatch('statistics/setRunningTime', data.payload.operatingTime)
  }

  const authenticateConnection = () => {
    const authData = {
      type: 'authentication',
      payload: {
        token: options.store.getters['auth/token']
      }
    }
    ws.send(JSON.stringify(authData))
  }
}

export default VueWebSocket
