package backend

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/processment/types"

	"github.com/Pegasus8/piworker/processment/configs"
	"github.com/Pegasus8/piworker/processment/data"
	actionsList "github.com/Pegasus8/piworker/processment/elements/actions/models"
	triggersList "github.com/Pegasus8/piworker/processment/elements/triggers/models"
	pwLogs "github.com/Pegasus8/piworker/processment/logs"
	"github.com/Pegasus8/piworker/processment/stats"
	"github.com/Pegasus8/piworker/webui/backend/auth"
	"github.com/Pegasus8/piworker/webui/backend/websocket"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gobuffalo/packr/v2"
	"github.com/gorilla/mux"
)

var statsChannel chan stats.Statistic
var tlsSupport bool

type postResponse struct {
	Successful bool   `json:"successful"`
	Error      string `json:"error"`
}

//
// ──────────────────────────────────────────────────── I ──────────
//   :::::: R O U T E S : :  :   :    :     :        :          :
// ──────────────────────────────────────────────────────────────
//

func setupRoutes() {
	defer auth.Database.Close()

	box := packr.New("WebUI", "../frontend/dist")

	auth.CheckSigningKey()

	router := mux.NewRouter()
	router.Use(loggingMiddleware)

	configs.CurrentConfigs.RLock()
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
		router.Handle("/api/tasks/delete", auth.IsAuthorized(deleteTaskAPI)).Methods("DELETE")
	}
	if apiConfigs.GetAllTasksAPI {
		router.Handle("/api/tasks/get-all", auth.IsAuthorized(getTasksAPI)).Methods("GET")
	}
	if apiConfigs.LogsAPI {
		router.Handle("/api/tasks/logs", auth.IsAuthorized(logsAPI)).Methods("GET")
	}
	if apiConfigs.StatisticsAPI {
		router.Handle("/api/info/statistics", auth.IsAuthorized(statisticsAPI)).Methods("GET")
	}
	router.Handle("/api/webui/triggers-structs", auth.IsAuthorized(triggersInfoAPI)).Methods("GET")
	router.Handle("/api/webui/actions-structs", auth.IsAuthorized(actionsInfoAPI)).Methods("GET")
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
	configs.CurrentConfigs.RUnlock()

	if _, err := os.Stat("./server.key"); err == nil {
		tlsSupport = true
		log.Println("File 'server.key' found")
		if _, err := os.Stat("./server.crt"); err == nil {
			log.Println("File 'server.crt' found")
			tlsSupport = true
		} else {
			tlsSupport = false
			log.Println("File 'server.crt' not found")
		}
	} else {
		tlsSupport = false
		log.Println("File 'server.key' not found")
	}

	if tlsSupport {
		log.Fatal(srv.ListenAndServeTLS("./server.crt", "./server.key"))
	} else {
		log.Fatal(srv.ListenAndServe())
	}
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
	if request.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	var response struct {
		Successful bool   `json:"successful"`
		Token      string `json:"token"`
		ExpiresAt  int64  `json:"expiresAt"`
	}
	var user = struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{}

	// Uncomment to enable CORS support.
	// setCORSHeaders(&w, request)

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("[ login API ] Error when trying to read the POST data sent by %s\n", request.Host)
		response.Successful = false
		goto response1
	}

	err = json.Unmarshal(body, &user)
	if err != nil {
		log.Printf("[ login API ] The data on the POST request of %s cannot be read\n", request.Host)
		response.Successful = false
		goto response1
	}

	if ok := configs.AuthUser(user.Username, user.Password); ok {
		configs.CurrentConfigs.RLock()
		duration := configs.CurrentConfigs.APIConfigs.TokenDuration
		configs.CurrentConfigs.RUnlock()
		expiresAt := time.Now().Add(time.Hour * time.Duration(duration))
		token, err := auth.NewJWT(
			auth.CustomClaims{
				User:           user.Username,
				StandardClaims: jwt.StandardClaims{ExpiresAt: expiresAt.Unix()},
			},
		)
		if err != nil {
			log.Println("[ login API ]", err.Error())
			response.Successful = false
			goto response1
		}
		response.Successful = true
		response.Token = token
		response.ExpiresAt = expiresAt.Unix()

		now := time.Now()
		err = auth.StoreToken(
			auth.UserInfo{
				ID:               0, // Not necessary, will be given by the sqlite database automatically.
				User:             user.Username,
				Token:            token,
				ExpiresAt:        expiresAt,
				LastTimeUsed:     now,
				InsertedDatetime: now,
			},
		)
		if err != nil {
			log.Fatal("[ login API ]", err.Error())
		}
	}

response1:

	json.NewEncoder(w).Encode(response)
}

func newTaskAPI(w http.ResponseWriter, request *http.Request) { // Method: POST
	if request.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	var response postResponse
	var task data.UserTask
	var tasksOnDB *data.UserData

	// Uncomment to enable CORS support.
	// setCORSHeaders(&w, request)

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("[ newTask API ] Error when trying to read the POST data sent by %s\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	err = json.Unmarshal(body, &task)
	if err != nil {
		log.Printf("[ newTask API ] The data on the POST request of %s cannot be read\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	// Read the data to see if the taskname already exists
	tasksOnDB, err = data.ReadData()
	if err != nil {
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}
	if _, _, err = tasksOnDB.GetTaskByName(task.TaskInfo.Name); err != nil {
		if err != data.ErrBadTaskName {
			response.Successful = false
			response.Error = err.Error()
			goto response1
		}
		// If the error is data.ErrBadTaskName means that the task doesn't
		// exists.
	} else {
		// If there is no error the name already exists.
		response.Successful = false
		response.Error = "The name of the task already exists"
		goto response1
	}

	err = data.NewTask(&task)
	if err != nil {
		log.Println("[ newTask API ]", err.Error())
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	response.Successful = true

response1:

	json.NewEncoder(w).Encode(response)
}

func modifyTaskAPI(w http.ResponseWriter, request *http.Request) { // Method: POST
	if request.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	var response postResponse
	var task data.UserTask

	// Uncomment to enable CORS support.
	// setCORSHeaders(&w, request)

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("[ modifyTask API ] Error when trying to read the POST data sent by %s\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	err = json.Unmarshal(body, &task)
	if err != nil {
		log.Printf("[ modifyTask API ] The data on the POST request of %s cannot be read\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	err = data.UpdateTask(task.TaskInfo.Name, &task)
	if err != nil {
		log.Println("[ modifyTask API ]", err.Error())
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	response.Successful = true

response1:

	json.NewEncoder(w).Encode(response)
}

func deleteTaskAPI(w http.ResponseWriter, request *http.Request) { // Method: DELETE
	if request.Method != "DELETE" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	var response postResponse
	var toDelete = struct {
		Taskname string `json:"taskname"`
	}{}

	// Uncomment to enable CORS support.
	//setCORSHeaders(&w, request)

	// TODO Implementation of partial delete

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("[ deleteTask API ] Error when trying to read the POST data sent by %s\n", request.Host)
		response.Successful = false
		response.Error = err.Error()
		goto response1
	}

	err = json.Unmarshal(body, &toDelete)
	if err != nil {
		log.Printf("[ deleteTask API ] The data on the POST request of %s cannot be read\n", request.Host)
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
	if request.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	keys, ok := request.URL.Query()["fromWebUI"]
	if !ok || len(keys[0]) < 1 {
		log.Println("[ get-tasks API ] Url Param 'fromWebUI' is missing, sending the data without recreation")
	}

	w.Header().Set("Content-Type", "application/json")
	userData, err := data.ReadData()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, err.Error())
		return
	}

	// fromWebUI = true
	if keys[0] == "true" {
		log.Println("[ get-tasks API ] Param 'fromWebUI' detected, this little maneuver is gonna cost us 54 years...")
		startTime := time.Now()

		type argForWebUI struct {
			Name        string       `json:"name"`
			Description string       `json:"description"`
			ID          string       `json:"ID"`
			Content     string       `json:"content"`
			ContentType types.PWType `json:"contentType"`
		}

		type triggerForWebUI struct {
			Name        string        `json:"name"`
			Description string        `json:"description"`
			ID          string        `json:"ID"`
			Timestamp   string        `json:"timestamp"`
			Args        []argForWebUI `json:"args"`
		}

		type actionForWebUI struct {
			Name                  string        `json:"name"`
			Description           string        `json:"description"`
			ID                    string        `json:"ID"`
			Timestamp             string        `json:"timestamp"`
			Args                  []argForWebUI `json:"args"`
			Order                 int           `json:"order"`
			Chained               bool          `json:"chained"`
			ArgumentToReplaceByCR string        `json:"argumentToReplaceByCR"`
		}

		type taskForWebUI struct {
			Name             string           `json:"name"`
			State            data.TaskState   `json:"state"`
			Trigger          triggerForWebUI  `json:"trigger"`
			Actions          []actionForWebUI `json:"actions"`
			Created          time.Time        `json:"created"`
			LastTimeModified time.Time        `json:"lastTimeModified"`
		}

		type userTaskFromWebUI struct {
			TaskInfo taskForWebUI `json:"task"`
		}

		type userDataFromWebUI struct {
			Tasks []userTaskFromWebUI `json:"user-data"`
		}

		var recreatedUserData userDataFromWebUI
		var results = make(chan *userTaskFromWebUI, len(userData.Tasks))

		for _, task := range userData.Tasks {
			// Usually, the best way of send data to a another goroutine is using channels among other things,
			// to avoid a race condition, but here we don't have that problem, because the data is not shared
			// between goroutines and because the data will be only read, will not be modified.
			go func(task data.UserTask, resultChannel chan *userTaskFromWebUI) {
				log.Printf("[ get-tasks API ] Starting the recreation of the task '%s'\n", task.TaskInfo.Name)
				startTime := time.Now()

				var recreatedUserTask userTaskFromWebUI
				var recreatedTask taskForWebUI

				recreatedTask.Name = task.TaskInfo.Name
				recreatedTask.State = task.TaskInfo.State
				recreatedTask.Created = task.TaskInfo.Created
				recreatedTask.LastTimeModified = task.TaskInfo.LastTimeModified

				for _, userAction := range task.TaskInfo.Actions {
					pwaction := actionsList.Get(userAction.ID)
					recreatedAction := actionForWebUI{
						Name:                  pwaction.Name,
						Description:           pwaction.Description,
						ID:                    userAction.ID,
						Timestamp:             userAction.Timestamp,
						Args:                  []argForWebUI{}, // Will be completed after
						Order:                 userAction.Order,
						Chained:               userAction.Chained,
						ArgumentToReplaceByCR: userAction.ArgumentToReplaceByCR,
					}
					for _, arg := range userAction.Args {
						for _, pwarg := range pwaction.Args {
							if arg.ID == pwarg.ID {
								recreatedArg := argForWebUI{
									Name:        pwarg.Name,
									Description: pwarg.Description,
									ID:          arg.ID,
									Content:     arg.Content,
									ContentType: pwarg.ContentType,
								}
								recreatedAction.Args = append(recreatedAction.Args, recreatedArg)
								break
							}
						}
					}

					recreatedTask.Actions = append(recreatedTask.Actions, recreatedAction)
				}

				func() {
					pwtrigger := triggersList.Get(task.TaskInfo.Trigger.ID)
					recreatedTrigger := triggerForWebUI{
						Name:        pwtrigger.Name,
						Description: pwtrigger.Description,
						ID:          task.TaskInfo.Trigger.ID,
						Timestamp:   task.TaskInfo.Trigger.Timestamp,
						Args:        []argForWebUI{}, // Will be completed after
					}
					for _, arg := range task.TaskInfo.Trigger.Args {
						for _, pwarg := range pwtrigger.Args {
							if arg.ID == pwarg.ID {
								recreatedArg := argForWebUI{
									Name:        pwarg.Name,
									Description: pwarg.Description,
									ID:          arg.ID,
									Content:     arg.Content,
									ContentType: pwarg.ContentType,
								}
								recreatedTrigger.Args = append(recreatedTrigger.Args, recreatedArg)
							}
						}
					}
					recreatedTask.Trigger = recreatedTrigger
				}()
				recreatedUserTask.TaskInfo = recreatedTask

				executionTime := time.Since(startTime)
				log.Printf("[ get-tasks API ] Task '%s' recreated in %s! Sending through the results channel...\n", recreatedTask.Name, executionTime.String())
				resultChannel <- &recreatedUserTask
			}(task, results)
		}

		for range userData.Tasks {
			t := <-results
			recreatedUserData.Tasks = append(recreatedUserData.Tasks, *t)
		}
		close(results)

		execTime := time.Since(startTime)
		log.Printf("[ get-tasks API ] Well, maybe I exaggerated. It wasn't 54 years, but it was close! Or maybe not... (request processed in %s)\n", execTime.String())
		
		json.NewEncoder(w).Encode(recreatedUserData.Tasks)
	} else {
		json.NewEncoder(w).Encode(userData.Tasks)
	}
}

func logsAPI(w http.ResponseWriter, request *http.Request) { // Method: GET
	if request.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	var response = struct {
		Successful bool     `json:"successful"`
		Error      string   `json:"error"`
		Logs       []string `json:"logs"`
	}{}
	var reqData = struct {
		Taskname string `json:"taskname"`
		Date     string `json:"date"`
	}{}
	var logsContent string

	defer func() {
		if r := recover(); r != nil {
			log.Println("[ logs API ] Recovering from panic triggered when getting logs")
		}
	}()

	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		response.Error = err.Error()
		goto resp
	}

	err = json.Unmarshal(body, &reqData)
	if err != nil {
		response.Error = err.Error()
		goto resp
	}

	logsContent, err = pwLogs.GetLogs()
	if err != nil {
		log.Panicln("[ logs API ] Cannot get the logs of PiWorker:", err.Error())
	}

	reqData.Date = strings.TrimSpace(reqData.Date)
	response.Logs, err = pwLogs.GetTaskLogs(&logsContent, reqData.Taskname, reqData.Date)
	if err != nil {
		response.Error = err.Error()
	} else {
		response.Successful = true
	}

resp:

	json.NewEncoder(w).Encode(response)
}

func statisticsAPI(w http.ResponseWriter, request *http.Request) { // Method: GET
	if request.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
}

func triggersInfoAPI(w http.ResponseWriter, request *http.Request) {
	if request.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(triggersList.TRIGGERS)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("[ triggersInfo API ] Error:", err.Error())
	}
}

func actionsInfoAPI(w http.ResponseWriter, request *http.Request) {
	if request.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(actionsList.ACTIONS)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println("[ actionsInfo API ] Error:", err.Error())
	}
}

func setCORSHeaders(w *http.ResponseWriter, reqest *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}
