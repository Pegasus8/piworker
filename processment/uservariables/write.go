package uservariables

import (
	"encoding/json"
	"strings"
	"os"
	"log"

	"github.com/Pegasus8/piworker/utilities/files"
)

func init() {
	// Create data path if not exists
	err := os.MkdirAll(UserVariablesPath, os.ModePerm)
	if err != nil {
		log.Fatal(err)
	}
}

// WriteToFile writes the current content of the LocalVariable to the corresponding file.
func (localVar *LocalVariable) WriteToFile() error {
	localVar.Lock()
	filename := localVar.Name + "-" + strings.ReplaceAll(localVar.ParentTaskName, " ", "_")

	byteData, err := json.MarshalIndent(localVar, "", "   ")
	if err != nil {
		localVar.Unlock()
		return err
	}
	localVar.Unlock()

	_, err = files.WriteFile(UserVariablesPath, filename, byteData)
	if err != nil {
		return err
	}

	return nil
}

// WriteToFile writes the current content of the GlobalVariable to the corresponding file.
func (globalVar *GlobalVariable) WriteToFile() error {
	globalVar.Lock()
	byteData, err := json.MarshalIndent(globalVar, "", "   ")
	if err != nil {
		globalVar.Unlock()
		return err
	}
	globalVar.Unlock()

	_, err = files.WriteFile(UserVariablesPath, globalVar.Name, byteData)
	if err != nil {
		return err
	}

	return nil
}
