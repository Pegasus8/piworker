package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
	"io/ioutil"

	"github.com/Pegasus8/piworker/processment/stats"
	"github.com/Pegasus8/piworker/webui/auth"
	"github.com/Pegasus8/piworker/webui/websocket"
	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/configs"
	triggersList"github.com/Pegasus8/piworker/processment/elements/triggers/models"
	actionsList "github.com/Pegasus8/piworker/processment/elements/actions/models"

	"github.com/gorilla/mux"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gobuffalo/packr/v2"
)

var statsChannel chan stats.Statistic

type postResponse struct {
	Successful bool `json:"successful"`
	Error string `json:"error"`
}

//
// ──────────────────────────────────────────────────── I ──────────
//   :::::: R O U T E S : :  :   :    :     :        :          :
// ──────────────────────────────────────────────────────────────
//

func setupRoutes() {
	defer auth.Database.Close()

	box := packr.New("WebUI", "./frontend/dist")

	router := mux.NewRouter()

	apiConfigs := &configs.CurrentConfigs.APIConfigs

	// ─── APIS ───────────────────────────────────────────────────────────────────────
	router.HandleFunc("/api/login", loginAPI)
	if apiConfigs.NewTaskAPI {
		router.Handle("/api/tasks/new", auth.IsAuthorized(newTaskAPI)).Methods("POST")
	}
	if apiConfigs.EditTaskAPI {
		router.Handle("/api/tasks/modify", auth.IsAuthorized(modifyTaskAPI)).Methods("POST")
	}
	if apiConfigs.DeleteTaskAPI {
		router.Handle("/api/tasks/delete", auth.IsAuthorized(deleteTaskAPI)).Methods("POST")
	}
	if apiConfigs.GetAllTasksAPI {
		router.Handle("/api/tasks/get-all", auth.IsAuthorized(getTasksAPI)).Methods("GET")
	}
	if apiConfigs.StatisticsAPI {
		router.Handle("/api/info/statistics", auth.IsAuthorized(statisticsAPI)).Methods("GET")
	}
	router.Handle("/api/webui/triggers-structs", auth.IsAuthorized(triggersInfoAPI))
	router.Handle("/api/webui/actions-structs", auth.IsAuthorized(actionsInfoAPI))
	// ────────────────────────────────────────────────────────────────────────────────

	if configs.CurrentConfigs.WebUI.Enabled {
		// ─── WEBSOCKET ──────────────────────────────────────────────────────────────────
		router.Handle("/ws", auth.IsAuthorized(statsWS))
		// ────────────────────────────────────────────────────────────────────────────────
	
		// ─── SINGLE PAGE APP ────────────────────────────────────────────────────────────
		router.PathPrefix("/").Handler(http.FileServer(box))
		// ────────────────────────────────────────────────────────────────────────────────
	}

	srv := &http.Server{
		Handler:      router,
		Addr:         ":" + configs.CurrentConfigs.WebUI.ListeningPort,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Println("Listening and serving on port", configs.CurrentConfigs.WebUI.ListeningPort)
	log.Fatal(srv.ListenAndServe())
}

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

func loginAPI(w http.ResponseWriter, request *http.Request) { // Method: POST
	var response struct {
		Successful bool `json:"successful"`
		Token string `json:"token"`
		ExpiresAt int64 `json:"expiresAt"`
	}
	var user configs.User

	//FIXME Must be removed
	setCORSHeaders(&w, request)

	body, err := ioutil.ReadAll(request.Body)
	if err != nil{
		log.Printf("Error when trying to read the POST data sent by %s\n", request.Host)
		response.Successful = false
		goto response1
	}

	err = json.Unmarshal(body, &user)
	if err != nil {
		log.Printf("The data on the POST request of %s cannot be read\n", request.Host)
		response.Successful = false
		goto response1
	} 

	if ok := configs.AuthUser(user.Username, user.Password); ok {
		duration := configs.CurrentConfigs.APIConfigs.TokenDuration
		expiresAt := time.Now().Add(duration)
		token, err := auth.NewJWT(
			auth.CustomClaims{
				User: user.Username, 
				StandardClaims: jwt.StandardClaims{ExpiresAt: expiresAt.Unix()},
			},
		)
		if err != nil {
			log.Println(err.Error())
			response.Successful = false
			goto response1
		}
		response.Successful = true
		response.Token = token
		response.ExpiresAt = expiresAt.Unix()

		now := time.Now()
		err = auth.StoreToken(
			auth.UserInfo{
				ID: 0, // Not necessary, will be given by the sqlite database automatically.
				User: user.Username,
				Token: token,
				ExpiresAt: expiresAt,
				LastTimeUsed: now,
				InsertedDatetime: now,
			},
		)
		if err != nil {
			log.Fatal(err.Error())
		}
	}
	
	response1:

	json.NewEncoder(w).Encode(response)
}

func newTaskAPI(w http.ResponseWriter, request *http.Request) { // Method: POST
	var response postResponse
	var task data.UserTask 

	//FIXME Must be removed
	setCORSHeaders(&w, request)

	body, err := ioutil.ReadAll(request.Body)
	if err != nil{
		log.Printf("Error when trying to read the POST data sent by %s\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}
	
	err = json.Unmarshal(body, &task)
	if err != nil {
		log.Printf("The data on the POST request of %s cannot be read\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	} 

	err = data.NewTask(&task)
	if err != nil {
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	response.Successful = true

	response1:
		
	json.NewEncoder(w).Encode(response)
}

func modifyTaskAPI(w http.ResponseWriter, request *http.Request) { // Method: POST
	var response postResponse
	var task data.UserTask

	//FIXME Must be removed
	setCORSHeaders(&w, request)

	body, err := ioutil.ReadAll(request.Body)
	if err != nil{
		log.Printf("Error when trying to read the POST data sent by %s\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	err = json.Unmarshal(body, &task)
	if err != nil {
		log.Printf("The data on the POST request of %s cannot be read\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	err = data.UpdateTask(task.TaskInfo.Name, &task)
	if err != nil {
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	response.Successful = true

	response1:
		
	json.NewEncoder(w).Encode(response)
}

func deleteTaskAPI(w http.ResponseWriter, request *http.Request) { // Method: POST
	var response postResponse
	var toDelete = struct {
		Taskname string `json:"taskname"`
	}{}

	//FIXME Must be removed
	setCORSHeaders(&w, request)

	// TODO Implementation of partial delete

	body, err := ioutil.ReadAll(request.Body)
	if err != nil{
		log.Printf("Error when trying to read the POST data sent by %s\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	err = json.Unmarshal(body, &toDelete)
	if err != nil {
		log.Printf("The data on the POST request of %s cannot be read\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	err = data.DeleteTask(toDelete.Taskname)
	if err != nil {
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	response.Successful = true

	response1:
		
	json.NewEncoder(w).Encode(response)
}

func getTasksAPI(w http.ResponseWriter, request *http.Request) { // Method: GET
	userData, err := data.ReadData()
	if err != nil {
		fmt.Fprintf(w, err.Error())
		return
	}

	json.NewEncoder(w).Encode(userData.Tasks)
}

func statisticsAPI(w http.ResponseWriter, request *http.Request) { // Method: GET
}

func triggersInfoAPI(w http.ResponseWriter, request *http.Request) {
	err := json.NewEncoder(w).Encode(triggersList.TRIGGERS)
	if err != nil {
		log.Println("Error:", err.Error())
	}
}

func actionsInfoAPI(w http.ResponseWriter, request *http.Request) {
	err := json.NewEncoder(w).Encode(actionsList.ACTIONS)
	if err != nil {
		log.Println("Error:", err.Error())
	}
}

func setCORSHeaders(w *http.ResponseWriter, reqest *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
    (*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
    (*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}