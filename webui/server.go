package webui

import (
	"fmt"
	"log"
	"net/http"

	"github.com/Pegasus8/piworker/webui/websocket"
)

func loadMainPage(w http.ResponseWriter, request *http.Request) {
	//TODO: launch main page
	fmt.Fprintf(w, "Hello from main page!")
}

func mainStats(w http.ResponseWriter, request *http.Request) {
	// Upgrade the connection from standard HTTP connection to WebSocket connection
	ws, err := websocket.Upgrade(w, request)
	if err != nil {
		fmt.Fprintf(w, "%+v\n", err)
		return
	}
	// Execution of data sending to the client 
	// into another goroutine
	go websocket.Writer(ws)
}

func setupRoutes() {
	const PORT = ":8080"

	//? Como lanzar la página principal (index.html)
	http.Handle("/", http.FileServer(http.Dir("./webui/frontend/static")))
	http.HandleFunc("/ws", mainStats)

	//TODO: implement https
	log.Println("Listening an serving on", PORT)
	log.Fatal(http.ListenAndServe(PORT, nil))
}

// Run - start the server
func Run() {
	log.Println("Starting server...")

	setupRoutes()
}