package main

import (
	// "log"
	"os"
	"path/filepath"

	"github.com/Pegasus8/piworker/core/configs"
	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/engine"
	"github.com/Pegasus8/piworker/core/logs"
	"github.com/Pegasus8/piworker/core/uservariables"
	"github.com/Pegasus8/piworker/utilities/files"
	"gopkg.in/natefinch/lumberjack.v2"
	"github.com/rs/zerolog"
    "github.com/rs/zerolog/log"
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

	log.Info().Msg("Starting PiWorker...")
	// Set user data filename
	data.Filename = "user_data.json" //TODO: assign the name dinamically

	err := initConfigs()
	if err != nil {
		log.Fatal().Err(err).Msg("Error when initializing configs")
	}

	log.Info().Str("path", uservariables.UserVariablesPath).Msg("Getting and reading user's global variables from files...")
	globalVariables, err := uservariables.ReadGlobalVariablesFromFiles()
	if err != nil {
		log.Fatal().Err(err).Msg("Error when trying to read the user's global variables")
	}
	log.Info().Int("length", len(*globalVariables)).Msg("Global variables read correctly!, saving them on the variable")
	uservariables.GlobalVariablesSlice = globalVariables

	log.Info().Str("path", uservariables.UserVariablesPath).Msg("Getting and reading user's local variables from files...")
	localVariables, err := uservariables.ReadLocalVariablesFromFiles()
	if err != nil {
		log.Fatal().Err(err).Msg("Error when trying to read the user's local variables")
	}
	log.Info().Int("length", len(*localVariables)).Msg("Local variables read correctly!, saving them on the variable")
	uservariables.LocalVariablesSlice = localVariables

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
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logFullpath := filepath.Join(logs.LogsPath, logs.Filename)

	if err := prepareLogsDirectory(logs.LogsPath); err != nil {
		log.Fatal().
			Err(err).
			Msg("Error when trying to initialize the directory of logs")
	}

	log.Logger = log.Output(&lumberjack.Logger{
		Filename:  logFullpath,
		MaxSize:   25,
		MaxAge:    7,
		LocalTime: true,
	})
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
