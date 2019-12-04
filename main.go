package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/engine"
	"github.com/Pegasus8/piworker/processment/configs"
	"github.com/Pegasus8/piworker/processment/uservariables"
	"github.com/Pegasus8/piworker/processment/logs"
	"github.com/Pegasus8/piworker/utilities/files"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	if len(os.Args) > 1 {
		handleFlags()
		os.Exit(0)
	}
	start()
}

func start() {
	// Logs settings
	setLogSettings()

	log.Println("Running PiWorker...")
	// Set user data filename
	data.Filename = "user_data.json" //TODO: assign the name dinamically

	err := initConfigs()
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Println("Getting user variables from files...")
	log.Println("Reading user global variables...")
	globalVariables, err := uservariables.ReadGlobalVariablesFromFiles()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Global variables read correctly!, saving on the global variable...")
	uservariables.GlobalVariablesSlice = globalVariables
	log.Println("Global variables saved!")

	log.Println("Reading user local variables...")
	localVariables, err := uservariables.ReadLocalVariablesFromFiles()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Local variables read correctly!, saving on the global variable...")
	uservariables.LocalVariablesSlice = localVariables
	log.Println("Local variables saved!")

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

func setLogSettings() {
	logFullpath := filepath.Join(logs.LogsPath, logs.Filename)

	if err := prepareLogsDirectory(logs.LogsPath); err != nil {
		log.Panicln(err)
	}

	log.SetOutput(&lumberjack.Logger{
		Filename:  logFullpath,
		MaxSize:   25,
		MaxAge:    7,
		LocalTime: true,
	})
	log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

func initConfigs() error {
	configsPath := filepath.Join(configs.ConfigsPath, configs.Filename)
	exists, err := files.Exists(configsPath)
	if err != nil {
		return err
	}
	if !exists {
		configs.CurrentConfigs = &configs.DefaultConfigs
		err = configs.WriteToFile()
		if err != nil {
			return err
		}
	} else {
		err = configs.ReadFromFile()
		if err != nil {
			return err
		}
	}
	return nil
}
