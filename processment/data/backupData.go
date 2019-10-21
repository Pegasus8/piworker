package data

import (
	"time"
	"encoding/json"
	"strings"
	"log"

	"github.com/Pegasus8/piworker/processment/configs"
	"github.com/Pegasus8/piworker/utilities/files"
)

// StartBackupLoop is a function used to backup the user data every 1 day.
func StartBackupLoop() error {
	if !configs.CurrentConfigs.Backups.BackupData{
		log.Println("Data backup config disabled, skipping...")
		return nil
	}

	if Filename == "" {
		return ErrNoFilenameAsigned
	}
	if BackupLoopState == true {
		return ErrBackupLoopAlreadyActive
	}
	// Set the state of the loop on true for prevention of multiple executions
	BackupLoopState = true

	log.Println("Backup loop started")

	go func() {
		// If the loop ends for some reason the state must be false
		defer func() {
			BackupLoopState = false
			log.Println("Backup loop finished")
		}()

		// First backup
		if err := backup(); err != nil {
			log.Println("Error when trying to backup the data:", err)
		}

		// Backup loop
		for range time.Tick(time.Hour * time.Duration(configs.CurrentConfigs.Backups.Freq)) {
			err := backup()
			if err != nil {
				log.Println("Error when trying to backup the data:", err)
			}
		}

	}()

	return nil
}

func backup() error {
	data, err := ReadData()
	if err != nil {
		log.Println("Error:", err)
		return err
	}

	byteData, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}

	backupFilename := Filename + "_" + time.Now().String() + ".backup"
	backupFilename = strings.ReplaceAll(backupFilename, " ", "_")

	_, err = files.WriteFile(DataPath, backupFilename, byteData)
	if err != nil {
		return err
	}

	return nil
}

