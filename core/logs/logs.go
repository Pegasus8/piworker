package logs

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
)

// GetLogs obtains the logs from the file `last.log`.
func GetLogs() (content string, err error) {
	fileContent, err := ioutil.ReadFile(filepath.Join(LogsPath, Filename))
	if err != nil {
		return "", err
	}
	return string(fileContent), nil
}

// GetTaskLogs returns the logs provenient from a specific task.
// Format of date: YYYY/MM/DD.
func GetTaskLogs(logs *string, taskID, date string) (taskLogs []string, err error) {
	var dateRgx = regexp.MustCompile(`^\d{4}/\d{2}/\d{2}$`)
	var taskIDRgx = regexp.MustCompile(`^[a-zA-Z0-9-?]+$`)
	// Prevent arbitrary regex execution
	if !dateRgx.MatchString(date) {
		date = `\d{4}/\d{2}/\d{2}` // Any date
	}
	if !taskIDRgx.MatchString(taskID) {
		return taskLogs, fmt.Errorf("Invalid task ID '%s'", taskID)
	}

	var rgx = regexp.MustCompile(`(?m)^(` + date + `).+:\s\[([` + taskID + `]+)\].+$`)

	taskLogs = append(taskLogs, rgx.FindAllString(*logs, -1)...)

	return taskLogs, nil
}
