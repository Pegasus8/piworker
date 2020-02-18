package auth

import (
	jwt "github.com/dgrijalva/jwt-go"
	"time"
)

// CustomClaims is the struct used to parse the claims from the JWT token
type CustomClaims struct {
	User string `json:"user"`
	jwt.StandardClaims
}

// UserInfo is the struct used to parse the auth info of some user from the
// sqlite3 database.
type UserInfo struct {
	ID               int64
	User             string
	Token            string
	ExpiresAt        time.Time
	LastTimeUsed     time.Time
	InsertedDatetime time.Time
}
