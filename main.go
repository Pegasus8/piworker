package main

import (
	"path/filepath"
	"time"
	"os"
	"log"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/engine" 
	// "github.com/Pegasus8/piworker/processment/configs"
)

func main() {
	if len(os.Args) > 1 {
		handleFlags()
		os.Exit(0)
	}

	// Logs settings
	logFile := setLogSettings()
	defer logFile.Close()

	log.Println("Running PiWorker...")
	// Set user data filename
	data.Filename = "user_data.json" //TODO: assign the name dinamically

	// Start the Dynamic Engine
	engine.StartEngine()
}


func prepareLogsDirectory(dir string) error {
	// Create dir if not exists
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}
	return nil
}

func setLogSettings() (logFile *os.File){
	var (
		loggingDir = "./logs/"
		logFilename = setLogNameByDate("log")
		logFullpath = filepath.Join(loggingDir, logFilename)
	)

	if err := prepareLogsDirectory(loggingDir); err != nil { log.Panicln(err) }
	f, err := os.OpenFile(
		logFullpath, os.O_RDWR | os.O_CREATE | os.O_APPEND, 0666,
	)
	if err != nil { log.Panicln(err) }

	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	return f
}

func setLogNameByDate(name string) (formattedName string) {
	now := time.Now().Format("02-01-2006")

	return name + "_" + now + ".log"
}