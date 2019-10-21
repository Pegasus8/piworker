package auth

import (
	"errors"
)

// ErrNilDB is the error used when the database returns nil
var ErrNilDB = errors.New(
	"Database error: database is nil",
)