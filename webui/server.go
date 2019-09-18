package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	// "io/ioutil"

	"github.com/Pegasus8/piworker/processment/stats"
	"github.com/Pegasus8/piworker/webui/auth"
	"github.com/Pegasus8/piworker/webui/websocket"

	"github.com/gorilla/mux"
	// jwt "github.com/dgrijalva/jwt-go"
)

var statsChannel chan stats.Statistic

type mainpageHandler struct {
	staticPath string
	indexPath  string
}

func (h mainpageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// =========================================================
	//  Copy paste from https://github.com/gorilla/mux#serving-single-page-applications
	// =========================================================

	// get the absolute path to prevent directory traversal
	path, err := filepath.Abs(r.URL.Path)
	if err != nil {
		// if we failed to get the absolute path respond with a 400 bad request
		// and stop
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// prepend the path with the path to the static directory
	path = filepath.Join(h.staticPath, path)

	// check whether a file exists at the given path
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		// file does not exist, serve index.html
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	} else if err != nil {
		// if we got an error (that wasn't that the file doesn't exist) stating the
		// file, return a 500 internal server error and stop
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// otherwise, use http.FileServer to serve the static dir
	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

//
// ──────────────────────────────────────────────────── I ──────────
//   :::::: R O U T E S : :  :   :    :     :        :          :
// ──────────────────────────────────────────────────────────────
//

func setupRoutes() {
	const PORT = "8080" // TODO Read from the config file
	router := mux.NewRouter()
	mainHandler := mainpageHandler{ // FIXME Packr implementation
		staticPath: "./frontend/static",
		indexPath:  "./frontend/static/index.html",
	}

	// ─── APIS ───────────────────────────────────────────────────────────────────────
	router.HandleFunc("/api/auth", func(w http.ResponseWriter, r *http.Request) {
		// TODO Authenticate the user and give him a token
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	router.Handle("/api/tasks/new", auth.IsAuthorized(newTaskAPI)).Methods("POST")
	router.Handle("/api/tasks/modify", auth.IsAuthorized(modifyTaskAPI)).Methods("POST")
	router.Handle("/api/tasks/delete", auth.IsAuthorized(deleteTaskAPI)).Methods("POST")
	router.Handle("/api/tasks/get-all", auth.IsAuthorized(getTasksAPI)).Methods("GET")
	router.Handle("/api/info/statistics", auth.IsAuthorized(statisticsAPI)).Methods("GET")
	// ────────────────────────────────────────────────────────────────────────────────

	// ─── WEBSOCKET ──────────────────────────────────────────────────────────────────
	router.Handle("/ws", auth.IsAuthorized(statsWS))
	// ────────────────────────────────────────────────────────────────────────────────

	// ─── SINGLE PAGE APP ────────────────────────────────────────────────────────────
	router.PathPrefix("/").Handler(mainHandler)
	// ────────────────────────────────────────────────────────────────────────────────

	srv := &http.Server{
		Handler:      router,
		Addr:         ":" + PORT,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Println("Listening and serving on port", PORT)
	log.Fatal(srv.ListenAndServe())
}

var notImplemented = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Not Implemented"))
})

// Run - start the server
func Run(statsChan chan stats.Statistic) {
	log.Println("Starting server...")

	statsChannel = statsChan

	setupRoutes()
}

//
// ──────────────────────────────────────────────────────── II ──────────
//   :::::: H A N D L E R S : :  :   :    :     :        :          :
// ──────────────────────────────────────────────────────────────────
//

func homePage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello World")
	fmt.Println("Endpoint Hit: homePage")
}

func statsWS(w http.ResponseWriter, request *http.Request) {
	// Upgrade the connection from standard HTTP connection to WebSocket connection
	ws, err := websocket.Upgrade(w, request)
	if err != nil {
		fmt.Fprintf(w, "%+v\n", err)
		return
	}
	// Execution of data sending to the client
	// into another goroutine
	go websocket.Writer(ws, statsChannel)
}

func newTaskAPI(w http.ResponseWriter, request *http.Request) {
}

func modifyTaskAPI(w http.ResponseWriter, request *http.Request) {
}

func deleteTaskAPI(w http.ResponseWriter, request *http.Request) {
}

func getTasksAPI(w http.ResponseWriter, request *http.Request) {
}

func statisticsAPI(w http.ResponseWriter, request *http.Request) {
}
