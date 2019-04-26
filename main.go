package main

import (
	"path/filepath"
	"time"
	"os"
	"strings"

	"github.com/Pegasus8/piworker/utilities/log"
	"github.com/Pegasus8/piworker/webui"
	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/stats"
)

func main() {

	// Logs settings
	var (
		loggingDir = "./logs/"
		logFile = setLogNameByDate("log")
		logFullpath = filepath.Join(loggingDir, logFile)
	)
	if err := prepareLogsDirectory(loggingDir); err != nil { log.Errorln(err) }
	f, err := os.OpenFile(
		logFullpath, os.O_RDWR | os.O_CREATE | os.O_APPEND, 0666,
	)
	if err != nil { log.Errorln(err) }
	defer f.Close()
	// All logs to the same file. Can be separated but is not necessary.
	log.Init(f, f, f, f, f)

	log.Infoln("Running PiWorker...")
	// Set user data filename
	data.Filename = "user_data.json" //TODO: assign the name dinamically

	// Statistics channel
	var statsChannel chan stats.Statistic

	// Stats loop
	go stats.StartLoop(statsChannel)

	// WebUI
	webui.Run(statsChannel)
}


func prepareLogsDirectory(dir string) error {
	// Create dir if not exists
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}
	return nil
}

func setLogNameByDate(name string) (formattedName string) {
	now := time.Now().String()
	now = strings.ReplaceAll(now, " ", "_")

	return name + "_" + now + ".log"
}