package backend

import (
	"net/http"
	"github.com/gorilla/mux"
	"encoding/json"
	"time"
	"log"
)

func httpsCheckServer() {
	router := mux.NewRouter()

	router.HandleFunc("/https-check", func(w http.ResponseWriter, r *http.Request){
		w.Header().Set("Content-type", "application/json")
		response := struct {
			Enabled bool `json:"enabled"` 
		}{}
		response.Enabled = tlsSupport
		json.NewEncoder(w).Encode(response)
	}).Methods("GET")

	server := http.Server {
		Handler: router,
		Addr: ":8826",
		WriteTimeout: 15 * time.Second,
		ReadTimeout: 15 * time.Second,
	}

	log.Fatalln(server.ListenAndServe())
}

