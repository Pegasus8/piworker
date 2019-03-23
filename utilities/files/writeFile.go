package files

import (
	"path/filepath"
	"sync"
	"os"
	"io/ioutil"
)

var mutex sync.Mutex

// WriteFile is a function that writes byte content inside 
// some file. If the file already exists it will be overwritten.
func WriteFile(dir string, filename string, data []byte) (path string, err error) {
	
	// Clear dir string
	dir = filepath.Clean(dir)

	mutex.Lock()
	defer mutex.Unlock()

	finalPath := filepath.Join(dir, filename)
	tempPath := finalPath + ".tmp"

	// Create basedir if not exists
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return "", err
	}

	// Write temporal file
	err = ioutil.WriteFile(tempPath, data, 0644)
	if err != nil {
		return "", err
	}

	// Move temporal file to definitive file
	err = os.Rename(tempPath, finalPath)
	if err != nil {
		return "", err
	}

	return finalPath, nil
}