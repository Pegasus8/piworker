package logs

import (
	"io/ioutil"
	"path/filepath"
	"regexp"
	"errors"
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
func GetTaskLogs(logs *string, taskname, date string) (taskLogs []string, err error) {
	var dateRgx = regexp.MustCompile(`^\d{4}/\d{2}/\d{2}$`)
	var tasknameRgx = regexp.MustCompile(`^[\w? ?]+$`)
	// Prevent arbitrary regex execution
	if !dateRgx.MatchString(date) {
		date = `\d{4}/\d{2}/\d{2}` // Any date
	}
	if !tasknameRgx.MatchString(taskname) {
		return taskLogs, errors.New("Invalid taskname")
	}
	var rgx = regexp.MustCompile(`(?m)^(` + date + `).+:\s\[([` + taskname + `]+)\].+$`)

	for _, match := range rgx.FindAllString(*logs, -1) {
        taskLogs = append(taskLogs, match)
	}

	return taskLogs, nil
}
