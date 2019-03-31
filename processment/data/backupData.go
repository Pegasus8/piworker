package data

import (
	"time"
	"log"
	"encoding/json"

	"github.com/Pegasus8/piworker/utilities/files"
)

// StartBackupLoop is a function used to backup the user data every 1 day.
func StartBackupLoop() error {
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
		for range time.Tick(time.Hour*24) {
			err := backup()
			if err != nil {
				log.Println("Error when trying to backup the data:", err)
			}
		}

	}()

	return nil
}

func backup() error {
	mutex.Lock()
	defer mutex.Unlock()

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

	_, err = files.WriteFile(DataPath, backupFilename, byteData)
	if err != nil {
		return err
	}

	return nil
}

