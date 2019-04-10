package stats

import (
	"errors"
)

// ErrBadTemperatureParse is the error used when the temperature of the Raspberry Pi 
// can't be parsed.
var ErrBadTemperatureParse = errors.New(
	"Failed temperature parsing: can't locate the temperature",
)
