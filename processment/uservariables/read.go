package uservariables

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// ReadLocalVariablesFromFiles reads the local variables stored on the files. Useful to restore the contents of the variables
// after a reboot or shutdown, for example.
func ReadLocalVariablesFromFiles() (*[]LocalVariable, error) {
	var localVariables []LocalVariable

	allFilesInfo, err := getFiles()
	if err != nil {
		return nil, err
	}
	localVariablesFiles := getLocalVariablesFiles(&allFilesInfo)
	for _, lvf := range localVariablesFiles {
		var lv LocalVariable
		fullpath := filepath.Join(UserVariablesPath, lvf.Name())
		globalMutex.Lock()

		jsonData, err := os.Open(fullpath)
		if err != nil {
			globalMutex.Unlock()
			return &localVariables, err
		}
		defer jsonData.Close()
		globalMutex.Unlock()

		byteContent, err := ioutil.ReadAll(jsonData)
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal(byteContent, &lv)
		if err != nil {
			return nil, err
		}

		localVariables = append(localVariables, lv)
	}

	return &localVariables, nil
}

// ReadGlobalVariablesFromFiles reads the global variables stored on the files. Useful to restore the contents of the variables
// after a reboot or shutdown, for example.
func ReadGlobalVariablesFromFiles() (*[]GlobalVariable, error) {
	var globalVariables []GlobalVariable

	allFilesInfo, err := getFiles()
	if err != nil {
		return nil, err
	}
	globalVariablesFiles := getGlobalVariablesFiles(&allFilesInfo)
	for _, gvf := range globalVariablesFiles {
		var gv GlobalVariable
		fullpath := filepath.Join(UserVariablesPath, gvf.Name())
		globalMutex.Lock()

		jsonData, err := os.Open(fullpath)
		if err != nil {
			globalMutex.Unlock()
			return &globalVariables, err
		}
		defer jsonData.Close()
		globalMutex.Unlock()

		byteContent, err := ioutil.ReadAll(jsonData)
		if err != nil {
			return &globalVariables, err
		}

		err = json.Unmarshal(byteContent, &gv)
		if err != nil {
			return &globalVariables, err
		}

		globalVariables = append(globalVariables, gv)
	}

	return &globalVariables, nil
}

// ContainLocalVariable is a function used to identify if an argument contains a LocalVariable reference.
func ContainLocalVariable(argument string) bool {
	return localVariableRgx.MatchString(argument)
}

// ContainGlobalVariable is a function used to identify if an argument contains a GlobalVariable reference.
func ContainGlobalVariable(argument string) bool {
	return globalVariableRgx.MatchString(argument)
}

// GetLocalVariableName returns the name of the variable used on an argument.
func GetLocalVariableName(argument string) string {
	if !ContainLocalVariable(argument) {
		return ""
	}
	match := localVariableRgx.FindStringSubmatch(argument)
	var variableName string
	if match != nil {
		variableName = match[1]
	}

	return variableName
}

// GetGlobalVariableName returns the name of the variable used on an argument.
func GetGlobalVariableName(argument string) string {
	if !ContainGlobalVariable(argument) {
		return ""
	}
	match := globalVariableRgx.FindStringSubmatch(argument)
	var variableName string
	if match != nil {
		variableName = match[1]
	}

	return variableName
}

func getFiles() ([]os.FileInfo, error) {
	files, err := ioutil.ReadDir(UserVariablesPath)
	if err != nil {
		return nil, err
	}
	for index, file := range files {
		// Remove a folder if exists. Teorically this can't happen, but prevention is not bad.
		if file.IsDir() {
			files = append(files[:index], files[index+1:]...)
		}
	}

	return files, nil
}

func getLocalVariablesFiles(files *[]os.FileInfo) []os.FileInfo {
	var lvfiles []os.FileInfo
	for _, file := range *files {
		// Filename with all letters on lowercase = local variable file
		if file.Name() == strings.ToLower(file.Name()) {
			// Add it to the returned list of files
			lvfiles = append(lvfiles, file)
		}
	}

	return lvfiles
}

func getGlobalVariablesFiles(files *[]os.FileInfo) []os.FileInfo {
	var gvfiles []os.FileInfo
	for _, file := range *files {
		// Filename with all letters on uppercase = global variable file
		if file.Name() == strings.ToUpper(file.Name()) {
			// Add it to the returned list of files
			gvfiles = append(gvfiles, file)
		}
	}

	return gvfiles
}
