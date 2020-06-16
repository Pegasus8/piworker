package configs

import "errors"

// ErrNoConfigFileDetected is the error used when the configs file is not founded.
var ErrNoConfigFileDetected = errors.New(
	"the file that contains the configurations was not found",
)

// ErrConfigFileCorrupted is the error used when the stored configurations
// can't be parsed correctly because an incorrect composition.
var ErrConfigFileCorrupted = errors.New(
	"the stored configurations are corrupt",
)

// ErrUserNotFound is the error used when a specific user is not found
// on the users slice (`Configs.Users`).
var ErrUserNotFound = errors.New("user not found")

// ErrUsernameExists is the error used when, at time of add a new user,
// the username already exists on the users slice (`Configs.Users`).
var ErrUsernameExists = errors.New(
	"the username is already in use",
)
