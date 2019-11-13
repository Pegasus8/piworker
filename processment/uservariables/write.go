package uservariables

import (
	"encoding/json"
	"strings"

	"github.com/Pegasus8/piworker/utilities/files"
)

// WriteToFile writes the current content of the LocalVariable to the corresponding file.
func (localVar *LocalVariable) WriteToFile() error {
	globalMutex.Lock()
	filename := localVar.Name + "-" + strings.ReplaceAll(localVar.ParentTaskName, " ", "_")

	byteData, err := json.MarshalIndent(localVar, "", "   ")
	if err != nil {
		globalMutex.Unlock()
		return err
	}
	globalMutex.Unlock()

	_, err = files.WriteFile(UserVariablesPath, filename, byteData)
	if err != nil {
		return err
	}

	return nil
}

// WriteToFile writes the current content of the GlobalVariable to the corresponding file.
func (globalVar *GlobalVariable) WriteToFile() error {
	globalMutex.Lock()
	byteData, err := json.MarshalIndent(globalVar, "", "   ")
	if err != nil {
		globalMutex.Unlock()
		return err
	}
	globalMutex.Unlock()

	_, err = files.WriteFile(UserVariablesPath, globalVar.Name, byteData)
	if err != nil {
		return err
	}

	return nil
}
