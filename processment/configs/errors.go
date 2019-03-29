package configs

import "errors"

// ErrNoConfigFileDetected is the error used when the configs file is not founded.
var ErrNoConfigFileDetected = errors.New(
	"Error: the file that contains the configurations was not finded",
)

// ErrConfigFileCorrupted is the error used when the stored configurations
// can't be parsed correctly because an incorrect composition.
var ErrConfigFileCorrupted = errors.New(
	"Error: the configurations stored are corrupt",
)

