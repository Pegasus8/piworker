package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Pegasus8/piworker/core/configs"
	"github.com/Pegasus8/piworker/core/data"
	engine2 "github.com/Pegasus8/piworker/core/engine"
	"github.com/Pegasus8/piworker/core/logs"
	"github.com/Pegasus8/piworker/core/signals"
	"github.com/Pegasus8/piworker/core/stats"
	"github.com/Pegasus8/piworker/core/uservariables"
	"github.com/Pegasus8/piworker/utilities/files"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	start()
}

func start() {
	err := initConfigs()
	if err != nil {
		fmt.Println("Error when reading configs:", err)
		os.Exit(1)
	}
	handleFlags()

	log.Info().Msg("Starting PiWorker...")

	uservariables.Init()
	stats.Init()
	data.Init()

	signals.Shutdown = make(chan os.Signal)
	signal.Notify(signals.Shutdown, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

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

	// Initialize the engine.
	engine := engine2.NewEngine()

	// TODO Use hooks.

	engine.Start()
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
	configsPath := filepath.Join(configs.Path, configs.Filename)
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
