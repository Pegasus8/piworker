const socket = new WebSocket(`ws://${location.host}/ws`)
let data = {}

socket.onopen = () => {
    console.log("Connected to WebSocket server")
}

socket.onerror = (error) => {
    console.log("Error when trying to connect to WebSocker server:", error)
}

socket.onmessage = (event) => {
    // Handle JSON data received from the server
    data = JSON.parse(event.data)
    console.log(data)

    if (length(data) > 0 ){
        activeTasks.number = data.Statistic.ActiveTasks
    }
 }

 socket.onclose = (event) => {
     console.log("Closing websocket connection...")
 }