//TODO: use ip of raspberry pi instead of localhost
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

    //TODO: implement ui update of elements parsing JSON data
    document.getElementById("test").innerText = data.activeTasksTest.activeTasks
 }

 socket.onclose = (event) => {
     console.log("Closing websocket connection...")
 }