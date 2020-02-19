package websocket

import (
	"encoding/json"
	"net/http"

	"github.com/Pegasus8/piworker/core/stats"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Read and write buffer sizes
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
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
		log.Error().Err(err).Str("remoteAddr", request.RemoteAddr).Msg("Error when upgrading from HTTP to WebSocker protocol")
		return ws, err
	}
	log.Info().Str("remoteAddr", request.RemoteAddr).Msg("WebSocket connection successfully established")
	// Return WebSocket connection
	return ws, nil
}

// Writer func sends data into WebSocket to the client
func Writer(conn *websocket.Conn, statsChannel chan stats.Statistic) {

	// Other way to do that
	// // for {
	// // 	ticker := time.NewTicker(5 * time.Second)
	// // 	for t := range ticker.C { ... }
	// // }

	log.Info().Str("remoteAddr", conn.RemoteAddr().String()).Msg("Sending data into WebSocket")
	// Send data to client every 1 sec
	for {

		// Get data
		data := <-statsChannel

		jsonData, err := json.Marshal(data)
		if err != nil {
			log.Error().Err(err).Msg("")
			return
		}

		// Send data
		err = conn.WriteMessage(websocket.TextMessage, jsonData)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Error().Err(err).Msg("")
				return
			}
			log.Warn().
				Str("remoteAddr", conn.RemoteAddr().String()).
				Msg("The client has closed the WebSocket connection")
			return
		}
	}
}
