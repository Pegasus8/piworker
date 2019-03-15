package stats

import (
	"encoding/json"
)

//TODO: pensar como será la estructura de las estadísticas

// Response is the struct parsed into JSON to be sended into the WebSocket
type Response struct {
	ActiveTasks int `json:"activeTasks"`

}

type Stats struct {
	//TODO
}

// GetStats return general stats of PiWorker
func GetStats() (jsondata []byte, err error) {
	data := map[string]Response{
		"activeTasksTest": Response{1},
	}

	jsonData, err := json.Marshal(data) 
	if err != nil {
		return []byte{}, err
	}

	return jsonData, nil
}

