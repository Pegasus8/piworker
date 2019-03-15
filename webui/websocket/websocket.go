package websocket

import (
	"log"
	"net/http"
	"time"

	"github.com/Pegasus8/piworker/webui/websocket/stats"
	"github.com/gorilla/websocket"
)

// Read and write buffer sizes
var upgrader = websocket.Upgrader{
	ReadBufferSize: 1024,
	WriteBufferSize: 1024,
}

// Upgrade func takes incoming connections and upgrade the request into a WebSocket connection
func Upgrade(w http.ResponseWriter, request *http.Request) (*websocket.Conn, error) {

	// Allow other origins connection
	upgrader.CheckOrigin = func(r *http.Request) bool {
		return true
	}

	// WebSocket connection
	ws, err := upgrader.Upgrade(w, request, nil)
	if err != nil {
		log.Println(err)
		return ws, err
	}
	log.Println("Connected with:",request.RemoteAddr)
	// Return WebSocket connection
	return ws, nil
}

// Writer func sends data into WebSocket to the client
func Writer(conn *websocket.Conn) {

	
	// Other way to do that
	// // for {
	// // 	ticker := time.NewTicker(5 * time.Second)
	// // 	for t := range ticker.C { ... }
	// // }
	
	log.Println("Sending data to ", conn.RemoteAddr())
	// Send data to client every 5 secs
	for range time.Tick(5 * time.Second) {
		// Get data
		data, err := stats.GetStats()
		if err != nil {
			log.Println(err)
		}
	
		// Send data
		err = conn.WriteMessage(websocket.TextMessage, []byte(data))
		if err != nil {
			log.Println(err)
			return
		}
	}
}
	
