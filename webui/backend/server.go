package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/Pegasus8/piworker/core/configs"
	"github.com/Pegasus8/piworker/core/data"
	actionsList "github.com/Pegasus8/piworker/core/elements/actions/models"
	triggersList "github.com/Pegasus8/piworker/core/elements/triggers/models"
	"github.com/Pegasus8/piworker/core/stats"

	// pwLogs "github.com/Pegasus8/piworker/core/logs"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/Pegasus8/piworker/webui/backend/auth"
	"github.com/Pegasus8/piworker/webui/backend/websocket"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/markbates/pkger"
	"github.com/rs/zerolog/log"
)

var tlsSupport bool
var devMode bool

// -- Using (partially) example from https://github.com/gorilla/mux#serving-single-page-applications --.
// spaHandler implements the http.Handler interface, so we can use it
// to respond to HTTP requests. The path to the static directory and
// path to the index file within that static directory are used to
// serve the SPA in the given static directory.
type spaHandler struct {
	staticPath string
	indexPath  string
}

// ServeHTTP inspects the URL path to locate a file within the static dir
// on the SPA handler. If a file is found, it will be served. If not, the
// request will be redirected to the root path ("/").
func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Prepend the path with the path to the static directory.
	path := filepath.Join(h.staticPath, r.URL.Path)

	// Check whether a file exists at the given path.
	_, err := pkger.Stat(path)
	if err != nil {
		// If there is an error, the file does not exist, so we must redirect to the root path.
		http.Redirect(w, r, "/", http.StatusPermanentRedirect)

		return
	}

	if devMode {
		// Avoid cache if we are in development.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	}

	// Otherwise, use http.FileServer to serve the static dir.
	http.FileServer(pkger.Dir(h.staticPath)).ServeHTTP(w, r)
}

//
// ──────────────────────────────────────────────────── I ──────────
//   :::::: R O U T E S : :  :   :    :     :        :          :
// ──────────────────────────────────────────────────────────────
//

func setupRoutes() {
	defer auth.Database.Close()

	_ = pkger.Include("/webui/frontend/dist")

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
		router.Handle("/api/tasks/update", auth.IsAuthorized(updateTaskAPI)).Methods("PUT")
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
	if apiConfigs.TypesCompatAPI {
		router.Handle("/api/info/types-compat", auth.IsAuthorized(typesCompatAPI)).Methods("GET")
	}
	// ────────────────────────────────────────────────────────────────────────────────

	if configs.CurrentConfigs.WebUI.Enabled {
		// ─── WEBSOCKET ──────────────────────────────────────────────────────────────────
		router.HandleFunc("/ws", statsWS)
		router.Handle("/api/ws-auth", auth.IsAuthorized(wsAuthAPI)).Methods("GET")
		// ────────────────────────────────────────────────────────────────────────────────

		// ─── MODELS INFO ────────────────────────────────────────────────────────────────
		router.Handle("/api/webui/triggers-structs", auth.IsAuthorized(triggersInfoAPI)).Methods("GET")
		router.Handle("/api/webui/actions-structs", auth.IsAuthorized(actionsInfoAPI)).Methods("GET")
		// ────────────────────────────────────────────────────────────────────────────────

		// ─── SINGLE PAGE APP ────────────────────────────────────────────────────────────
		spa := spaHandler{staticPath: "/webui/frontend/dist", indexPath: "index.html"}
		router.PathPrefix("/").Handler(spa)
		// ────────────────────────────────────────────────────────────────────────────────

	}

	// If the setting `.Security.LocalNetworkAccess` is disabled, set the server's addr to localhost, preventing
	// external connections.
	var addr string
	if !configs.CurrentConfigs.Security.LocalNetworkAccess {
		addr = "127.0.0.1"
	}

	srv := &http.Server{
		Handler:      router,
		Addr:         addr + ":" + configs.CurrentConfigs.WebUI.ListeningPort,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Info().Msgf("Listening and serving on port %s", configs.CurrentConfigs.WebUI.ListeningPort)
	configs.CurrentConfigs.RUnlock()

	if _, err := os.Stat("./server.key"); err == nil {
		tlsSupport = true
		log.Info().Msg("File 'server.key' found")
		if _, err := os.Stat("./server.crt"); err == nil {
			log.Info().Msg("File 'server.crt' found")
			tlsSupport = true
		} else {
			tlsSupport = false
			log.Warn().Msg("File 'server.crt' not found")
		}
	} else {
		tlsSupport = false
		log.Warn().Msg("File 'server.key' not found")
	}

	if tlsSupport {
		log.Fatal().Err(srv.ListenAndServeTLS("./server.crt", "./server.key")).Msg("")
	} else {
		log.Fatal().Err(srv.ListenAndServe()).Msg("")
	}
}

// Run - start the server
func Run() {
	log.Info().Msg("Starting server...")

	if d := os.Getenv("DEV"); d != "" {
		log.Warn().Msg("Server on development mode activated")
		devMode = true
	}

	setupRoutes()
}

//
// ──────────────────────────────────────────────────────── II ──────────
//   :::::: H A N D L E R S : :  :   :    :     :        :          :
// ──────────────────────────────────────────────────────────────────
//

func statsWS(w http.ResponseWriter, request *http.Request) {
	keys, ok := request.URL.Query()["auth"]
	if !ok || len(keys[0]) < 1 {
		log.Error().
			Err(errors.New("Url Param 'auth' is missing")).
			Bool("wsAuthenticated", false).
			Str("remoteAddr", request.RemoteAddr).
			Msg("Rejecting request because absence of 'auth' param")

		w.WriteHeader(http.StatusBadRequest)

		return
	}

	authTicket := keys[0]

	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if authorized := auth.IsWSAuthorized(host, authTicket); !authorized {
		log.Warn().
			Bool("wsAuthenticated", false).
			Str("remoteAddr", request.RemoteAddr).
			Msg("Authorization failed")

		w.WriteHeader(http.StatusUnauthorized)

		return
	}

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

func wsAuthAPI(w http.ResponseWriter, request *http.Request) {
	if request.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var response = struct {
		Ticket string `json:"ticket"`
	}{}

	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	ticket := auth.NewWSTicket(host)
	response.Ticket = ticket

	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Error().
			Err(err).
			Str("api", "wsAuthAPI").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Error when trying to encode the JSON response")

		w.WriteHeader(http.StatusInternalServerError)
	}
}

func loginAPI(w http.ResponseWriter, request *http.Request) { // Method: POST
	if request.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var response struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expiresAt"`
		Admin     bool   `json:"admin"`
	}
	var user = struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{}

	// Uncomment to enable CORS support.
	// setCORSHeaders(&w, request)

	err := json.NewDecoder(request.Body).Decode(&user)
	if err != nil {
		log.Error().
			Err(err).
			Str("api", "login").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Error when trying to read the data received")

		w.WriteHeader(http.StatusBadRequest)

		return
	}

	if u, ok := configs.AuthUser(user.Username, user.Password); ok {
		configs.CurrentConfigs.RLock()
		duration := configs.CurrentConfigs.APIConfigs.TokenDuration
		configs.CurrentConfigs.RUnlock()
		now := time.Now()
		expiresAt := now.Add(time.Hour * time.Duration(duration))
		tokenID := uuid.New().String()
		token, err := auth.NewJWT(
			auth.CustomClaims{
				Admin:     u.Admin,
				UserAgent: request.UserAgent(),
				StandardClaims: jwt.StandardClaims{
					Subject:   u.Username,
					Issuer:    "PiWorker",
					Id:        tokenID,
					IssuedAt:  now.Unix(),
					ExpiresAt: expiresAt.Unix(),
				},
			},
		)
		if err != nil {
			log.Error().
				Err(err).
				Str("api", "login").
				Str("remoteAddr", request.RemoteAddr).
				Msg("")

			w.WriteHeader(http.StatusInternalServerError)

			return
		}
		response.Token = token
		response.ExpiresAt = expiresAt.Unix()
		response.Admin = u.Admin

		err = auth.StoreToken(
			auth.UserInfo{
				ID:               0, // Not necessary, will be given by the sqlite database automatically.
				User:             user.Username,
				TokenID:          tokenID,
				ExpiresAt:        expiresAt,
				LastTimeUsed:     now,
				InsertedDatetime: now,
			},
		)
		if err != nil {
			log.Error().
				Err(err).
				Str("api", "login").
				Str("remoteAddr", request.RemoteAddr).
				Msg("")
		}
	} else {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func typesCompatAPI(w http.ResponseWriter, request *http.Request) { // Method: GET
	if request.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(w).Encode(types.CompatList())
	if err != nil {
		log.Error().
			Err(err).
			Str("api", "typesCompatAPI").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Error when trying to encode the JSON response")

		w.WriteHeader(http.StatusInternalServerError)
	}
}

func newTaskAPI(w http.ResponseWriter, request *http.Request) { // Method: POST
	if request.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var task data.UserTask

	// Uncomment to enable CORS support.
	// setCORSHeaders(&w, request)

	err := json.NewDecoder(request.Body).Decode(&task)
	if err != nil {
		log.Error().
			Err(err).
			Str("api", "newTask").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Error when trying to read the data received")

		w.WriteHeader(http.StatusBadRequest)

		return
	}

	task.Created = time.Now()
	task.LastTimeModified = task.Created

	err = data.NewTask(&task)
	if err != nil {
		log.Error().
			Err(err).
			Str("api", "newTask").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Error when trying to add a new task")

		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

func updateTaskAPI(w http.ResponseWriter, request *http.Request) { // Method: PUT
	if request.Method != "PUT" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var task data.UserTask
	var taskID string

	// w.Header().Set("Content-Type", "application/json")
	keys, ok := request.URL.Query()["id"]
	if !ok || len(keys[0]) < 1 {
		log.Error().
			Err(errors.New("Url Param 'id' is missing")).
			Str("api", "updateTask").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Rejecting request because absence of 'id' param")

		w.WriteHeader(http.StatusBadRequest)

		return
	}
	taskID = keys[0]

	// Uncomment to enable CORS support.
	// setCORSHeaders(&w, request)

	err := json.NewDecoder(request.Body).Decode(&task)
	if err != nil {
		log.Error().
			Err(err).
			Str("api", "updateTask").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Error when trying to read the data received")

		w.WriteHeader(http.StatusBadRequest)

		return
	}

	err = data.UpdateTask(taskID, &task)
	if err != nil {
		log.Error().
			Err(err).
			Str("api", "updateTask").
			Str("remoteAddr", request.RemoteAddr).
			Msg("")

		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteTaskAPI(w http.ResponseWriter, request *http.Request) { // Method: DELETE
	if request.Method != "DELETE" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var taskID string

	keys, ok := request.URL.Query()["id"]
	if !ok || len(keys[0]) < 1 {
		log.Error().
			Err(errors.New("Url Param 'id' is missing")).
			Str("api", "deleteTask").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Rejecting request because absence of 'id' param")

		w.WriteHeader(http.StatusBadRequest)

		return
	}
	taskID = keys[0]

	// Uncomment to enable CORS support.
	//setCORSHeaders(&w, request)

	err := data.DeleteTask(taskID)
	if err != nil {
		log.Error().
			Err(err).
			Str("api", "deleteTask").
			Str("remoteAddr", request.RemoteAddr).
			Str("taskID", taskID).
			Msg("There was a problem when trying to delete the task")

		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

func getTasksAPI(w http.ResponseWriter, request *http.Request) { // Method: GET
	if request.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	keys, ok := request.URL.Query()["fromWebUI"]
	if !ok || len(keys[0]) < 1 {
		log.Warn().
			Str("api", "getTasks").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Url Param 'fromWebUI' is missing, sending the data without recreation")
	}

	tasks, err := data.GetTasks()
	if err != nil {
		log.Error().
			Err(err).
			Str("api", "getTasks").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Error when trying to read the user tasks")

		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	// fromWebUI = true
	if keys[0] == "true" {
		startTime := time.Now()

		log.Info().
			Str("api", "getTasks").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Param 'fromWebUI' detected, this little maneuver is gonna cost us 54 years...")

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
			Order                 uint8         `json:"order"`
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
			ID               string           `json:"ID"`
		}

		var recreatedUserData []taskForWebUI
		var results = make(chan *taskForWebUI, len(*tasks))

		for _, task := range *tasks {
			// Usually, the best way of send data to a another goroutine is using channels among other things,
			// to avoid a race condition, but here we don't have that problem, because the data is not shared
			// between goroutines and because the data will be only read, will not be modified.
			go func(task data.UserTask, resultChannel chan *taskForWebUI) {
				log.Info().
					Str("api", "getTasks").
					Str("remoteAddr", request.RemoteAddr).
					Str("taskID", task.ID).
					Msg("Starting the recreation of the task")

				startTime := time.Now()
				var recreatedTask taskForWebUI

				recreatedTask.Name = task.Name
				recreatedTask.State = task.State
				recreatedTask.Created = task.Created
				recreatedTask.LastTimeModified = task.LastTimeModified
				recreatedTask.ID = task.ID

				// Reformatting of actions
				for _, userAction := range task.Actions {
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

				// Reformatting of trigger
				pwtrigger := triggersList.Get(task.Trigger.ID)
				recreatedTrigger := triggerForWebUI{
					Name:        pwtrigger.Name,
					Description: pwtrigger.Description,
					ID:          task.Trigger.ID,
					Timestamp:   task.Trigger.Timestamp,
					Args:        []argForWebUI{}, // Will be completed after
				}
				for _, arg := range task.Trigger.Args {
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

				executionTime := time.Since(startTime)
				log.Info().
					Str("api", "getTasks").
					Str("remoteAddr", request.RemoteAddr).
					Str("taskID", task.ID).
					Dur("executionTime", executionTime).
					Msg("Task recreated, sending through the results channel...")
				resultChannel <- &recreatedTask
			}(task, results)
		}

		for range *tasks {
			t := <-results
			recreatedUserData = append(recreatedUserData, *t)
		}
		close(results)

		sort.Slice(recreatedUserData, func(i, j int) bool {
			return recreatedUserData[i].Created.After(recreatedUserData[j].Created)
		})

		execTime := time.Since(startTime)
		log.Info().
			Str("api", "getTasks").
			Str("remoteAddr", request.RemoteAddr).
			Dur("executionTime", execTime).
			Msg("Well, maybe I exaggerated. It wasn't 54 years, but it was close! Or maybe not...")

		err = json.NewEncoder(w).Encode(recreatedUserData)
		if err != nil {
			log.Error().
				Err(err).
				Str("api", "getTasks").
				Str("remoteAddr", request.RemoteAddr).
				Msg("Error when trying to encode the JSON response (with data recreated)")
		}
	} else {
		err = json.NewEncoder(w).Encode(tasks)
		if err != nil {
			log.Error().
				Err(err).
				Str("api", "getTasks").
				Str("remoteAddr", request.RemoteAddr).
				Msg("Error when trying to encode the JSON response")
		}
	}
}

func logsAPI(w http.ResponseWriter, request *http.Request) { // Method: GET
	if request.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusNotImplemented)

	/*
			w.Header().Set("Content-Type", "application/json")

			// TODO Use the ID of the task as a param

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
					log.Warn().
						Str("api", "logs").
						Str("remoteAddr", request.RemoteAddr).
						Msg("Recovering from panic triggered when getting logs")
				}
			}()

			err := json.NewDecoder(request.Body).Decode(&reqData)
			if err != nil {
				response.Error = err.Error()
				goto resp
			}

			logsContent, err = pwLogs.GetLogs()
			if err != nil {
				log.Panic().
					Err(err).
					Str("api", "logs").
					Str("remoteAddr", request.RemoteAddr).
					Msg("Cannot get the logs")
			}

			reqData.Date = strings.TrimSpace(reqData.Date)
			response.Logs, err = pwLogs.GetTaskLogs(&logsContent, reqData.Taskname, reqData.Date)
			if err != nil {
				log.Error().
					Err(err).
					Str("api", "logs").
					Str("remoteAddr", request.RemoteAddr).
					Str("taskID", reqData.Taskname). //TODO Change for ID
					Msg("")

				response.Error = err.Error()
			} else {
				response.Successful = true
			}

		resp:

			json.NewEncoder(w).Encode(response)
	*/
}

func statisticsAPI(w http.ResponseWriter, request *http.Request) { // Method: GET
	if request.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	rgx := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

	date, ok := request.URL.Query()["date"]
	if !ok || len(date[0]) < 1 {
		log.Error().
			Err(errors.New("Url Param 'date' is missing")).
			Str("api", "statisticsAPI").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Rejecting request because absence of 'date' param")

		w.WriteHeader(http.StatusBadRequest)

		return
	}

	hour, byHour := request.URL.Query()["hour"]
	if !byHour || len(hour[0]) < 1 {
		log.Info().
			Str("api", "statisticsAPI").
			Str("remoteAddr", request.RemoteAddr).
			Msg("Param 'hour' not found, that's not a problem")
	}

	if !rgx.MatchString(date[0]) {
		log.Error().
			Err(errors.New("Incorrect format")).
			Str("api", "statisticsAPI").
			Str("remoteAddr", request.RemoteAddr).
			Str("dateParam", date[0]).
			Str("hourParam", hour[0]).
			Msg("Rejecting request because a wrong format on the parameters")

		w.WriteHeader(http.StatusBadRequest)

		return
	}

	if byHour {
		ts, rs, err := stats.ReadStatsByHour(date[0], hour[0])
		if err != nil {
			log.Error().
				Err(err).
				Str("api", "statisticsAPI").
				Str("remoteAddr", request.RemoteAddr).
				Str("dateParam", date[0]).
				Str("hourParam", hour[0]).
				Msg("Error when trying to read the statistics from the db")

			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		r := struct {
			TasksStats *[]stats.TasksStats     `json:"tasksStats"`
			RpiStats   *[]stats.RaspberryStats `json:"rpiStats"`
		}{
			ts,
			rs,
		}

		err = json.NewEncoder(w).Encode(r)
		if err != nil {
			log.Error().
				Err(err).
				Str("api", "statisticsAPI").
				Str("remoteAddr", request.RemoteAddr).
				Msg("")

			w.WriteHeader(http.StatusInternalServerError)
		}
	} else {
		ts, rs, err := stats.ReadStatsByDate(date[0])
		if err != nil {
			log.Error().
				Err(err).
				Str("api", "statisticsAPI").
				Str("remoteAddr", request.RemoteAddr).
				Str("dateParam", date[0]).
				Str("hourParam", hour[0]).
				Msg("Error when trying to read the statistics from the db")

			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		r := struct {
			TasksStats *[]stats.TasksStats     `json:"tasksStats"`
			RpiStats   *[]stats.RaspberryStats `json:"rpiStats"`
		}{
			ts,
			rs,
		}

		err = json.NewEncoder(w).Encode(r)
		if err != nil {
			log.Error().
				Err(err).
				Str("api", "statisticsAPI").
				Str("remoteAddr", request.RemoteAddr).
				Msg("")

			w.WriteHeader(http.StatusInternalServerError)
		}
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
		log.Error().
			Err(err).
			Str("api", "triggersInfo").
			Str("remoteAddr", request.RemoteAddr).
			Msg("")

		w.WriteHeader(http.StatusInternalServerError)
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
		log.Error().
			Err(err).
			Str("api", "actionsInfo").
			Str("remoteAddr", request.RemoteAddr).
			Msg("")

		w.WriteHeader(http.StatusInternalServerError)
	}
}

func setCORSHeaders(w *http.ResponseWriter, reqest *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}
