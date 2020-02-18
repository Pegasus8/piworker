package stats

import (
	"errors"
)

// ErrBadTemperatureParse is the error used when the temperature of the Raspberry Pi
// can't be parsed.
var ErrBadTemperatureParse = errors.New(
	"Failed temperature parsing: can't locate the temperature",
)

// ErrBadCPULoadParse is the error used when the CPU load can't be parsed.
var ErrBadCPULoadParse = errors.New(
	"Falied CPU load parsing: can't locate the values",
)

// ErrBadFreeStorageParse is the error used when the Free Storage can't be parsed.
var ErrBadFreeStorageParse = errors.New(
	"Falied Free Storage parsing:  can't locate the value",
)

// ErrBadRAMUsageParse is the error used whem the RAM usage can't be parsed.
var ErrBadRAMUsageParse = errors.New(
	"Falied RAM usage parsing: can't locate the value",
)

// ErrNilDB is the error used when the database returns nil
var ErrNilDB = errors.New(
	"Database error: database is nil",
)
