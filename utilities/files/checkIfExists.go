package files

import (
	"os"
)

// Exists checks if the file passed on the `filepath` param exists or not.
func Exists(filepath string) (bool, error) {
	_, err := os.Stat(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
