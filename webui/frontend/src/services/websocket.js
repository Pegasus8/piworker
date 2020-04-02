// Credits: https://github.com/latovicalmin/vuejs-websockets-example
const VueWebSocket = {}

/* --- Options ---

  - url                  - Required
  - store                - Required
  - reconnectInterval    - Default: 1000ms
  - maxReconnectInterval - Default: 3000ms
  - connectManually      - Default: false
*/

VueWebSocket.install = (Vue, options) => {
  let ws
  let reconnectInterval = options.reconnectInterval || 1000
  const maxReconnectInterval = options.maxReconnectInterval || 3000
  const connectManually = options.connectManually || false

  if (!connectManually) {
    ws = new WebSocket(options.url)
  }

  Vue.prototype.$websocket = {}

  Vue.prototype.$websocket.connect = () => {
    if (ws == null) {
      // Initialize the websocket
      ws = new WebSocket(options.url)
    } else {
      // Close the current connection and replace the previous instance of WebSocket.
      ws.close()
      ws = new WebSocket(options.url)
    }

    ws.onopen = () => {
      authenticateConnection()
    }

    ws.onmessage = (event) => {
      // Handle the messages from the backend.
      handleMessage(JSON.parse(event.data))
    }

    ws.onclose = (event) => {
      if (event) {
        // Event.code 1000 is our normal close event
        if (event.code !== 1000) {
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

    ws.onerror = (_err) => {
      // TODO Store the error on vuex, for a posterior notification on the frontend.
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
    options.store.dispatch('statistics/setRaspberryStatistics', {
      temperature: 0.0, // TODO
      cpuLoad: data.payload.cpuLoad.toFixed(2),
      freeStorage: formatBytes(data.payload.storage.free),
      ramUsage: formatBytes(data.payload.ram.used)
    })
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

  const formatBytes = (bytes, decimals = 2) => {
    if (bytes === 0) return '0 Bytes'

    const k = 1024
    const dm = decimals < 0 ? 0 : decimals
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB']

    const i = Math.floor(Math.log(bytes) / Math.log(k))

    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i]
  }
}

export default VueWebSocket
