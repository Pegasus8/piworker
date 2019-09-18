package auth

import (
	// "os"
	"path/filepath"
	"log"
	
	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/utilities/files"
)

const (
	// Filename is used as the name of the file that contains the users
	Filename = "users.json"

	// UsersFilePath provides the path of the users file 
	UsersFilePath = data.DataPath
)

// User -
type User struct {
	Username string
	Password string
}

func init() {
	path := filepath.Join(UsersFilePath, Filename)
	// Check if the users file exists
	if exists, _ := files.Exists(path); !exists{
		err := initializeUsersFile()
		if err != nil {
			log.Fatal(err)
		}
	}
} 

// UserExists check if the user from the param exists in the users file.
func UserExists(user string) (bool, error) {


	return false, nil
}

func initializeUsersFile() error {

	return nil
}